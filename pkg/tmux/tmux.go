// Package tmux manages tmux server and sessions for agent harnesses.
// agentd runs each agent in its own tmux session, allowing the admin user
// to "tmux attach -t <session>" to observe or interact with running agents.
package tmux

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SessionOpts configures a new tmux session.
type SessionOpts struct {
	// Name is the tmux session name.
	Name string

	// Command is the binary to run in the session.
	Command string

	// Args are the arguments to the command.
	Args []string

	// Env holds environment variables for the session.
	Env map[string]string

	// Workdir is the working directory for the session.
	Workdir string
}

// Server manages a tmux server instance using a dedicated socket,
// keeping agentd's tmux sessions isolated from user tmux sessions.
type Server struct {
	socketPath string
}

// NewServer creates a new tmux server manager. The socketPath is the path
// to the tmux server socket (e.g., "/run/stereos/agentd-tmux.sock").
func NewServer(socketPath string) *Server {
	return &Server{socketPath: socketPath}
}

// SocketPath returns the tmux server socket path.
func (s *Server) SocketPath() string {
	return s.socketPath
}

// Start launches the tmux server. tmux servers start implicitly when the
// first session is created, so this is a no-op that verifies tmux is
// available on the system.
func (s *Server) Start() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not found in PATH: %w", err)
	}
	return nil
}

// Stop kills the tmux server and all its sessions.
func (s *Server) Stop() error {
	cmd := exec.Command("tmux", "-S", s.socketPath, "kill-server")
	// Ignore errors — the server may already be stopped.
	_ = cmd.Run()
	return nil
}

// CreateSession creates a new tmux session with the given options.
// The session runs the specified command with its arguments.
func (s *Server) CreateSession(opts SessionOpts) error {
	if opts.Name == "" {
		return fmt.Errorf("session name is required")
	}
	if opts.Command == "" {
		return fmt.Errorf("session command is required")
	}

	// Build the full shell command string. We use shell execution so that
	// environment variables and the full command line are properly handled.
	var shellCmd string
	if len(opts.Args) > 0 {
		shellCmd = opts.Command + " " + shelljoin(opts.Args)
	} else {
		shellCmd = opts.Command
	}

	args := []string{
		"-S", s.socketPath,
		"new-session",
		"-d",            // detached
		"-s", opts.Name, // session name
	}

	if opts.Workdir != "" {
		args = append(args, "-c", opts.Workdir)
	}

	// Set environment variables via tmux's -e flag (tmux 3.2+).
	for k, v := range opts.Env {
		args = append(args, "-e", k+"="+v)
	}

	args = append(args, shellCmd)

	cmd := exec.Command("tmux", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("creating tmux session %q: %w: %s", opts.Name, err, string(output))
	}

	return nil
}

// DestroySession kills a tmux session by name.
func (s *Server) DestroySession(name string) error {
	cmd := exec.Command("tmux", "-S", s.socketPath, "kill-session", "-t", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("destroying tmux session %q: %w: %s", name, err, string(output))
	}
	return nil
}

// ListSessions returns the names of all active tmux sessions.
func (s *Server) ListSessions() ([]string, error) {
	cmd := exec.Command("tmux", "-S", s.socketPath, "list-sessions", "-F", "#{session_name}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		outStr := string(output)
		// These error messages mean the server or sessions don't exist yet,
		// which is a valid state (not an error).
		if strings.Contains(outStr, "no server running") ||
			strings.Contains(outStr, "no sessions") ||
			strings.Contains(outStr, "No such file or directory") ||
			strings.Contains(outStr, "error connecting to") {
			return nil, nil
		}
		return nil, fmt.Errorf("listing tmux sessions: %w: %s", err, outStr)
	}

	lines := strings.TrimSpace(string(output))
	if lines == "" {
		return nil, nil
	}

	return strings.Split(lines, "\n"), nil
}

// IsSessionRunning checks if a tmux session with the given name exists.
func (s *Server) IsSessionRunning(name string) (bool, error) {
	cmd := exec.Command("tmux", "-S", s.socketPath, "has-session", "-t", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		outStr := string(output)
		// Exit code 1 means session doesn't exist — not an error.
		// Also handle the case where the tmux server socket doesn't exist yet.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		if strings.Contains(outStr, "No such file or directory") ||
			strings.Contains(outStr, "error connecting to") ||
			strings.Contains(outStr, "no server running") {
			return false, nil
		}
		return false, fmt.Errorf("checking tmux session %q: %w", name, err)
	}
	return true, nil
}

// WaitForExit blocks until the named session no longer exists,
// polling at the given interval. Returns nil when the session exits.
// Returns an error if the context is cancelled or polling fails.
func (s *Server) WaitForExit(name string, pollInterval time.Duration) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		<-ticker.C
		running, err := s.IsSessionRunning(name)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
	}
}

// SendKeys sends keystrokes to a tmux session. This is used to send
// signals like C-c (SIGINT) to the running process.
func (s *Server) SendKeys(name string, keys string) error {
	cmd := exec.Command("tmux", "-S", s.socketPath, "send-keys", "-t", name, keys)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sending keys to tmux session %q: %w: %s", name, err, string(output))
	}
	return nil
}

// shelljoin quotes arguments for shell execution.
func shelljoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsAny(arg, " \t\n\"'\\$`!#&|;(){}[]<>?*~") {
			quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
		} else {
			quoted[i] = arg
		}
	}
	return strings.Join(quoted, " ")
}
