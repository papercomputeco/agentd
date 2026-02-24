package config_test

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/papercomputeco/agentd/pkg/config"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Suite")
}

var _ = Describe("Config", func() {
	Describe("ParseConfig", func() {
		It("should parse a minimal valid config", func() {
			toml := `
[agent]
harness = "claude-code"
`
			cfg, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Harness).To(Equal("claude-code"))
			Expect(cfg.Workdir).To(Equal("/home/agent/workspace"))
			Expect(cfg.Restart).To(Equal(config.RestartNo))
			Expect(cfg.GracePeriod).To(Equal("30s"))
			Expect(cfg.Session).To(Equal("claude-code"))
		})

		It("should parse a fully specified config", func() {
			toml := `
[agent]
harness = "opencode"
prompt = "fix the tests"
workdir = "/home/agent/project"
restart = "on-failure"
max_restarts = 5
timeout = "2h"
grace_period = "1m"
session = "my-session"

[agent.env]
FOO = "bar"
BAZ = "qux"
`
			cfg, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Harness).To(Equal("opencode"))
			Expect(cfg.Prompt).To(Equal("fix the tests"))
			Expect(cfg.Workdir).To(Equal("/home/agent/project"))
			Expect(cfg.Restart).To(Equal(config.RestartOnFailure))
			Expect(cfg.MaxRestarts).To(Equal(5))
			Expect(cfg.Timeout).To(Equal("2h"))
			Expect(cfg.GracePeriod).To(Equal("1m"))
			Expect(cfg.Session).To(Equal("my-session"))
			Expect(cfg.Env).To(HaveKeyWithValue("FOO", "bar"))
			Expect(cfg.Env).To(HaveKeyWithValue("BAZ", "qux"))
		})

		It("should reject missing harness", func() {
			toml := `
[agent]
prompt = "do stuff"
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("harness is required"))
		})

		It("should reject unknown harness", func() {
			toml := `
[agent]
harness = "unknown-thing"
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown agent.harness"))
		})

		It("should reject invalid restart policy", func() {
			toml := `
[agent]
harness = "claude-code"
restart = "sometimes"
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid agent.restart"))
		})

		It("should reject invalid timeout duration", func() {
			toml := `
[agent]
harness = "claude-code"
timeout = "not-a-duration"
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid agent.timeout"))
		})

		It("should reject invalid grace_period duration", func() {
			toml := `
[agent]
harness = "claude-code"
grace_period = "bad"
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid agent.grace_period"))
		})

		It("should reject negative max_restarts", func() {
			toml := `
[agent]
harness = "claude-code"
max_restarts = -1
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("max_restarts"))
		})

		It("should accept all valid harness types", func() {
			for _, h := range []string{"claude-code", "opencode", "gemini-cli", "custom"} {
				toml := "[agent]\nharness = \"" + h + "\""
				cfg, err := config.ParseConfig(toml)
				Expect(err).NotTo(HaveOccurred())
				Expect(cfg.Harness).To(Equal(h))
			}
		})

		It("should accept all valid restart policies", func() {
			for _, r := range []string{"no", "on-failure", "always"} {
				toml := "[agent]\nharness = \"claude-code\"\nrestart = \"" + r + "\""
				cfg, err := config.ParseConfig(toml)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(cfg.Restart)).To(Equal(r))
			}
		})

		It("should ignore non-agent sections", func() {
			toml := `
mixtape = "base"

[resources]
cpus = 2
memory = "4GiB"

[agent]
harness = "claude-code"
`
			cfg, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Harness).To(Equal("claude-code"))
		})
	})

	Describe("LoadConfig", func() {
		It("should load config from a file", func() {
			dir := GinkgoT().TempDir()
			path := filepath.Join(dir, "jcard.toml")
			err := os.WriteFile(path, []byte(`
[agent]
harness = "gemini-cli"
prompt = "hello world"
`), 0644)
			Expect(err).NotTo(HaveOccurred())

			cfg, err := config.LoadConfig(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Harness).To(Equal("gemini-cli"))
			Expect(cfg.Prompt).To(Equal("hello world"))
		})

		It("should return error for non-existent file", func() {
			_, err := config.LoadConfig("/nonexistent/jcard.toml")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ResolvePrompt", func() {
		It("should return prompt when set", func() {
			cfg := &config.AgentConfig{
				Harness: "claude-code",
				Prompt:  "do the thing",
			}
			prompt, err := cfg.ResolvePrompt()
			Expect(err).NotTo(HaveOccurred())
			Expect(prompt).To(Equal("do the thing"))
		})

		It("should return empty string when neither prompt nor prompt_file is set", func() {
			cfg := &config.AgentConfig{
				Harness: "claude-code",
			}
			prompt, err := cfg.ResolvePrompt()
			Expect(err).NotTo(HaveOccurred())
			Expect(prompt).To(BeEmpty())
		})

		It("should read prompt from file when prompt_file is set", func() {
			dir := GinkgoT().TempDir()
			promptPath := filepath.Join(dir, "prompt.md")
			err := os.WriteFile(promptPath, []byte("review all the code\n"), 0644)
			Expect(err).NotTo(HaveOccurred())

			cfg := &config.AgentConfig{
				Harness:    "claude-code",
				PromptFile: promptPath,
			}
			prompt, err := cfg.ResolvePrompt()
			Expect(err).NotTo(HaveOccurred())
			Expect(prompt).To(Equal("review all the code"))
		})

		It("should prefer prompt_file over prompt", func() {
			dir := GinkgoT().TempDir()
			promptPath := filepath.Join(dir, "prompt.md")
			err := os.WriteFile(promptPath, []byte("from file"), 0644)
			Expect(err).NotTo(HaveOccurred())

			cfg := &config.AgentConfig{
				Harness:    "claude-code",
				Prompt:     "from field",
				PromptFile: promptPath,
			}
			prompt, err := cfg.ResolvePrompt()
			Expect(err).NotTo(HaveOccurred())
			Expect(prompt).To(Equal("from file"))
		})
	})

	Describe("TimeoutDuration", func() {
		It("should return 0 when no timeout is set", func() {
			cfg := &config.AgentConfig{Harness: "claude-code"}
			d, err := cfg.TimeoutDuration()
			Expect(err).NotTo(HaveOccurred())
			Expect(d).To(BeZero())
		})

		It("should parse a valid timeout", func() {
			cfg := &config.AgentConfig{Harness: "claude-code", Timeout: "2h"}
			d, err := cfg.TimeoutDuration()
			Expect(err).NotTo(HaveOccurred())
			Expect(d.Hours()).To(Equal(2.0))
		})
	})

	Describe("GraceDuration", func() {
		It("should return default when no grace_period is set", func() {
			cfg := &config.AgentConfig{Harness: "claude-code"}
			d, err := cfg.GraceDuration()
			Expect(err).NotTo(HaveOccurred())
			Expect(d.Seconds()).To(Equal(30.0))
		})

		It("should parse a valid grace_period", func() {
			cfg := &config.AgentConfig{Harness: "claude-code", GracePeriod: "1m"}
			d, err := cfg.GraceDuration()
			Expect(err).NotTo(HaveOccurred())
			Expect(d.Minutes()).To(Equal(1.0))
		})
	})
})
