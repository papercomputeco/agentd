package harness

// Custom implements the Harness interface for user-defined agent harnesses.
// When harness = "custom" is set in jcard.toml, the harness binary name
// comes from the session name or can be overridden via environment variables.
//
// For custom harnesses, the prompt (if any) is passed as a positional argument.
type Custom struct {
	// BinaryName is the name of the custom binary to execute.
	// If empty, defaults to "agent".
	BinaryName string
}

// Name returns "custom".
func (c *Custom) Name() string {
	return "custom"
}

// BuildCommand returns the custom binary and arguments.
func (c *Custom) BuildCommand(prompt string) (string, []string) {
	bin := c.BinaryName
	if bin == "" {
		bin = "agent"
	}

	if prompt == "" {
		return bin, nil
	}
	return bin, []string{prompt}
}
