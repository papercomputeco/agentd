// Package agentd implements the agent daemon — responsible for starting,
// supervising, and stopping configured agent harnesses (Claude Code,
// OpenCode, Gemini CLI, etc.).
//
// agentd manages tmux sessions for each agent, allowing the admin user
// to "tmux attach [session]" to introspect running agents.
//
// The daemon uses a reconciliation-loop architecture inspired by
// Kubernetes: it periodically reads the desired state from the
// configuration file and secrets directory, compares it against the
// current runtime state, and converges (starting, stopping, or
// reconfiguring agents as needed). This design decouples agentd from
// any control-plane daemon — external consumers pull agent state from
// agentd's own HTTP API served over a Unix domain socket.
package agentd

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"maps"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/papercomputeco/agentd/pkg/api"
	"github.com/papercomputeco/agentd/pkg/config"
	"github.com/papercomputeco/agentd/pkg/harness"
	"github.com/papercomputeco/agentd/pkg/native"
	"github.com/papercomputeco/agentd/pkg/sandbox"
	"github.com/papercomputeco/agentd/pkg/secrets"
	"github.com/papercomputeco/agentd/pkg/tmux"
)

const (
	// SecretDir is where secrets are written for agentd to consume.
	SecretDir = "/run/stereos/secrets"

	// TmuxSocketPath is the dedicated tmux server socket for agentd sessions.
	// Lives under /run/agentd/ (owned by agent:admin) rather than /run/stereos/
	// (owned by root:admin) so the agent user can create and own the socket.
	TmuxSocketPath = "/run/agentd/tmux.sock"

	// DefaultConfigPath is the default location for the jcard.toml config.
	DefaultConfigPath = "/etc/stereos/jcard.toml"

	// DefaultAPISocketPath is the default Unix socket for agentd's API.
	DefaultAPISocketPath = "/run/stereos/agentd.sock"

	// DefaultReconcileInterval is how often the reconciliation loop ticks.
	DefaultReconcileInterval = 5 * time.Second

	// AgentUser is the unprivileged user that agent harnesses run as.
	// tmux sessions are launched as this user so the processes execute
	// with agent-level (not root) privileges.
	AgentUser = "agent"

	// DefaultSandboxStateDir is the default root directory for runsc state.
	DefaultSandboxStateDir = "/run/agentd/runsc-state"

	// DefaultSandboxBundleDir is the default base directory for OCI bundles.
	DefaultSandboxBundleDir = "/run/agentd/sandboxes"
)

// AgentManager is the common interface for agent lifecycle management.
// Both the tmux-based native manager and the gVisor sandbox manager
// implement this interface.
type AgentManager interface {
	Start(ctx context.Context) error
	Stop() error
	IsRunning() bool
	Status() api.AgentStatus
}

// Daemon is the agent daemon. It runs a reconciliation loop that
// watches for configuration and secret changes, serves an API for
// external consumers to pull agent state, and manages agent harnesses
// via either tmux sessions (native) or gVisor sandboxes (sandboxed).
type Daemon struct {
	configPath        string
	secretDir         string
	apiSocketPath     string
	tmuxSocketPath    string
	reconcileInterval time.Duration
	debug             bool

	// Sandbox configuration.
	runscPath        string // path to the runsc binary
	sandboxStateDir  string // --root flag for runsc
	sandboxBundleDir string // base directory for OCI bundles

	// runtime state, guarded by mu
	mu        sync.Mutex
	manager   AgentManager
	tmux      *tmux.Server
	runner    *sandbox.Runner
	apiServer *api.Server

	// lastConfigHash and lastSecretHash track whether config/secrets
	// have changed since the last reconciliation.
	lastConfigHash [sha256.Size]byte
	lastSecretHash [sha256.Size]byte
}

// NewDaemon creates a new agentd instance. The configPath is the path to
// the jcard.toml file. If empty, DefaultConfigPath is used.
func NewDaemon(configPath string) *Daemon {
	if configPath == "" {
		configPath = DefaultConfigPath
	}
	return &Daemon{
		configPath:        configPath,
		secretDir:         SecretDir,
		apiSocketPath:     DefaultAPISocketPath,
		tmuxSocketPath:    TmuxSocketPath,
		reconcileInterval: DefaultReconcileInterval,
		sandboxStateDir:   DefaultSandboxStateDir,
		sandboxBundleDir:  DefaultSandboxBundleDir,
	}
}

// SetRunscPath overrides the auto-detected runsc binary path.
func (d *Daemon) SetRunscPath(path string) {
	d.runscPath = path
}

