package harness

// OpenCode implements the Harness interface for the OpenCode CLI.
// OpenCode accepts a prompt with the -m flag:
//
//	opencode -m "review the code and fix tests"
type OpenCode struct{}

// Name returns "opencode".
func (o *OpenCode) Name() string {
	return "opencode"
}

// BuildCommand returns the opencode binary and arguments.
func (o *OpenCode) BuildCommand(prompt string) (string, []string) {
	if prompt == "" {
		return "opencode", nil
	}
	return "opencode", []string{"--prompt", prompt}
}
