// Package supervisor manages the lifecycle of an agent process running
// in a tmux session. It handles starting the agent, monitoring its health,
// implementing restart policies (no, on-failure, always), enforcing
// timeouts, and coordinating graceful shutdown.
package supervisor

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/papercomputeco/agentd/pkg/api"
	"github.com/papercomputeco/agentd/pkg/config"
	"github.com/papercomputeco/agentd/pkg/harness"
	"github.com/papercomputeco/agentd/pkg/tmux"
)

const (
	// defaultPollInterval is how often the supervisor checks if the
	// agent's tmux session is still running.
	defaultPollInterval = 2 * time.Second

	// restartBackoff is the delay between restart attempts.
	restartBackoff = 3 * time.Second
)

// Opts holds configuration for creating a new Supervisor.
type Opts struct {
	Config  *config.AgentConfig
	Harness harness.Harness
	Tmux    *tmux.Server
	Env     map[string]string // merged secrets + agent env
	Prompt  string            // resolved prompt
}

// Supervisor manages a single agent process lifecycle.
type Supervisor struct {
	config  *config.AgentConfig
	harness harness.Harness
	tmux    *tmux.Server
	env     map[string]string
	prompt  string

	mu       sync.Mutex
	running  bool
	restarts int
	lastErr  string
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewSupervisor creates a new supervisor with the given options.
func NewSupervisor(opts Opts) *Supervisor {
	return &Supervisor{
		config:  opts.Config,
		harness: opts.Harness,
		tmux:    opts.Tmux,
		env:     opts.Env,
		prompt:  opts.Prompt,
		done:    make(chan struct{}),
	}
}

// Start launches the agent and begins the supervision loop. It runs
// until the context is cancelled or the agent exits and the restart
// policy does not call for a restart.
func (s *Supervisor) Start(ctx context.Context) error {
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

	// Start the supervision loop in a goroutine.
	go s.supervisionLoop(ctx)

	return nil
}

// Stop gracefully stops the agent. It sends SIGINT (C-c) to the tmux
// session, waits the grace period, and then forcibly destroys the session.
func (s *Supervisor) Stop() error {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()

	// Wait for the supervision loop to finish.
	<-s.done

	return s.stopAgent()
}

// IsRunning returns whether the agent process is currently running.
func (s *Supervisor) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Status returns the current agent status suitable for the API.
func (s *Supervisor) Status() api.AgentStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return api.AgentStatus{
		Name:     s.harness.Name(),
		Running:  s.running,
		Session:  s.config.Session,
		Restarts: s.restarts,
		Error:    s.lastErr,
	}
}

// Restarts returns the number of times the agent has been restarted.
func (s *Supervisor) Restarts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restarts
}

// launchAgent creates a tmux session with the agent harness command.
func (s *Supervisor) launchAgent() error {
	bin, args := s.harness.BuildCommand(s.prompt)

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

	log.Printf("supervisor: agent %q launched in tmux session %q", s.harness.Name(), s.config.Session)
	return nil
}

// stopAgent gracefully stops the running agent.
func (s *Supervisor) stopAgent() error {
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
		log.Printf("supervisor: error checking session %q: %v", sessionName, err)
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

	log.Printf("supervisor: sending C-c to agent session %q, grace period %s", sessionName, grace)

	// Send C-c (SIGINT) to the tmux session.
	if err := s.tmux.SendKeys(sessionName, "C-c"); err != nil {
		log.Printf("supervisor: error sending C-c to session %q: %v", sessionName, err)
	}

	// Wait for the session to exit within the grace period.
	exitCh := make(chan struct{})
	go func() {
		_ = s.tmux.WaitForExit(sessionName, time.Second)
		close(exitCh)
	}()

	select {
	case <-exitCh:
		log.Printf("supervisor: agent session %q exited gracefully", sessionName)
	case <-time.After(grace):
		log.Printf("supervisor: grace period expired, destroying session %q", sessionName)
		if err := s.tmux.DestroySession(sessionName); err != nil {
			log.Printf("supervisor: error destroying session %q: %v", sessionName, err)
		}
	}

	s.mu.Lock()
	s.running = false
	s.mu.Unlock()

	return nil
}

// supervisionLoop monitors the agent and handles restarts per the
// configured restart policy.
func (s *Supervisor) supervisionLoop(ctx context.Context) {
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

			log.Printf("supervisor: agent %q exited", s.harness.Name())

			// Evaluate restart policy.
			if !s.shouldRestart() {
				log.Printf("supervisor: not restarting agent %q (policy=%s, restarts=%d)",
					s.harness.Name(), s.config.Restart, s.restarts)
				return
			}

			s.restarts++
			log.Printf("supervisor: restarting agent %q (attempt %d)", s.harness.Name(), s.restarts)

			// Backoff before restart.
			select {
			case <-ctx.Done():
				return
			case <-time.After(restartBackoff):
			}

			if err := s.launchAgent(); err != nil {
				log.Printf("supervisor: failed to restart agent %q: %v", s.harness.Name(), err)
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
func (s *Supervisor) shouldRestart() bool {
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
