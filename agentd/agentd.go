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
	"sort"
	"sync"
	"time"

	"github.com/papercomputeco/agentd/pkg/api"
	"github.com/papercomputeco/agentd/pkg/config"
	"github.com/papercomputeco/agentd/pkg/harness"
	"github.com/papercomputeco/agentd/pkg/secrets"
	"github.com/papercomputeco/agentd/pkg/supervisor"
	"github.com/papercomputeco/agentd/pkg/tmux"
)

const (
	// SecretDir is where secrets are written for agentd to consume.
	SecretDir = "/run/stereos/secrets"

	// TmuxSocketPath is the dedicated tmux server socket for agentd sessions.
	TmuxSocketPath = "/run/stereos/agentd-tmux.sock"

	// DefaultConfigPath is the default location for the jcard.toml config.
	DefaultConfigPath = "/etc/stereos/jcard.toml"

	// DefaultAPISocketPath is the default Unix socket for agentd's API.
	DefaultAPISocketPath = "/run/stereos/agentd.sock"

	// DefaultReconcileInterval is how often the reconciliation loop ticks.
	DefaultReconcileInterval = 5 * time.Second
)

// Daemon is the agent daemon. It runs a reconciliation loop that
// watches for configuration and secret changes, serves an API for
// external consumers to pull agent state, and manages agent harnesses
// in tmux sessions via a supervisor.
type Daemon struct {
	configPath        string
	secretDir         string
	apiSocketPath     string
	tmuxSocketPath    string
	reconcileInterval time.Duration
	debug             bool

	// runtime state, guarded by mu
	mu         sync.Mutex
	supervisor *supervisor.Supervisor
	tmux       *tmux.Server
	apiServer  *api.Server

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
	}
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
// supervisor logs the full command, environment variable names, and
// captures tmux pane output when agents exit.
func (d *Daemon) SetDebug(debug bool) {
	d.debug = debug
}

// AgentStatuses implements api.AgentProvider. It returns the status of
// all managed agents (currently at most one).
func (d *Daemon) AgentStatuses() []api.AgentStatus {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.supervisor == nil {
		return nil
	}

	return []api.AgentStatus{d.supervisor.Status()}
}

// Run starts the agentd daemon and blocks until the context is cancelled.
// It performs the following lifecycle:
//
//  1. Start the tmux server
//  2. Start the API server
//  3. Run the reconciliation loop (reads config + secrets, converges)
//  4. On shutdown: stop supervisor, stop API, stop tmux
func (d *Daemon) Run(ctx context.Context) error {
	log.Println("agentd: initializing agent manager")

	// 1. Start tmux server.
	log.Println("agentd: starting tmux server")
	d.tmux = tmux.NewServer(d.tmuxSocketPath)
	if err := d.tmux.Start(); err != nil {
		return fmt.Errorf("starting tmux server: %w", err)
	}
	defer func() {
		log.Println("agentd: stopping tmux server")
		_ = d.tmux.Stop()
	}()

	// 2. Start API server.
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

	// 3. Run the reconciliation loop.
	log.Printf("agentd: starting reconciliation loop (interval=%s)", d.reconcileInterval)
	d.reconcileLoop(ctx)

	// 4. Graceful shutdown.
	log.Println("agentd: shutting down")
	d.mu.Lock()
	sup := d.supervisor
	d.mu.Unlock()

	if sup != nil {
		log.Println("agentd: stopping agent")
		if err := sup.Stop(); err != nil {
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
	hasSupervisor := d.supervisor != nil
	d.mu.Unlock()

	if !changed && hasSupervisor {
		// No changes and agent is already running — nothing to do.
		return
	}

	// If the desired state changed and we have a running supervisor, stop it.
	if changed && hasSupervisor {
		log.Println("agentd: reconcile: config or secrets changed, restarting agent")
		d.mu.Lock()
		sup := d.supervisor
		d.supervisor = nil
		d.mu.Unlock()

		if err := sup.Stop(); err != nil {
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

	// Create and start supervisor.
	sup := supervisor.NewSupervisor(supervisor.Opts{
		Config:  cfg,
		Harness: h,
		Tmux:    d.tmux,
		Env:     mergedEnv,
		Prompt:  prompt,
		Debug:   d.debug,
	})

	log.Printf("agentd: reconcile: launching agent harness=%s session=%s", cfg.Harness, cfg.Session)
	if err := sup.Start(ctx); err != nil {
		log.Printf("agentd: reconcile: error starting agent: %v", err)
		return
	}

	d.mu.Lock()
	d.supervisor = sup
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
