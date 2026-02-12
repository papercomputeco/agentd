// Package agentd implements the agent daemon — responsible for starting,
// supervising, and stopping configured agent harnesses (Claude Code,
// OpenCode, Gemini CLI, etc.).
//
// agentd manages tmux sessions for each agent, allowing the admin user
// to "tmux attach [session]" to introspect running agents.
//
// Communication with stereosd is over a local unix socket HTTP API. Secrets
// are read from a tmpfs directory written by stereosd.
package agentd

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/papercomputeco/agentd/pkg/config"
	"github.com/papercomputeco/agentd/pkg/harness"
	"github.com/papercomputeco/agentd/pkg/secrets"
	"github.com/papercomputeco/agentd/pkg/stereosd"
	"github.com/papercomputeco/agentd/pkg/supervisor"
	"github.com/papercomputeco/agentd/pkg/tmux"
)

const (
	// StereosdSocketPath is the unix socket used to communicate with stereosd.
	StereosdSocketPath = "/run/stereos/stereosd.sock"

	// SecretDir is where stereosd writes secrets for agentd to consume.
	SecretDir = "/run/stereos/secrets"

	// TmuxSocketPath is the dedicated tmux server socket for agentd sessions.
	TmuxSocketPath = "/run/stereos/agentd-tmux.sock"

	// DefaultConfigPath is the default location for the jcard.toml config.
	DefaultConfigPath = "/etc/stereos/jcard.toml"

	// stereosdConnectRetryInterval is the delay between connection attempts
	// to the stereosd unix socket.
	stereosdConnectRetryInterval = 2 * time.Second

	// stereosdConnectMaxRetries is the maximum number of connection attempts
	// before giving up.
	stereosdConnectMaxRetries = 30
)

// Daemon is the agent daemon. It connects to stereosd, reads configuration,
// launches agent harnesses in tmux sessions, and supervises their lifecycle.
type Daemon struct {
	configPath string
}

// NewDaemon creates a new agentd instance. The configPath is the path to
// the jcard.toml file. If empty, DefaultConfigPath is used.
func NewDaemon(configPath string) *Daemon {
	if configPath == "" {
		configPath = DefaultConfigPath
	}
	return &Daemon{
		configPath: configPath,
	}
}

// Run starts the agentd daemon and blocks until the context is cancelled.
// It performs the following lifecycle:
//
//  1. Connect to stereosd over unix socket (with retry)
//  2. Read and validate the [agent] config from jcard.toml
//  3. Read secrets from /run/stereos/secrets/
//  4. Start the tmux server
//  5. Resolve the agent harness
//  6. Launch the agent via the supervisor
//  7. Block until shutdown signal
//  8. Graceful shutdown: stop agent, clean up tmux, notify stereosd
func (d *Daemon) Run(ctx context.Context) error {
	log.Println("agentd: initializing agent manager")

	// 1. Connect to stereosd.
	log.Println("agentd: connecting to stereosd")
	stereosdClient := stereosd.NewClient(StereosdSocketPath)
	defer stereosdClient.Close()

	if err := d.waitForStereosd(ctx, stereosdClient); err != nil {
		return fmt.Errorf("waiting for stereosd: %w", err)
	}
	log.Println("agentd: connected to stereosd")

	// 2. Read and validate config.
	log.Printf("agentd: loading config from %s", d.configPath)
	cfg, err := config.LoadConfig(d.configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	log.Printf("agentd: config loaded: harness=%s session=%s workdir=%s restart=%s",
		cfg.Harness, cfg.Session, cfg.Workdir, cfg.Restart)

	// 3. Read secrets from tmpfs.
	log.Println("agentd: reading secrets")
	secretReader := secrets.NewReader(SecretDir)
	secretEnv, err := secretReader.ReadAll()
	if err != nil {
		return fmt.Errorf("reading secrets: %w", err)
	}
	log.Printf("agentd: loaded %d secrets", len(secretEnv))

	// 4. Start tmux server.
	log.Println("agentd: starting tmux server")
	tmuxServer := tmux.NewServer(TmuxSocketPath)
	if err := tmuxServer.Start(); err != nil {
		return fmt.Errorf("starting tmux server: %w", err)
	}
	defer func() {
		log.Println("agentd: stopping tmux server")
		_ = tmuxServer.Stop()
	}()

	// 5. Resolve harness.
	h, err := harness.Get(cfg.Harness)
	if err != nil {
		return fmt.Errorf("resolving harness: %w", err)
	}
	log.Printf("agentd: using harness %q", h.Name())

	// 6. Resolve prompt.
	prompt, err := cfg.ResolvePrompt()
	if err != nil {
		return fmt.Errorf("resolving prompt: %w", err)
	}
	if prompt != "" {
		log.Printf("agentd: prompt loaded (%d chars)", len(prompt))
	} else {
		log.Println("agentd: no prompt configured, agent starts in interactive mode")
	}

	// Merge environment: secrets first, then agent env (agent env overrides).
	mergedEnv := make(map[string]string, len(secretEnv)+len(cfg.Env))
	for k, v := range secretEnv {
		mergedEnv[k] = v
	}
	for k, v := range cfg.Env {
		mergedEnv[k] = v
	}

	// 7. Create and start supervisor.
	sup := supervisor.NewSupervisor(supervisor.Opts{
		Config:   cfg,
		Harness:  h,
		Tmux:     tmuxServer,
		Stereosd: stereosdClient,
		Env:      mergedEnv,
		Prompt:   prompt,
	})

	log.Println("agentd: launching agent")
	if err := sup.Start(ctx); err != nil {
		return fmt.Errorf("starting supervisor: %w", err)
	}

	log.Println("agentd: ready")

	// 8. Block until shutdown signal.
	<-ctx.Done()

	log.Println("agentd: shutting down")

	// 9. Graceful shutdown.
	log.Println("agentd: stopping agent")
	if err := sup.Stop(); err != nil {
		log.Printf("agentd: error stopping agent: %v", err)
	}

	// Notify stereosd that all agents are stopped.
	log.Println("agentd: notifying stereosd agents stopped")
	notifyCtx, notifyCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer notifyCancel()
	if err := stereosdClient.NotifyAgentsStopped(notifyCtx); err != nil {
		log.Printf("agentd: error notifying stereosd: %v", err)
	}

	log.Println("agentd: shutdown complete")
	return nil
}

// waitForStereosd retries connecting to stereosd until it responds to a
// ping or the context is cancelled.
func (d *Daemon) waitForStereosd(ctx context.Context, client *stereosd.Client) error {
	for i := 0; i < stereosdConnectMaxRetries; i++ {
		if err := client.Ping(ctx); err == nil {
			return nil
		}

		log.Printf("agentd: waiting for stereosd (attempt %d/%d)", i+1, stereosdConnectMaxRetries)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(stereosdConnectRetryInterval):
		}
	}

	return fmt.Errorf("stereosd not reachable after %d attempts", stereosdConnectMaxRetries)
}
