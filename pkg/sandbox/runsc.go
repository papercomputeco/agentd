package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

const (
	// DefaultPlatform is the gVisor platform used for sandboxes.
	// systrap is the fastest on modern kernels.
	DefaultPlatform = "systrap"
)

// ContainerState represents the JSON output of runsc state.
type ContainerState struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	PID    int    `json:"pid"`
	Bundle string `json:"bundle"`
}

// Runner wraps the runsc binary for container lifecycle management.
type Runner struct {
	// RunscPath is the path to the runsc binary.
	RunscPath string

	// StateDir is the root directory for runsc state (--root flag).
	StateDir string

	// Platform is the gVisor platform to use (default: systrap).
	Platform string

	// Debug enables verbose logging of runsc commands.
	Debug bool
}

// NewRunner creates a new runsc runner. If runscPath is empty, it
// attempts to find runsc in PATH.
func NewRunner(runscPath, stateDir string) (*Runner, error) {
	if runscPath == "" {
		var err error
		runscPath, err = exec.LookPath("runsc")
		if err != nil {
			return nil, fmt.Errorf("runsc not found in PATH: %w", err)
		}
	}

	return &Runner{
		RunscPath: runscPath,
		StateDir:  stateDir,
		Platform:  DefaultPlatform,
	}, nil
}

// baseArgs returns the common arguments for all runsc commands.
func (r *Runner) baseArgs() []string {
	return []string{
		"--root", r.StateDir,
		"--platform=" + r.Platform,
		"--directfs=true",
		"--network=host",
	}
}

// Run creates and starts a sandbox in detached mode. This is equivalent
// to runsc create + runsc start in a single operation.
//
// The detached runsc process forks the sandbox and then exits. We send
// stdout to /dev/null to prevent the forked sandbox from holding the
// pipe open (which would cause CombinedOutput to block indefinitely).
// Stderr is captured for error reporting.
func (r *Runner) Run(ctx context.Context, id, bundleDir string) error {
	args := r.baseArgs()
	args = append(args, "run", "--detach", "--bundle", bundleDir, id)

	if r.Debug {
		log.Printf("sandbox: runsc %s", strings.Join(args, " "))
	}

	// Use a temporary file for stderr so we can capture error output
	// without pipes. Pipes would be inherited by the detached sandbox
	// child process, causing cmd.Wait() to block indefinitely.
	stderrFile, err := os.CreateTemp("", "runsc-stderr-*")
	if err != nil {
		return fmt.Errorf("creating stderr temp file: %w", err)
	}
	defer os.Remove(stderrFile.Name())
	defer stderrFile.Close()

	devNull, err2 := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err2 != nil {
		return fmt.Errorf("opening /dev/null: %w", err2)
	}
	defer devNull.Close()

	cmd := exec.CommandContext(ctx, r.RunscPath, args...)
	cmd.Stdout = devNull
	cmd.Stderr = stderrFile

	if err := cmd.Run(); err != nil {
		// Read captured stderr for diagnostics.
		var stderrOutput string
		if _, seekErr := stderrFile.Seek(0, 0); seekErr == nil {
			if data, readErr := os.ReadFile(stderrFile.Name()); readErr == nil {
				stderrOutput = string(data)
			}
		}
		return fmt.Errorf("runsc run %s: %w\nstderr: %s", id, err, stderrOutput)
	}

	return nil
}

// Kill sends a signal to the sandbox's init process.
func (r *Runner) Kill(ctx context.Context, id, signal string) error {
	args := r.baseArgs()
	args = append(args, "kill", id, signal)

	if r.Debug {
		log.Printf("sandbox: runsc %s", strings.Join(args, " "))
	}

	cmd := exec.CommandContext(ctx, r.RunscPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("runsc kill %s %s: %w\noutput: %s", id, signal, err, string(output))
	}

	return nil
}

// Delete removes a sandbox. The sandbox must be stopped first.
func (r *Runner) Delete(ctx context.Context, id string) error {
	args := r.baseArgs()
	args = append(args, "delete", id)

	if r.Debug {
		log.Printf("sandbox: runsc %s", strings.Join(args, " "))
	}

	cmd := exec.CommandContext(ctx, r.RunscPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("runsc delete %s: %w\noutput: %s", id, err, string(output))
	}

	return nil
}

// State queries the current state of a sandbox.
func (r *Runner) State(ctx context.Context, id string) (*ContainerState, error) {
	args := r.baseArgs()
	args = append(args, "state", id)

	cmd := exec.CommandContext(ctx, r.RunscPath, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("runsc state %s: %w", id, err)
	}

	var state ContainerState
	if err := json.Unmarshal(output, &state); err != nil {
		return nil, fmt.Errorf("parsing runsc state output: %w", err)
	}

	return &state, nil
}

// IsRunning checks whether a sandbox is currently running.
func (r *Runner) IsRunning(ctx context.Context, id string) (bool, error) {
	state, err := r.State(ctx, id)
	if err != nil {
		// If the container doesn't exist, it's not running.
		return false, nil
	}
	return state.Status == "running", nil
}

// Exec runs a command inside a running sandbox. This is used for admin
// access (equivalent to tmux attach for native agents).
func (r *Runner) Exec(ctx context.Context, id string, cmd []string) error {
	args := r.baseArgs()
	args = append(args, "exec", id)
	args = append(args, cmd...)

	if r.Debug {
		log.Printf("sandbox: runsc %s", strings.Join(args, " "))
	}

	c := exec.CommandContext(ctx, r.RunscPath, args...)
	output, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("runsc exec %s: %w\noutput: %s", id, err, string(output))
	}

	return nil
}

// Cleanup deletes any orphaned containers that may exist from a previous
// agentd crash. This should be called on daemon startup.
func (r *Runner) Cleanup(ctx context.Context) error {
	args := r.baseArgs()
	args = append(args, "list", "--format=json")

	cmd := exec.CommandContext(ctx, r.RunscPath, args...)
	output, err := cmd.Output()
	if err != nil {
		// If runsc list fails (e.g. no state dir yet), that's fine.
		return nil
	}

	var containers []ContainerState
	if err := json.Unmarshal(output, &containers); err != nil {
		// Might be empty or malformed; not critical.
		return nil
	}

	for _, c := range containers {
		if c.Status == "stopped" || c.Status == "created" {
			log.Printf("sandbox: cleaning up orphaned container %s (status=%s)", c.ID, c.Status)
			_ = r.Delete(ctx, c.ID)
		}
	}

	return nil
}
