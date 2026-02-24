// Package config handles parsing and validation of the [agent] section
// from jcard.toml configuration files used by agentd.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// RestartPolicy defines the agent restart behavior.
type RestartPolicy string

const (
	// RestartNo means the agent is not restarted when it exits.
	RestartNo RestartPolicy = "no"

	// RestartOnFailure restarts the agent only on non-zero exit codes.
	RestartOnFailure RestartPolicy = "on-failure"

	// RestartAlways restarts the agent unconditionally when it exits.
	RestartAlways RestartPolicy = "always"
)

const (
	// DefaultWorkdir is the default working directory for agents.
	DefaultWorkdir = "/home/agent/workspace"

	// DefaultGracePeriod is the default SIGTERM-to-SIGKILL grace period.
	DefaultGracePeriod = "30s"

	// DefaultRestart is the default restart policy.
	DefaultRestart = RestartNo
)

// harnessSet are the built-in harnesses.
var harnessSet = map[string]bool{
	"claude-code": true,
	"opencode":    true,
	"gemini-cli":  true,
	"custom":      true,
}

// AgentConfig represents the [agent] section of a jcard.toml file.
type AgentConfig struct {
	// Harness is the agent harness to use.
	// Built-in harnesses: "claude-code", "opencode", "gemini-cli", "custom".
	Harness string `toml:"harness"`

	// Prompt is the prompt or command to give the agent on boot.
	// If empty, the harness starts in interactive mode.
	Prompt string `toml:"prompt,omitempty"`

	// PromptFile is a path to a file containing the prompt.
	// Takes precedence over Prompt when set.
	PromptFile string `toml:"prompt_file,omitempty"`

	// Workdir is the working directory inside the sandbox where the agent starts.
	// Defaults to /home/agent/workspace.
	Workdir string `toml:"workdir,omitempty"`

	// Restart defines the restart policy for the agent.
	// "no", "on-failure", or "always". Defaults to "no".
	Restart RestartPolicy `toml:"restart,omitempty"`

	// MaxRestarts is the maximum number of restart attempts before giving up.
	// Only applies when Restart != "no". 0 means unlimited.
	MaxRestarts int `toml:"max_restarts,omitempty"`

	// Timeout is the maximum duration for the agent to complete.
	// After this duration, agentd sends SIGTERM. Unset means no timeout.
	Timeout string `toml:"timeout,omitempty"`

	// GracePeriod is the duration between SIGTERM and SIGKILL on shutdown.
	// Defaults to "30s".
	GracePeriod string `toml:"grace_period,omitempty"`

	// Session is the tmux session name for this agent.
	// Defaults to the harness name.
	Session string `toml:"session,omitempty"`

	// Env holds environment variables set only for the agent process.
	Env map[string]string `toml:"env,omitempty"`
}

// jcardFile is the top-level structure of a jcard.toml, used for partial
// parsing. We only care about the [agent] section.
type jcardFile struct {
	Agent AgentConfig `toml:"agent"`
}

// LoadConfig reads and parses the [agent] section from a jcard.toml file
// at the given path. It applies defaults and validates the configuration.
func LoadConfig(path string) (*AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	return ParseConfig(string(data))
}

// ParseConfig parses the [agent] section from TOML content, applies
// defaults, and validates the configuration.
func ParseConfig(content string) (*AgentConfig, error) {
	var jcard jcardFile
	if _, err := toml.Decode(content, &jcard); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg := &jcard.Agent
	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// applyDefaults fills in default values for unset fields.
func (c *AgentConfig) applyDefaults() {
	if c.Workdir == "" {
		c.Workdir = DefaultWorkdir
	}
	if c.Restart == "" {
		c.Restart = DefaultRestart
	}
	if c.GracePeriod == "" {
		c.GracePeriod = DefaultGracePeriod
	}
	if c.Session == "" {
		c.Session = c.Harness
	}
	if c.Env == nil {
		c.Env = make(map[string]string)
	}
}

// Validate checks the configuration for errors.
func (c *AgentConfig) Validate() error {
	if c.Harness == "" {
		return fmt.Errorf("agent.harness is required")
	}
	if !harnessSet[c.Harness] {
		return fmt.Errorf("unknown agent.harness %q: must be one of claude-code, opencode, gemini-cli, custom", c.Harness)
	}

	switch c.Restart {
	case RestartNo, RestartOnFailure, RestartAlways:
		// valid
	default:
		return fmt.Errorf("invalid agent.restart %q: must be no, on-failure, or always", c.Restart)
	}

	if c.MaxRestarts < 0 {
		return fmt.Errorf("agent.max_restarts must be >= 0, got %d", c.MaxRestarts)
	}

	if c.Timeout != "" {
		if _, err := time.ParseDuration(c.Timeout); err != nil {
			return fmt.Errorf("invalid agent.timeout %q: %w", c.Timeout, err)
		}
	}

	if c.GracePeriod != "" {
		if _, err := time.ParseDuration(c.GracePeriod); err != nil {
			return fmt.Errorf("invalid agent.grace_period %q: %w", c.GracePeriod, err)
		}
	}

	return nil
}

// ResolvePrompt returns the prompt string for the agent. If PromptFile is set,
// it reads the file and returns its contents (taking precedence over Prompt).
// If neither is set, returns an empty string (interactive mode).
func (c *AgentConfig) ResolvePrompt() (string, error) {
	if c.PromptFile != "" {
		data, err := os.ReadFile(c.PromptFile)
		if err != nil {
			return "", fmt.Errorf("reading prompt file %s: %w", c.PromptFile, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	return c.Prompt, nil
}

// TimeoutDuration parses and returns the timeout as a time.Duration.
// Returns 0 if no timeout is set.
func (c *AgentConfig) TimeoutDuration() (time.Duration, error) {
	if c.Timeout == "" {
		return 0, nil
	}
	return time.ParseDuration(c.Timeout)
}

// GraceDuration parses and returns the grace period as a time.Duration.
func (c *AgentConfig) GraceDuration() (time.Duration, error) {
	if c.GracePeriod == "" {
		return 30 * time.Second, nil
	}
	return time.ParseDuration(c.GracePeriod)
}