// SetSandboxStateDir overrides the default sandbox state directory.
func (d *Daemon) SetSandboxStateDir(dir string) {
	d.sandboxStateDir = dir
}

// SetSandboxBundleDir overrides the default sandbox bundle directory.
func (d *Daemon) SetSandboxBundleDir(dir string) {
	d.sandboxBundleDir = dir
}

// SetAPISocketPath overrides the default API socket path. This is useful
// for testing or non-standard deployments.
func (d *Daemon) SetAPISocketPath(path string) {
	d.apiSocketPath = path
}

// SetTmuxSocketPath overrides the default tmux socket path.
func (d *Daemon) SetTmuxSocketPath(path string) {
	d.tmuxSocketPath = path
}

// SetSecretDir overrides the default secret directory.
func (d *Daemon) SetSecretDir(path string) {
	d.secretDir = path
}

// SetReconcileInterval overrides the default reconcile interval.
func (d *Daemon) SetReconcileInterval(interval time.Duration) {
	d.reconcileInterval = interval
}

// SetDebug enables or disables debug logging. When enabled, the
// manager logs the full command, environment variable names, and
// captures tmux pane output when agents exit.
func (d *Daemon) SetDebug(debug bool) {
	d.debug = debug
}

// AgentStatuses implements api.AgentProvider. It returns the status of
// all managed agents (currently at most one).
func (d *Daemon) AgentStatuses() []api.AgentStatus {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.manager == nil {
		return nil
	}

	return []api.AgentStatus{d.manager.Status()}
}

// Run starts the agentd daemon and blocks until the context is cancelled.
// It performs the following lifecycle:
//
//  1. Initialize sandbox runner
//  2. Start the tmux server
//  3. Start the API server
//  4. Run the reconciliation loop (reads config + secrets, converges)
//  5. On shutdown: stop manager, stop API, stop tmux
func (d *Daemon) Run(ctx context.Context) error {
	log.Println("agentd: initializing agent manager")

	// 1. Verify required binaries. agentd runs on stereOS (NixOS) and
	// requires both runsc (gVisor) and nix to be available.
	if _, err := exec.LookPath("nix"); err != nil {
		return fmt.Errorf("nix not found in PATH: %w", err)
	}

	// Initialize sandbox runner. gVisor (runsc) is required — agentd
	// cannot start without it since sandboxed is the default agent type.
	runner, err := sandbox.NewRunner(d.runscPath, d.sandboxStateDir)
	if err != nil {
		return fmt.Errorf("sandbox runtime unavailable: %w", err)
	}
	d.runner = runner
	d.runner.Debug = d.debug
	log.Printf("agentd: sandbox runtime initialized (runsc=%s, state=%s)", runner.RunscPath, d.sandboxStateDir)

	// Clean up any orphaned containers from a previous crash.
	if err := d.runner.Cleanup(ctx); err != nil {
		log.Printf("agentd: warning: sandbox cleanup: %v", err)
	}

	// 2. Start tmux server.
	// Run tmux as the agent user so sessions execute with agent-level
	// privileges and the socket is owned by agent (tmux enforces UID
	// ownership checks on socket connections).
	log.Printf("agentd: starting tmux server (run-as=%s)", AgentUser)
	d.tmux = tmux.NewServerAs(d.tmuxSocketPath, AgentUser)
	if err := d.tmux.Start(); err != nil {
		return fmt.Errorf("starting tmux server: %w", err)
	}
	defer func() {
		log.Println("agentd: stopping tmux server")
		_ = d.tmux.Stop()
	}()

	// 3. Start API server.
	log.Printf("agentd: starting API server on %s", d.apiSocketPath)
	d.apiServer = api.NewServer(d.apiSocketPath, d)
	if err := d.apiServer.Start(); err != nil {
		return fmt.Errorf("starting API server: %w", err)
	}
	defer func() {
		log.Println("agentd: stopping API server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = d.apiServer.Stop(shutdownCtx)
	}()

	// 4. Run the reconciliation loop.
	log.Printf("agentd: starting reconciliation loop (interval=%s)", d.reconcileInterval)
	d.reconcileLoop(ctx)

	// 5. Graceful shutdown.
	log.Println("agentd: shutting down")
	d.mu.Lock()
	mgr := d.manager
	d.mu.Unlock()

	if mgr != nil {
		log.Println("agentd: stopping agent")
		if err := mgr.Stop(); err != nil {
			log.Printf("agentd: error stopping agent: %v", err)
		}
	}

	log.Println("agentd: shutdown complete")
	return nil
}

// reconcileLoop ticks at the configured interval, reads desired state,
// and converges toward it. It runs the first reconciliation immediately.
func (d *Daemon) reconcileLoop(ctx context.Context) {
	// Reconcile immediately on startup.
	d.reconcile(ctx)

	ticker := time.NewTicker(d.reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.reconcile(ctx)
		}
	}
}

