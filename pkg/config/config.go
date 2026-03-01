// Package config handles parsing and validation of the [[agents]] section
// from jcard.toml configuration files used by agentd.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// AgentType defines how the agent process is executed.
type AgentType string

const (
	// AgentTypeSandboxed runs the agent in a gVisor (runsc) sandbox with
	// read-only /nix/store bind mounts and a writable tmpfs overlay.
	AgentTypeSandboxed AgentType = "sandboxed"

	// AgentTypeNative runs the agent directly on the host in a tmux
	// session as the agent user (the original agentd behavior).
	AgentTypeNative AgentType = "native"
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

	// DefaultAgentType is the default agent execution mode.
	DefaultAgentType = AgentTypeSandboxed

	// DefaultMemory is the default memory limit for sandboxed agents.
	DefaultMemory = "2GiB"

	// DefaultPidLimit is the default PID limit for sandboxed agents.
	DefaultPidLimit = 512
)

// harnessSet are the built-in harnesses.
var harnessSet = map[string]bool{
	"claude-code": true,
	"opencode":    true,
	"gemini-cli":  true,
	"custom":      true,
}

// AgentConfig represents a single entry in the [[agents]] array of a jcard.toml file.
type AgentConfig struct {
	// Name is a unique identifier for this agent. If omitted, a name is
	// auto-generated from the harness name (e.g. "claude-code", "claude-code-1").
	Name string `toml:"name,omitempty"`

	// Type selects the agent execution mode.
	// "sandboxed" (default) runs in a gVisor container with /nix/store sharing.
	// "native" runs directly on the host in a tmux session.
	Type AgentType `toml:"type,omitempty"`

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
	// Defaults to the harness name. Only used for native agents.
	Session string `toml:"session,omitempty"`

	// Memory is the memory limit for sandboxed agents (e.g. "2GiB", "512MiB").
	// Ignored for native agents. Defaults to "2GiB".
	Memory string `toml:"memory,omitempty"`

	// PidLimit is the maximum number of processes inside a sandboxed agent.
	// Ignored for native agents. Defaults to 512.
	PidLimit int `toml:"pid_limit,omitempty"`

	// ExtraPackages is a list of additional Nix package attribute names
	// to install into the sandbox (e.g. ["ripgrep", "fd", "python311"]).
	// These are resolved against the system's nixpkgs and materialized
	// into /nix/store at agent launch time. Only used for sandboxed agents.
	ExtraPackages []string `toml:"extra_packages,omitempty"`

	// Replicas is the number of identical agents to launch from this
	// spec. Defaults to 1. When > 1, each replica gets a unique name
	// suffixed with its index (e.g. "reviewer-0", "reviewer-1").
	// Useful for launching swarms of agents performing the same task.
	Replicas int `toml:"replicas,omitempty"`

	// Env holds environment variables set only for the agent process.
	Env map[string]string `toml:"env,omitempty"`
}

// jcardFile is the top-level structure of a jcard.toml, used for partial
// parsing. We only care about the [[agents]] section.
type jcardFile struct {
	Agents []AgentConfig `toml:"agents"`
}

// LoadConfig reads and parses the [[agents]] section from a jcard.toml file
// at the given path. It applies defaults and validates the configuration.
func LoadConfig(path string) ([]AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	return ParseConfig(string(data))
}

// ParseConfig parses the [[agents]] section from TOML content, applies
// defaults, and validates each agent configuration.
func ParseConfig(content string) ([]AgentConfig, error) {
	var jcard jcardFile
	if _, err := toml.Decode(content, &jcard); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Apply defaults, expand replicas, then assign names.
	for i := range jcard.Agents {
		jcard.Agents[i].applyDefaults()
	}
	jcard.Agents = expandReplicas(jcard.Agents)
	assignAgentNames(jcard.Agents)

	// Validate each agent.
	namesSeen := make(map[string]bool, len(jcard.Agents))
	for i := range jcard.Agents {
		if err := jcard.Agents[i].Validate(); err != nil {
			return nil, fmt.Errorf("agents[%d]: %w", i, err)
		}
		if namesSeen[jcard.Agents[i].Name] {
			return nil, fmt.Errorf("agents[%d]: duplicate agent name %q", i, jcard.Agents[i].Name)
		}
		namesSeen[jcard.Agents[i].Name] = true
	}

	return jcard.Agents, nil
}

// applyDefaults fills in default values for unset fields.
// Note: Name and Session are finalized after assignAgentNames runs,
// since Name may be auto-generated from the harness.
func (c *AgentConfig) applyDefaults() {
	if c.Type == "" {
		c.Type = DefaultAgentType
	}
	if c.Workdir == "" {
		c.Workdir = DefaultWorkdir
	}
	if c.Restart == "" {
		c.Restart = DefaultRestart
	}
	if c.GracePeriod == "" {
		c.GracePeriod = DefaultGracePeriod
	}
	if c.Type == AgentTypeSandboxed {
		if c.Memory == "" {
			c.Memory = DefaultMemory
		}
		if c.PidLimit == 0 {
			c.PidLimit = DefaultPidLimit
		}
	}
	if c.Replicas <= 0 {
		c.Replicas = 1
	}
	if c.Env == nil {
		c.Env = make(map[string]string)
	}
}

