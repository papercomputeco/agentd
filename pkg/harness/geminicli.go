package harness

// GeminiCLI implements the Harness interface for Google's Gemini CLI.
// Gemini CLI accepts a prompt as a positional argument:
//
//	gemini "review the code and fix tests"
type GeminiCLI struct{}

// Name returns "gemini-cli".
func (g *GeminiCLI) Name() string {
	return "gemini-cli"
}

// BuildCommand returns the gemini binary and arguments.
func (g *GeminiCLI) BuildCommand(prompt string) (string, []string) {
	if prompt == "" {
		return "gemini", nil
	}
	return "gemini", []string{prompt}
}
