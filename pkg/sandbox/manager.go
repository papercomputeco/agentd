package sandbox

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/papercomputeco/agentd/pkg/api"
	"github.com/papercomputeco/agentd/pkg/config"
	"github.com/papercomputeco/agentd/pkg/harness"
)

const (
	// defaultPollInterval is how often the manager checks if the
	// sandbox is still running.
	defaultPollInterval = 2 * time.Second

	// restartBackoff is the delay between restart attempts.
	restartBackoff = 3 * time.Second
)

// ManagerOpts holds configuration for creating a new sandbox Manager.
type ManagerOpts struct {
	Config              *config.AgentConfig
	Harness             harness.Harness
	Runner              *Runner
	Env                 map[string]string // merged secrets + agent env
	Prompt              string            // resolved prompt
	Debug               bool
	ClosureManifestPath string   // path to pre-computed closure manifest
	BundleBaseDir       string   // base directory for sandbox bundles
	ExtraPackages       []string // additional nixpkgs attribute names to install
}

// Manager manages a single sandboxed agent process lifecycle.
// It is the gVisor counterpart to the tmux-based pkg/native.Manager.
type Manager struct {
	config              *config.AgentConfig
	harness             harness.Harness
	runner              *Runner
	env                 map[string]string
	prompt              string
	debug               bool
	closureManifestPath string
	bundleBaseDir       string
	extraPackages       []string

	mu           sync.Mutex
	running      bool
	restarts     int
	lastErr      string
	cancel       context.CancelFunc
	done         chan struct{}
	sandboxID    string
	bundleDir    string
	closureCache []string // cached closure paths
}

// NewManager creates a new sandbox manager with the given options.
func NewManager(opts ManagerOpts) *Manager {
	bundleBase := opts.BundleBaseDir
	if bundleBase == "" {
		bundleBase = "/run/agentd/sandboxes"
	}
	manifestPath := opts.ClosureManifestPath
	if manifestPath == "" {
		manifestPath = DefaultClosureManifest
	}

	return &Manager{
		config:              opts.Config,
		harness:             opts.Harness,
		runner:              opts.Runner,
		env:                 opts.Env,
		prompt:              opts.Prompt,
		debug:               opts.Debug,
		closureManifestPath: manifestPath,
		bundleBaseDir:       bundleBase,
		extraPackages:       opts.ExtraPackages,
		done:                make(chan struct{}),
	}
}

// Start launches the sandboxed agent and begins the monitoring loop.
// It runs until the context is cancelled or the agent exits and the
// restart policy does not call for a restart.
func (m *Manager) Start(ctx context.Context) error {
	ctx, m.cancel = context.WithCancel(ctx)

	// Apply timeout if configured.
	timeout, err := m.config.TimeoutDuration()
	if err != nil {
		return fmt.Errorf("parsing timeout: %w", err)
	}
	if timeout > 0 {
		ctx, m.cancel = context.WithTimeout(ctx, timeout)
	}

	// Compute the Nix closure (cached for restarts).
	if err := m.ensureClosure(ctx); err != nil {
		return fmt.Errorf("computing closure: %w", err)
	}

	// Launch the initial sandbox.
	if err := m.launchSandbox(ctx); err != nil {
		return fmt.Errorf("launching sandbox: %w", err)
	}

	go m.run(ctx)
	return nil
}

// Stop gracefully stops the sandboxed agent.
func (m *Manager) Stop() error {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()

	// Wait for the monitoring loop to finish.
	<-m.done

	return m.stopSandbox()
}

// IsRunning returns whether the sandbox is currently running.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Status returns the current agent status suitable for the API.
func (m *Manager) Status() api.AgentStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return api.AgentStatus{
		Name:     m.harness.Name(),
		Running:  m.running,
		Session:  m.sandboxID, // sandbox ID serves as the "session" identifier
		Restarts: m.restarts,
		Error:    m.lastErr,
		Type:     "sandboxed",
	}
}

// Restarts returns the number of times the sandbox has been restarted.
func (m *Manager) Restarts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.restarts
}