// expandReplicas expands agent entries with Replicas > 1 into individual
// agent entries. Each replica is a copy of the original with a unique
// name suffix. For replicas=1, the entry is left unchanged.
//
// Naming rules:
//   - replicas=1, name="rev"   -> "rev" (unchanged)
//   - replicas=3, name="rev"   -> "rev-0", "rev-1", "rev-2"
//   - replicas=3, name=""      -> name left empty (assignAgentNames handles it later)
func expandReplicas(agents []AgentConfig) []AgentConfig {
	// Fast path: if all agents have replicas=1, return as-is.
	needsExpansion := false
	total := 0
	for i := range agents {
		if agents[i].Replicas > 1 {
			needsExpansion = true
		}
		total += agents[i].Replicas
	}
	if !needsExpansion {
		return agents
	}

	expanded := make([]AgentConfig, 0, total)
	for _, a := range agents {
		if a.Replicas <= 1 {
			expanded = append(expanded, a)
			continue
		}

		baseName := a.Name
		for j := 0; j < a.Replicas; j++ {
			replica := a
			replica.Replicas = 1
			if baseName != "" {
				replica.Name = fmt.Sprintf("%s-%d", baseName, j)
			}
			// If baseName is empty, leave Name empty — assignAgentNames
			// will handle it and produce unique names from the harness.
			// Session is also left empty so it defaults to the final name.
			replica.Session = ""
			// Deep-copy the env map so replicas don't share a reference.
			if a.Env != nil {
				replica.Env = make(map[string]string, len(a.Env))
				for k, v := range a.Env {
					replica.Env[k] = v
				}
			}
			expanded = append(expanded, replica)
		}
	}
	return expanded
}

// assignAgentNames fills in Name and Session for agents that don't have them set.
// The first agent with a given harness gets the harness name (e.g. "claude-code").
// Subsequent unnamed agents with the same harness get "<harness>-1", "<harness>-2", etc.
func assignAgentNames(agents []AgentConfig) {
	// Count how many unnamed agents use each harness.
	harnessCount := make(map[string]int)
	for i := range agents {
		if agents[i].Name == "" {
			harnessCount[agents[i].Harness]++
		}
	}

	// Assign names.
	harnessIdx := make(map[string]int)
	for i := range agents {
		if agents[i].Name == "" {
			h := agents[i].Harness
			idx := harnessIdx[h]
			harnessIdx[h]++

			if harnessCount[h] == 1 {
				agents[i].Name = h
			} else {
				agents[i].Name = fmt.Sprintf("%s-%d", h, idx)
			}
		}

		// Default session to agent name (not harness) for uniqueness.
		if agents[i].Session == "" {
			agents[i].Session = agents[i].Name
		}
	}
}

// Validate checks the configuration for errors.
func (c *AgentConfig) Validate() error {
	switch c.Type {
	case AgentTypeSandboxed, AgentTypeNative:
		// valid
	default:
		return fmt.Errorf("invalid agent.type %q: must be sandboxed or native", c.Type)
	}

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

	// Sandbox-specific validation.
	if c.Type == AgentTypeSandboxed {
		if c.Memory != "" {
			if _, err := ParseMemory(c.Memory); err != nil {
				return fmt.Errorf("invalid agent.memory %q: %w", c.Memory, err)
			}
		}
		if c.PidLimit < 0 {
			return fmt.Errorf("agent.pid_limit must be >= 0, got %d", c.PidLimit)
		}
		for i, pkg := range c.ExtraPackages {
			if strings.TrimSpace(pkg) == "" {
				return fmt.Errorf("agent.extra_packages[%d] is empty", i)
			}
		}
	}

	// extra_packages is only valid for sandboxed agents.
	if c.Type != AgentTypeSandboxed && len(c.ExtraPackages) > 0 {
		return fmt.Errorf("agent.extra_packages is only supported for type=sandboxed")
	}

	if c.Replicas < 1 {
		return fmt.Errorf("agent.replicas must be >= 1, got %d", c.Replicas)
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

// MemoryBytes parses the Memory field and returns the value in bytes.
// Returns 0 if no memory limit is set.
func (c *AgentConfig) MemoryBytes() (int64, error) {
	if c.Memory == "" {
		return 0, nil
	}
	return ParseMemory(c.Memory)
}

// ParseMemory parses a human-readable memory string into bytes.
// Supported suffixes: KiB, MiB, GiB, KB, MB, GB (case-insensitive).
// Plain integers are treated as bytes.
func ParseMemory(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty memory string")
	}

	// Try parsing as plain integer (bytes).
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n < 0 {
			return 0, fmt.Errorf("memory must be non-negative, got %d", n)
		}
		return n, nil
	}

	// Find where the numeric part ends and the suffix begins.
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("invalid memory format %q: no numeric value", s)
	}

	numStr := s[:i]
	suffix := strings.ToLower(strings.TrimSpace(s[i:]))

	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory format %q: %w", s, err)
	}
	if num < 0 {
		return 0, fmt.Errorf("memory must be non-negative, got %s", s)
	}

	var multiplier float64
	switch suffix {
	case "kib":
		multiplier = 1024
	case "mib":
		multiplier = 1024 * 1024
	case "gib":
		multiplier = 1024 * 1024 * 1024
	case "kb":
		multiplier = 1000
	case "mb":
		multiplier = 1000 * 1000
	case "gb":
		multiplier = 1000 * 1000 * 1000
	default:
		return 0, fmt.Errorf("unknown memory suffix %q in %q: use KiB, MiB, GiB, KB, MB, or GB", suffix, s)
	}

	return int64(num * multiplier), nil
}
