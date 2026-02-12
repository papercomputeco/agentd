package harness_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/papercomputeco/agentd/pkg/harness"
)

func TestHarness(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Harness Suite")
}

var _ = Describe("Harness", func() {
	Describe("Get", func() {
		It("should return a ClaudeCode harness", func() {
			h, err := harness.Get("claude-code")
			Expect(err).NotTo(HaveOccurred())
			Expect(h.Name()).To(Equal("claude-code"))
		})

		It("should return an OpenCode harness", func() {
			h, err := harness.Get("opencode")
			Expect(err).NotTo(HaveOccurred())
			Expect(h.Name()).To(Equal("opencode"))
		})

		It("should return a GeminiCLI harness", func() {
			h, err := harness.Get("gemini-cli")
			Expect(err).NotTo(HaveOccurred())
			Expect(h.Name()).To(Equal("gemini-cli"))
		})

		It("should return a Custom harness", func() {
			h, err := harness.Get("custom")
			Expect(err).NotTo(HaveOccurred())
			Expect(h.Name()).To(Equal("custom"))
		})

		It("should return error for unknown harness", func() {
			_, err := harness.Get("nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown harness"))
		})
	})

	Describe("Names", func() {
		It("should list all registered harness names", func() {
			names := harness.Names()
			Expect(names).To(ConsistOf("claude-code", "opencode", "gemini-cli", "custom"))
		})
	})

	Describe("ClaudeCode", func() {
		var h harness.Harness

		BeforeEach(func() {
			var err error
			h, err = harness.Get("claude-code")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should build command with prompt", func() {
			bin, args := h.BuildCommand("fix the tests")
			Expect(bin).To(Equal("claude"))
			Expect(args).To(Equal([]string{"-p", "fix the tests"}))
		})

		It("should build command without prompt (interactive mode)", func() {
			bin, args := h.BuildCommand("")
			Expect(bin).To(Equal("claude"))
			Expect(args).To(BeNil())
		})
	})

	Describe("OpenCode", func() {
		var h harness.Harness

		BeforeEach(func() {
			var err error
			h, err = harness.Get("opencode")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should build command with prompt", func() {
			bin, args := h.BuildCommand("review code")
			Expect(bin).To(Equal("opencode"))
			Expect(args).To(Equal([]string{"-m", "review code"}))
		})

		It("should build command without prompt", func() {
			bin, args := h.BuildCommand("")
			Expect(bin).To(Equal("opencode"))
			Expect(args).To(BeNil())
		})
	})

	Describe("GeminiCLI", func() {
		var h harness.Harness

		BeforeEach(func() {
			var err error
			h, err = harness.Get("gemini-cli")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should build command with prompt", func() {
			bin, args := h.BuildCommand("analyze this")
			Expect(bin).To(Equal("gemini"))
			Expect(args).To(Equal([]string{"analyze this"}))
		})

		It("should build command without prompt", func() {
			bin, args := h.BuildCommand("")
			Expect(bin).To(Equal("gemini"))
			Expect(args).To(BeNil())
		})
	})

	Describe("Custom", func() {
		It("should use default binary name 'agent'", func() {
			h, err := harness.Get("custom")
			Expect(err).NotTo(HaveOccurred())

			bin, args := h.BuildCommand("do something")
			Expect(bin).To(Equal("agent"))
			Expect(args).To(Equal([]string{"do something"}))
		})

		It("should return no args in interactive mode", func() {
			h, err := harness.Get("custom")
			Expect(err).NotTo(HaveOccurred())

			bin, args := h.BuildCommand("")
			Expect(bin).To(Equal("agent"))
			Expect(args).To(BeNil())
		})
	})
})