// ensureClosure loads the base closure from the manifest and
// materializes any extra packages. The result is cached and reused
// across restarts since store paths don't change during agentd's
// lifetime.
func (m *Manager) ensureClosure(ctx context.Context) error {
	if len(m.closureCache) > 0 {
		return nil
	}

	// 1. Load the base closure from the manifest (required).
	basePaths, err := ComputeClosureFromManifest(m.closureManifestPath)
	if err != nil {
		return fmt.Errorf("loading base closure manifest: %w", err)
	}
	if len(basePaths) == 0 {
		return fmt.Errorf("base closure manifest %s is empty", m.closureManifestPath)
	}
	log.Printf("sandbox: loaded %d store paths from manifest %s", len(basePaths), m.closureManifestPath)

	// 2. Materialize extra packages if configured.
	var extraPaths []string
	if len(m.extraPackages) > 0 {
		log.Printf("sandbox: materializing %d extra packages", len(m.extraPackages))
		extraPaths, err = MaterializePackages(ctx, m.extraPackages)
		if err != nil {
			return fmt.Errorf("materializing extra packages: %w", err)
		}
		log.Printf("sandbox: extra packages contributed %d store paths", len(extraPaths))
	}

	// 3. Merge and cache.
	m.closureCache = MergePaths(basePaths, extraPaths)
	log.Printf("sandbox: total closure: %d store paths", len(m.closureCache))
	return nil
}

// launchSandbox creates an OCI bundle and runs the sandbox.
func (m *Manager) launchSandbox(ctx context.Context) error {
	// Generate a deterministic sandbox ID.
	m.sandboxID = fmt.Sprintf("agent-%s", m.harness.Name())

	// Create the bundle directory.
	m.bundleDir = filepath.Join(m.bundleBaseDir, m.sandboxID)
	rootfsDir := filepath.Join(m.bundleDir, "rootfs")

	if err := os.MkdirAll(rootfsDir, 0755); err != nil {
		return fmt.Errorf("creating bundle directory: %w", err)
	}

	// Prepare the rootfs.
	if err := PrepareRootfs(rootfsDir, m.closureCache); err != nil {
		return fmt.Errorf("preparing rootfs: %w", err)
	}

	// Build the harness command.
	bin, args := m.harness.BuildCommand(m.prompt)

	if m.debug {
		if len(args) > 0 {
			log.Printf("sandbox: [debug] command: %s %s", bin, strings.Join(args, " "))
		} else {
			log.Printf("sandbox: [debug] command: %s", bin)
		}
		log.Printf("sandbox: [debug] workdir: %s", m.config.Workdir)
		log.Printf("sandbox: [debug] env keys: %s", envKeys(m.env))
		log.Printf("sandbox: [debug] sandbox ID: %s", m.sandboxID)
		log.Printf("sandbox: [debug] bundle dir: %s", m.bundleDir)
		log.Printf("sandbox: [debug] closure: %d store paths", len(m.closureCache))
	}

	// Parse memory limit.
	memBytes, err := m.config.MemoryBytes()
	if err != nil {
		return fmt.Errorf("parsing memory limit: %w", err)
	}

	// Generate the OCI spec.
	sandboxCfg := &Config{
		ID:          m.sandboxID,
		Command:     bin,
		Args:        args,
		Env:         m.env,
		Workdir:     m.config.Workdir,
		StorePaths:  m.closureCache,
		MemoryLimit: memBytes,
		PidLimit:    int64(m.config.PidLimit),
		Hostname:    m.sandboxID,
	}

	spec, err := GenerateSpec(sandboxCfg)
	if err != nil {
		return fmt.Errorf("generating OCI spec: %w", err)
	}

	specJSON, err := MarshalSpec(spec)
	if err != nil {
		return fmt.Errorf("marshaling OCI spec: %w", err)
	}

	configPath := filepath.Join(m.bundleDir, "config.json")
	if err := os.WriteFile(configPath, specJSON, 0644); err != nil {
		return fmt.Errorf("writing config.json: %w", err)
	}

	// Run the sandbox.
	if err := m.runner.Run(ctx, m.sandboxID, m.bundleDir); err != nil {
		return fmt.Errorf("runsc run: %w", err)
	}

	// Verify it's running.
	running, err := m.runner.IsRunning(ctx, m.sandboxID)
	if err != nil {
		return fmt.Errorf("checking sandbox state: %w", err)
	}
	if !running {
		return fmt.Errorf("sandbox %s failed to start", m.sandboxID)
	}

	m.mu.Lock()
	m.running = true
	m.lastErr = ""
	m.mu.Unlock()

	log.Printf("sandbox: agent %q launched in sandbox %q", m.harness.Name(), m.sandboxID)
	return nil
}

