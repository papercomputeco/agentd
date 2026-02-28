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
		It("should parse a minimal valid config with one agent", func() {
			toml := `
[[agents]]
harness = "claude-code"
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(1))
			Expect(agents[0].Harness).To(Equal("claude-code"))
			Expect(agents[0].Type).To(Equal(config.AgentTypeSandboxed))
			Expect(agents[0].Workdir).To(Equal("/home/agent/workspace"))
			Expect(agents[0].Restart).To(Equal(config.RestartNo))
			Expect(agents[0].GracePeriod).To(Equal("30s"))
			Expect(agents[0].Name).To(Equal("claude-code"))
			Expect(agents[0].Session).To(Equal("claude-code"))
			Expect(agents[0].Memory).To(Equal("2GiB"))
			Expect(agents[0].PidLimit).To(Equal(512))
		})

		It("should parse a fully specified config", func() {
			toml := `
[[agents]]
name = "my-agent"
harness = "opencode"
prompt = "fix the tests"
workdir = "/home/agent/project"
restart = "on-failure"
max_restarts = 5
timeout = "2h"
grace_period = "1m"
session = "my-session"

[agents.env]
FOO = "bar"
BAZ = "qux"
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(1))
			cfg := agents[0]
			Expect(cfg.Name).To(Equal("my-agent"))
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

		It("should parse multiple agents", func() {
			toml := `
[[agents]]
name = "reviewer"
harness = "claude-code"
prompt = "review the code"

[[agents]]
name = "coder"
harness = "opencode"
prompt = "implement the feature"

[[agents]]
harness = "gemini-cli"
prompt = "security audit"
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(3))
			Expect(agents[0].Name).To(Equal("reviewer"))
			Expect(agents[1].Name).To(Equal("coder"))
			Expect(agents[2].Name).To(Equal("gemini-cli"))
		})

		It("should auto-generate unique names for duplicate harnesses", func() {
			toml := `
[[agents]]
harness = "claude-code"
prompt = "task one"

[[agents]]
harness = "claude-code"
prompt = "task two"
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(2))
			Expect(agents[0].Name).To(Equal("claude-code-0"))
			Expect(agents[1].Name).To(Equal("claude-code-1"))
		})

		It("should default session to agent name", func() {
			toml := `
[[agents]]
name = "my-reviewer"
harness = "claude-code"
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents[0].Session).To(Equal("my-reviewer"))
		})

		It("should return empty slice when no agents defined", func() {
			toml := `
mixtape = "base"
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(BeEmpty())
		})

		It("should reject missing harness", func() {
			toml := `
[[agents]]
prompt = "do stuff"
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("harness is required"))
		})

		It("should reject unknown harness", func() {
			toml := `
[[agents]]
harness = "unknown-thing"
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown agent.harness"))
		})

		It("should reject invalid restart policy", func() {
			toml := `
[[agents]]
harness = "claude-code"
restart = "sometimes"
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid agent.restart"))
		})

		It("should reject invalid timeout duration", func() {
			toml := `
[[agents]]
harness = "claude-code"
timeout = "not-a-duration"
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid agent.timeout"))
		})

		It("should reject invalid grace_period duration", func() {
			toml := `
[[agents]]
harness = "claude-code"
grace_period = "bad"
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid agent.grace_period"))
		})

		It("should reject negative max_restarts", func() {
			toml := `
[[agents]]
harness = "claude-code"
max_restarts = -1
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("max_restarts"))
		})

		It("should reject duplicate agent names", func() {
			toml := `
[[agents]]
name = "same-name"
harness = "claude-code"

[[agents]]
name = "same-name"
harness = "opencode"
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("duplicate agent name"))
		})

		It("should accept all valid harness types", func() {
			for _, h := range []string{"claude-code", "opencode", "gemini-cli", "custom"} {
				toml := "[[agents]]\nharness = \"" + h + "\""
				agents, err := config.ParseConfig(toml)
				Expect(err).NotTo(HaveOccurred())
				Expect(agents[0].Harness).To(Equal(h))
			}
		})

		It("should accept all valid restart policies", func() {
			for _, r := range []string{"no", "on-failure", "always"} {
				toml := "[[agents]]\nharness = \"claude-code\"\nrestart = \"" + r + "\""
				agents, err := config.ParseConfig(toml)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(agents[0].Restart)).To(Equal(r))
			}
		})

		It("should ignore non-agent sections", func() {
			toml := `
mixtape = "base"

[resources]
cpus = 2
memory = "4GiB"

[[agents]]
harness = "claude-code"
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents[0].Harness).To(Equal("claude-code"))
		})

		It("should parse type=sandboxed explicitly", func() {
			toml := `
[[agents]]
harness = "claude-code"
type = "sandboxed"
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents[0].Type).To(Equal(config.AgentTypeSandboxed))
			Expect(agents[0].Memory).To(Equal("2GiB"))
			Expect(agents[0].PidLimit).To(Equal(512))
		})

		It("should parse type=native", func() {
			toml := `
[[agents]]
harness = "claude-code"
type = "native"
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents[0].Type).To(Equal(config.AgentTypeNative))
			// Native agents do not get sandbox defaults.
			Expect(agents[0].Memory).To(BeEmpty())
			Expect(agents[0].PidLimit).To(Equal(0))
		})

		It("should reject invalid type", func() {
			toml := `
[[agents]]
harness = "claude-code"
type = "docker"
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid agent.type"))
		})

		It("should parse sandbox-specific fields", func() {
			toml := `
[[agents]]
harness = "claude-code"
type = "sandboxed"
memory = "4GiB"
pid_limit = 1024
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents[0].Memory).To(Equal("4GiB"))
			Expect(agents[0].PidLimit).To(Equal(1024))
		})

		It("should reject invalid memory format", func() {
			toml := `
[[agents]]
harness = "claude-code"
type = "sandboxed"
memory = "lots"
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid agent.memory"))
		})

		It("should reject negative pid_limit", func() {
			toml := `
[[agents]]
harness = "claude-code"
type = "sandboxed"
pid_limit = -1
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("pid_limit"))
		})

		It("should parse extra_packages for sandboxed agents", func() {
			toml := `
[[agents]]
harness = "claude-code"
type = "sandboxed"
extra_packages = ["ripgrep", "fd", "python311"]
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents[0].ExtraPackages).To(Equal([]string{"ripgrep", "fd", "python311"}))
		})

		It("should accept empty extra_packages", func() {
			toml := `
[[agents]]
harness = "claude-code"
type = "sandboxed"
extra_packages = []
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents[0].ExtraPackages).To(BeEmpty())
		})

		It("should accept sandboxed agent without extra_packages", func() {
			toml := `
[[agents]]
harness = "claude-code"
type = "sandboxed"
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents[0].ExtraPackages).To(BeNil())
		})

		It("should reject extra_packages with empty string entries", func() {
			toml := `
[[agents]]
harness = "claude-code"
type = "sandboxed"
extra_packages = ["ripgrep", "", "fd"]
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("extra_packages[1] is empty"))
		})

		It("should reject extra_packages for native agents", func() {
			toml := `
[[agents]]
harness = "claude-code"
type = "native"
extra_packages = ["ripgrep"]
`
			_, err := config.ParseConfig(toml)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("extra_packages is only supported for type=sandboxed"))
		})

		It("should parse mixed native and sandboxed agents", func() {
			toml := `
[[agents]]
name = "native-claude"
harness = "claude-code"
type = "native"

[[agents]]
name = "sandbox-opencode"
harness = "opencode"
type = "sandboxed"
memory = "4GiB"
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(2))
			Expect(agents[0].Type).To(Equal(config.AgentTypeNative))
			Expect(agents[1].Type).To(Equal(config.AgentTypeSandboxed))
			Expect(agents[1].Memory).To(Equal("4GiB"))
		})
	})

	Describe("LoadConfig", func() {
		It("should load config from a file", func() {
			dir := GinkgoT().TempDir()
			path := filepath.Join(dir, "jcard.toml")
			err := os.WriteFile(path, []byte(`
[[agents]]
harness = "gemini-cli"
prompt = "hello world"
`), 0644)
			Expect(err).NotTo(HaveOccurred())

			agents, err := config.LoadConfig(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(1))
			Expect(agents[0].Harness).To(Equal("gemini-cli"))
			Expect(agents[0].Prompt).To(Equal("hello world"))
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

	Describe("ParseMemory", func() {
		It("should parse GiB", func() {
			n, err := config.ParseMemory("2GiB")
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(int64(2 * 1024 * 1024 * 1024)))
		})

		It("should parse MiB", func() {
			n, err := config.ParseMemory("512MiB")
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(int64(512 * 1024 * 1024)))
		})

		It("should parse KiB", func() {
			n, err := config.ParseMemory("1024KiB")
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(int64(1024 * 1024)))
		})

		It("should parse GB (decimal)", func() {
			n, err := config.ParseMemory("1GB")
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(int64(1000 * 1000 * 1000)))
		})

		It("should parse MB (decimal)", func() {
			n, err := config.ParseMemory("500MB")
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(int64(500 * 1000 * 1000)))
		})

		It("should parse plain bytes", func() {
			n, err := config.ParseMemory("1073741824")
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(int64(1073741824)))
		})

		It("should be case-insensitive", func() {
			n, err := config.ParseMemory("2gib")
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(int64(2 * 1024 * 1024 * 1024)))
		})

		It("should reject empty string", func() {
			_, err := config.ParseMemory("")
			Expect(err).To(HaveOccurred())
		})

		It("should reject unknown suffix", func() {
			_, err := config.ParseMemory("2TiB")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown memory suffix"))
		})

		It("should reject non-numeric value", func() {
			_, err := config.ParseMemory("lots")
			Expect(err).To(HaveOccurred())
		})

		It("should parse fractional values", func() {
			n, err := config.ParseMemory("1.5GiB")
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(int64(1.5 * 1024 * 1024 * 1024)))
		})
	})

	Describe("Replicas", func() {
		It("should default replicas to 1", func() {
			toml := `
[[agents]]
harness = "claude-code"
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(1))
			Expect(agents[0].Replicas).To(Equal(1))
			Expect(agents[0].Name).To(Equal("claude-code"))
		})

		It("should expand replicas=1 without suffix", func() {
			toml := `
[[agents]]
name = "reviewer"
harness = "claude-code"
replicas = 1
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(1))
			Expect(agents[0].Name).To(Equal("reviewer"))
		})

		It("should expand named replicas with index suffix", func() {
			toml := `
[[agents]]
name = "reviewer"
harness = "claude-code"
prompt = "review code"
replicas = 3
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(3))
			Expect(agents[0].Name).To(Equal("reviewer-0"))
			Expect(agents[1].Name).To(Equal("reviewer-1"))
			Expect(agents[2].Name).To(Equal("reviewer-2"))
			// Each replica should have the same harness and prompt.
			for _, a := range agents {
				Expect(a.Harness).To(Equal("claude-code"))
				Expect(a.Prompt).To(Equal("review code"))
				Expect(a.Replicas).To(Equal(1))
			}
		})

		It("should expand unnamed replicas and auto-generate names", func() {
			toml := `
[[agents]]
harness = "claude-code"
replicas = 3
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(3))
			Expect(agents[0].Name).To(Equal("claude-code-0"))
			Expect(agents[1].Name).To(Equal("claude-code-1"))
			Expect(agents[2].Name).To(Equal("claude-code-2"))
		})

		It("should handle mixed replicas and single agents", func() {
			toml := `
[[agents]]
name = "lead"
harness = "claude-code"

[[agents]]
name = "worker"
harness = "opencode"
replicas = 3
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(4))
			Expect(agents[0].Name).To(Equal("lead"))
			Expect(agents[1].Name).To(Equal("worker-0"))
			Expect(agents[2].Name).To(Equal("worker-1"))
			Expect(agents[3].Name).To(Equal("worker-2"))
		})

		It("should deep-copy env maps across replicas", func() {
			toml := `
[[agents]]
name = "worker"
harness = "claude-code"
replicas = 2

[agents.env]
KEY = "value"
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(2))
			// Verify both have the env var.
			Expect(agents[0].Env).To(HaveKeyWithValue("KEY", "value"))
			Expect(agents[1].Env).To(HaveKeyWithValue("KEY", "value"))
			// Verify they are independent maps (deep copy).
			agents[0].Env["EXTRA"] = "modified"
			Expect(agents[1].Env).NotTo(HaveKey("EXTRA"))
		})

		It("should support large replica counts", func() {
			toml := `
[[agents]]
name = "swarm"
harness = "claude-code"
replicas = 500
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(500))
			Expect(agents[0].Name).To(Equal("swarm-0"))
			Expect(agents[499].Name).To(Equal("swarm-499"))
		})

		It("should set unique sessions for replicas", func() {
			toml := `
[[agents]]
name = "worker"
harness = "claude-code"
replicas = 3
`
			agents, err := config.ParseConfig(toml)
			Expect(err).NotTo(HaveOccurred())
			Expect(agents[0].Session).To(Equal("worker-0"))
			Expect(agents[1].Session).To(Equal("worker-1"))
			Expect(agents[2].Session).To(Equal("worker-2"))
		})
	})

	Describe("MemoryBytes", func() {
		It("should return 0 when memory is empty", func() {
			cfg := &config.AgentConfig{Harness: "claude-code"}
			n, err := cfg.MemoryBytes()
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(BeZero())
		})

		It("should parse the memory field", func() {
			cfg := &config.AgentConfig{Harness: "claude-code", Memory: "2GiB"}
			n, err := cfg.MemoryBytes()
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(int64(2 * 1024 * 1024 * 1024)))
		})
	})
})
