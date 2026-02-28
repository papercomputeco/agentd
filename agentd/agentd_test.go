package agentd_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/papercomputeco/agentd/agentd"
)

func TestAgentd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "agentd Suite")
}

var _ = Describe("Daemon", func() {
	Describe("NewDaemon", func() {
		It("should create a new daemon instance", func() {
			d := agentd.NewDaemon("")
			Expect(d).NotTo(BeNil())
		})

		It("should accept a custom config path", func() {
			d := agentd.NewDaemon("/tmp/custom.toml")
			Expect(d).NotTo(BeNil())
		})
	})

	Describe("AgentStatuses", func() {
		It("should return nil when no agents are running", func() {
			d := agentd.NewDaemon("")
			statuses := d.AgentStatuses()
			Expect(statuses).To(BeNil())
		})
	})

	Describe("SetAPISocketPath", func() {
		It("should not panic when setting a custom socket path", func() {
			d := agentd.NewDaemon("")
			Expect(func() { d.SetAPISocketPath("/tmp/test.sock") }).NotTo(Panic())
		})
	})

	Describe("SetSecretDir", func() {
		It("should not panic when setting a custom secret dir", func() {
			d := agentd.NewDaemon("")
			Expect(func() { d.SetSecretDir("/tmp/secrets") }).NotTo(Panic())
		})
	})

	Describe("SetLaunchConcurrency", func() {
		It("should not panic when setting concurrency", func() {
			d := agentd.NewDaemon("")
			Expect(func() { d.SetLaunchConcurrency(100) }).NotTo(Panic())
		})

		It("should ignore non-positive values", func() {
			d := agentd.NewDaemon("")
			Expect(func() { d.SetLaunchConcurrency(0) }).NotTo(Panic())
			Expect(func() { d.SetLaunchConcurrency(-1) }).NotTo(Panic())
		})
	})

	Describe("Constants", func() {
		It("should reference the correct secret directory", func() {
			Expect(agentd.SecretDir).To(Equal("/run/stereos/secrets"))
		})

		It("should reference the correct default API socket path", func() {
			Expect(agentd.DefaultAPISocketPath).To(Equal("/run/stereos/agentd.sock"))
		})

		It("should reference the correct tmux socket path", func() {
			Expect(agentd.TmuxSocketPath).To(Equal("/run/agentd/tmux.sock"))
		})

		It("should reference the correct default config path", func() {
			Expect(agentd.DefaultConfigPath).To(Equal("/etc/stereos/jcard.toml"))
		})

		It("should have a default launch concurrency", func() {
			Expect(agentd.DefaultLaunchConcurrency).To(Equal(50))
		})
	})
})