// stopSandbox gracefully stops and cleans up the running sandbox.
func (m *Manager) stopSandbox() error {
	m.mu.Lock()
	wasRunning := m.running
	sandboxID := m.sandboxID
	bundleDir := m.bundleDir
	m.mu.Unlock()

	if !wasRunning {
		return nil
	}

	ctx := context.Background()

	// Check if the sandbox is still running.
	running, _ := m.runner.IsRunning(ctx, sandboxID)
	if !running {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
		// Clean up the bundle directory.
		_ = m.runner.Delete(ctx, sandboxID)
		_ = os.RemoveAll(bundleDir)
		return nil
	}

	grace, err := m.config.GraceDuration()
	if err != nil {
		grace = 30 * time.Second
	}

	log.Printf("sandbox: sending SIGTERM to sandbox %q, grace period %s", sandboxID, grace)

	// Send SIGTERM.
	if err := m.runner.Kill(ctx, sandboxID, "SIGTERM"); err != nil {
		log.Printf("sandbox: error sending SIGTERM to %q: %v", sandboxID, err)
	}

	// Wait for the sandbox to exit within the grace period.
	deadline := time.After(grace)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	exited := false
	for !exited {
		select {
		case <-deadline:
			log.Printf("sandbox: grace period expired, sending SIGKILL to %q", sandboxID)
			_ = m.runner.Kill(ctx, sandboxID, "SIGKILL")
			// Give a moment for SIGKILL to take effect.
			time.Sleep(1 * time.Second)
			exited = true
		case <-ticker.C:
			running, _ := m.runner.IsRunning(ctx, sandboxID)
			if !running {
				log.Printf("sandbox: sandbox %q exited gracefully", sandboxID)
				exited = true
			}
		}
	}

	// Delete the container and clean up the bundle.
	if err := m.runner.Delete(ctx, sandboxID); err != nil {
		log.Printf("sandbox: error deleting container %q: %v", sandboxID, err)
	}
	if err := os.RemoveAll(bundleDir); err != nil {
		log.Printf("sandbox: error removing bundle %q: %v", bundleDir, err)
	}

	m.mu.Lock()
	m.running = false
	m.mu.Unlock()

	return nil
}

// run monitors the sandbox and handles restarts per the configured
// restart policy.
func (m *Manager) run(ctx context.Context) {
	defer close(m.done)

	for {
		// Wait for the sandbox to exit or context to be cancelled.
		exitCh := make(chan struct{})
		go func() {
			m.waitForExit(ctx)
			close(exitCh)
		}()

		select {
		case <-ctx.Done():
			// Shutdown requested — Stop() handles cleanup.
			return

		case <-exitCh:
			m.mu.Lock()
			m.running = false
			m.mu.Unlock()

			log.Printf("sandbox: agent %q exited", m.harness.Name())

			if !m.shouldRestart() {
				log.Printf("sandbox: not restarting agent %q (policy=%s, restarts=%d)",
					m.harness.Name(), m.config.Restart, m.restarts)
				return
			}

			m.restarts++
			log.Printf("sandbox: restarting agent %q (attempt %d)", m.harness.Name(), m.restarts)

			// Clean up old sandbox before restart.
			cleanupCtx := context.Background()
			_ = m.runner.Delete(cleanupCtx, m.sandboxID)
			_ = os.RemoveAll(m.bundleDir)

			// Backoff before restart.
			select {
			case <-ctx.Done():
				return
			case <-time.After(restartBackoff):
			}

			if err := m.launchSandbox(ctx); err != nil {
				log.Printf("sandbox: failed to restart agent %q: %v", m.harness.Name(), err)
				m.mu.Lock()
				m.lastErr = err.Error()
				m.mu.Unlock()
				return
			}
		}
	}
}

// waitForExit polls runsc state until the sandbox is no longer running.
func (m *Manager) waitForExit(ctx context.Context) {
	ticker := time.NewTicker(defaultPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			running, _ := m.runner.IsRunning(ctx, m.sandboxID)
			if !running {
				return
			}
		}
	}
}

// shouldRestart evaluates whether the sandbox should be restarted based
// on the configured restart policy and restart count.
func (m *Manager) shouldRestart() bool {
	switch m.config.Restart {
	case config.RestartNo:
		return false

	case config.RestartOnFailure, config.RestartAlways:
		if m.config.MaxRestarts > 0 && m.restarts >= m.config.MaxRestarts {
			return false
		}
		return true

	default:
		return false
	}
}

// envKeys returns a sorted, comma-separated list of environment variable
// names. Values are omitted because they may contain secrets.
func envKeys(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
