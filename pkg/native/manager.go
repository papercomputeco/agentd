// Package native manages the lifecycle of a native agent process running
// in a tmux session. It handles starting the agent, monitoring its health,
// implementing restart policies (no, on-failure, always), enforcing
// timeouts, and coordinating graceful shutdown.
package native

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/papercomputeco/agentd/pkg/api"
	"github.com/papercomputeco/agentd/pkg/config"
	"github.com/papercomputeco/agentd/pkg/harness"
	"github.com/papercomputeco/agentd/pkg/tmux"
)

const (
	// defaultPollInterval is how often the manager checks if the
	// agent's tmux session is still running.
	defaultPollInterval = 2 * time.Second

	// restartBackoff is the delay between restart attempts.
	restartBackoff = 3 * time.Second
)

// Opts holds configuration for creating a new Manager.
type Opts struct {
	Config  *config.AgentConfig
	Harness harness.Harness
	Tmux    *tmux.Server
	Env     map[string]string // merged secrets + agent env
	Prompt  string            // resolved prompt
	Debug   bool              // enable verbose debug logging
}

// Manager manages a single agent process lifecycle.
type Manager struct {
	config  *config.AgentConfig
	harness harness.Harness
	tmux    *tmux.Server
	env     map[string]string
	prompt  string
	debug   bool

	mu       sync.Mutex
	running  bool
	restarts int
	lastErr  string
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewManager creates a new manager with the given options.
func NewManager(opts Opts) *Manager {
	return &Manager{
		config:  opts.Config,
		harness: opts.Harness,
		tmux:    opts.Tmux,
		env:     opts.Env,
		prompt:  opts.Prompt,
		debug:   opts.Debug,
		done:    make(chan struct{}),
	}
}

// Start launches the agent and begins the run loop. It runs
// until the context is cancelled or the agent exits and the restart
// policy does not call for a restart.
func (s *Manager) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)

	// Apply timeout if configured.
	timeout, err := s.config.TimeoutDuration()
	if err != nil {
		return fmt.Errorf("parsing timeout: %w", err)
	}
	if timeout > 0 {
		ctx, s.cancel = context.WithTimeout(ctx, timeout)
	}

	// Launch the initial agent process.
	if err := s.launchAgent(); err != nil {
		return fmt.Errorf("launching agent: %w", err)
	}

	go s.run(ctx)
	return nil
}

// Stop gracefully stops the agent. It sends SIGINT (C-c) to the tmux
// session, waits the grace period, and then forcibly destroys the session.
func (s *Manager) Stop() error {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()

	// Wait for the run loop to finish.
	<-s.done

	return s.stopAgent()
}

// IsRunning returns whether the agent process is currently running.
func (s *Manager) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Status returns the current agent status suitable for the API.
func (s *Manager) Status() api.AgentStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return api.AgentStatus{
		Name:     s.config.Name,
		Running:  s.running,
		Session:  s.config.Session,
		Restarts: s.restarts,
		Error:    s.lastErr,
		Type:     "native",
	}
}

// Restarts returns the number of times the agent has been restarted.
func (s *Manager) Restarts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restarts
}

// launchAgent creates a tmux session with the agent harness command.
func (s *Manager) launchAgent() error {
	bin, args := s.harness.BuildCommand(s.prompt)

	if s.debug {
		if len(args) > 0 {
			log.Printf("manager: [debug] command: %s %s", bin, strings.Join(args, " "))
		} else {
			log.Printf("manager: [debug] command: %s", bin)
		}
		log.Printf("manager: [debug] workdir: %s", s.config.Workdir)
		log.Printf("manager: [debug] env keys: %s", envKeys(s.env))
		log.Printf("manager: [debug] tmux socket: %s", s.tmux.SocketPath())
		log.Printf("manager: [debug] tmux session: %s", s.config.Session)
	}

	opts := tmux.SessionOpts{
		Name:    s.config.Session,
		Command: bin,
		Args:    args,
		Env:     s.env,
		Workdir: s.config.Workdir,
	}

	if err := s.tmux.CreateSession(opts); err != nil {
		return err
	}

	s.mu.Lock()
	s.running = true
	s.lastErr = ""
	s.mu.Unlock()

	log.Printf("manager: agent %q launched in tmux session %q", s.config.Name, s.config.Session)
	return nil
}