// reconcile reads the current desired state (config file + secrets) and
// converges. If the config or secrets have changed since the last
// reconciliation, the agent is restarted with the new values.
func (d *Daemon) reconcile(ctx context.Context) {
	// Read config file as raw bytes so we can hash before parsing.
	cfgBytes, err := os.ReadFile(d.configPath)
	if err != nil {
		// Config not present yet — that's fine, we'll try again next tick.
		log.Printf("agentd: reconcile: config not available: %v", err)
		return
	}

	cfg, err := config.ParseConfig(string(cfgBytes))
	if err != nil {
		log.Printf("agentd: reconcile: invalid config: %v", err)
		return
	}

	// Read secrets (best-effort; missing dir is fine).
	secretReader := secrets.NewReader(d.secretDir)
	secretEnv, err := secretReader.ReadAll()
	if err != nil {
		log.Printf("agentd: reconcile: warning: failed to read secrets: %v", err)
		secretEnv = make(map[string]string)
	}

	// Compute hashes to detect changes.
	configHash := sha256.Sum256(cfgBytes)
	secretHash := hashSecrets(secretEnv)

	d.mu.Lock()
	changed := configHash != d.lastConfigHash || secretHash != d.lastSecretHash
	hasManager := d.manager != nil
	d.mu.Unlock()

	if !changed && hasManager {
		// No changes and agent is already running — nothing to do.
		return
	}

	// If the desired state changed and we have a running manager, stop it.
	if changed && hasManager {
		log.Println("agentd: reconcile: config or secrets changed, restarting agent")
		d.mu.Lock()
		mgr := d.manager
		d.manager = nil
		d.mu.Unlock()

		if err := mgr.Stop(); err != nil {
			log.Printf("agentd: reconcile: error stopping previous agent: %v", err)
		}
	}

	// Resolve harness.
	h, err := harness.Get(cfg.Harness)
	if err != nil {
		log.Printf("agentd: reconcile: %v", err)
		return
	}

	// Resolve prompt.
	prompt, err := cfg.ResolvePrompt()
	if err != nil {
		log.Printf("agentd: reconcile: error resolving prompt: %v", err)
		return
	}

	// Merge environment: secrets first, then agent env (agent env overrides).
	mergedEnv := make(map[string]string, len(secretEnv)+len(cfg.Env))
	maps.Copy(mergedEnv, secretEnv)
	maps.Copy(mergedEnv, cfg.Env)

	// Create the appropriate manager based on agent type.
	var mgr AgentManager

	switch cfg.Type {
	case config.AgentTypeSandboxed:
		if d.runner == nil {
			log.Println("agentd: reconcile: type=sandboxed but sandbox runner not initialized (this should not happen)")
			return
		}
		mgr = sandbox.NewManager(sandbox.ManagerOpts{
			Config:        cfg,
			Harness:       h,
			Runner:        d.runner,
			Env:           mergedEnv,
			Prompt:        prompt,
			Debug:         d.debug,
			BundleBaseDir: d.sandboxBundleDir,
			ExtraPackages: cfg.ExtraPackages,
		})
		log.Printf("agentd: reconcile: launching sandboxed agent harness=%s", cfg.Harness)

	case config.AgentTypeNative:
		mgr = native.NewManager(native.Opts{
			Config:  cfg,
			Harness: h,
			Tmux:    d.tmux,
			Env:     mergedEnv,
			Prompt:  prompt,
			Debug:   d.debug,
		})
		log.Printf("agentd: reconcile: launching native agent harness=%s session=%s", cfg.Harness, cfg.Session)

	default:
		log.Printf("agentd: reconcile: unknown agent type %q", cfg.Type)
		return
	}

	if err := mgr.Start(ctx); err != nil {
		log.Printf("agentd: reconcile: error starting agent: %v", err)
		return
	}

	d.mu.Lock()
	d.manager = mgr
	d.lastConfigHash = configHash
	d.lastSecretHash = secretHash
	d.mu.Unlock()

	log.Println("agentd: reconcile: agent running")
}

// hashSecrets produces a deterministic hash of a secrets map by sorting
// keys and hashing key-value pairs.
func hashSecrets(env map[string]string) [sha256.Size]byte {
	h := sha256.New()

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(env[k]))
		_, _ = h.Write([]byte{0})
	}

	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out
}
