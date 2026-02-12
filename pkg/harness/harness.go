// Package harness defines the interface for agent harnesses and provides
// implementations for built-in harnesses (Claude Code, OpenCode, Gemini CLI)
// as well as custom harnesses.
//
// A harness knows how to construct the command line for launching a specific
// AI agent tool.
package harness

import "fmt"

// Harness represents an agent harness that can be launched in a tmux session.
type Harness interface {
	// Name returns the harness identifier (e.g., "claude-code").
	Name() string

	// BuildCommand returns the binary and arguments to launch this harness.
	// The prompt is the resolved prompt string — it may be empty for
	// interactive mode.
	BuildCommand(prompt string) (bin string, args []string)
}

// registry maps harness names to constructor functions.
var registry = map[string]func() Harness{
	"claude-code": func() Harness { return &ClaudeCode{} },
	"opencode":    func() Harness { return &OpenCode{} },
	"gemini-cli":  func() Harness { return &GeminiCLI{} },
	"custom":      func() Harness { return &Custom{} },
}

// Get returns a Harness implementation for the given name.
func Get(name string) (Harness, error) {
	ctor, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown harness %q", name)
	}
	return ctor(), nil
}

// Names returns the list of all registered harness names.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