// stopAgent gracefully stops the running agent.
func (s *Manager) stopAgent() error {
	s.mu.Lock()
	wasRunning := s.running
	s.mu.Unlock()

	if !wasRunning {
		return nil
	}

	sessionName := s.config.Session

	// Check if the session is still actually running.
	running, err := s.tmux.IsSessionRunning(sessionName)
	if err != nil {
		log.Printf("manager: error checking session %q: %v", sessionName, err)
	}
	if !running {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		return nil
	}

	grace, err := s.config.GraceDuration()
	if err != nil {
		grace = 30 * time.Second
	}

	log.Printf("manager: sending C-c to agent session %q, grace period %s", sessionName, grace)

	// Send C-c (SIGINT) to the tmux session.
	if err := s.tmux.SendKeys(sessionName, "C-c"); err != nil {
		log.Printf("manager: error sending C-c to session %q: %v", sessionName, err)
	}

	// Wait for the session to exit within the grace period.
	exitCh := make(chan struct{})
	go func() {
		_ = s.tmux.WaitForExit(sessionName, time.Second)
		close(exitCh)
	}()

	select {
	case <-exitCh:
		log.Printf("manager: agent session %q exited gracefully", sessionName)
	case <-time.After(grace):
		log.Printf("manager: grace period expired, destroying session %q", sessionName)
		if err := s.tmux.DestroySession(sessionName); err != nil {
			log.Printf("manager: error destroying session %q: %v", sessionName, err)
		}
	}

	s.mu.Lock()
	s.running = false
	s.mu.Unlock()

	return nil
}

// run monitors the agent and handles restarts per the
// configured restart policy.
func (s *Manager) run(ctx context.Context) {
	defer close(s.done)

	for {
		// Wait for the agent to exit or context to be cancelled.
		exitCh := make(chan struct{})
		go func() {
			_ = s.tmux.WaitForExit(s.config.Session, defaultPollInterval)
			close(exitCh)
		}()

		select {
		case <-ctx.Done():
			// Shutdown requested — the Stop() method handles cleanup.
			return

		case <-exitCh:
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()

			log.Printf("manager: agent %q exited", s.config.Name)

			// Evaluate restart policy.
			if !s.shouldRestart() {
				log.Printf("manager: not restarting agent %q (policy=%s, restarts=%d)",
					s.config.Name, s.config.Restart, s.restarts)
				return
			}

			s.restarts++
			log.Printf("manager: restarting agent %q (attempt %d)", s.config.Name, s.restarts)

			// Backoff before restart.
			select {
			case <-ctx.Done():
				return
			case <-time.After(restartBackoff):
			}

			if err := s.launchAgent(); err != nil {
				log.Printf("manager: failed to restart agent %q: %v", s.config.Name, err)
				s.mu.Lock()
				s.lastErr = err.Error()
				s.mu.Unlock()
				return
			}
		}
	}
}

// shouldRestart evaluates whether the agent should be restarted based
// on the configured restart policy and restart count.
func (s *Manager) shouldRestart() bool {
	switch s.config.Restart {
	case config.RestartNo:
		return false

	case config.RestartOnFailure:
		// We can't easily get the exit code from tmux, so we restart
		// on any exit when policy is "on-failure". In practice, the
		// harness exiting normally (exit 0) is indistinguishable from
		// a tmux session ending when checking via has-session.
		// Future improvement: capture exit codes via tmux pipe-pane.
		if s.config.MaxRestarts > 0 && s.restarts >= s.config.MaxRestarts {
			return false
		}
		return true

	case config.RestartAlways:
		if s.config.MaxRestarts > 0 && s.restarts >= s.config.MaxRestarts {
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
