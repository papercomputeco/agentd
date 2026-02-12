package harness

// ClaudeCode implements the Harness interface for Anthropic's Claude Code CLI.
// Claude Code accepts a prompt as a positional argument:
//
//	claude "review the code and fix tests"
type ClaudeCode struct{}

// Name returns "claude-code".
func (c *ClaudeCode) Name() string {
	return "claude-code"
}

// BuildCommand returns the claude binary and arguments.
// In non-interactive mode, the prompt is passed with the -p flag.
func (c *ClaudeCode) BuildCommand(prompt string) (string, []string) {
	if prompt == "" {
		return "claude", nil
	}
	return "claude", []string{"-p", prompt}
}
