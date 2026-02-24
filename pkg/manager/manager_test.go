package manager_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/papercomputeco/agentd/pkg/config"
	"github.com/papercomputeco/agentd/pkg/harness"
	"github.com/papercomputeco/agentd/pkg/manager"
)

func TestManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Manager Suite")
}

var _ = Describe("Manager", func() {
	Describe("NewManager", func() {
		It("should create a new manager", func() {
			h, err := harness.Get("claude-code")
			Expect(err).NotTo(HaveOccurred())

			cfg := &config.AgentConfig{
				Harness:     "claude-code",
				Workdir:     "/home/agent/workspace",
				Restart:     config.RestartNo,
				GracePeriod: "30s",
				Session:     "claude-code",
			}

			m := manager.NewManager(manager.Opts{
				Config:  cfg,
				Harness: h,
				Env:     map[string]string{"FOO": "bar"},
				Prompt:  "test prompt",
			})

			Expect(m).NotTo(BeNil())
			Expect(m.IsRunning()).To(BeFalse())
		})
	})

	Describe("Status", func() {
		It("should return initial status as not running", func() {
			h, err := harness.Get("opencode")
			Expect(err).NotTo(HaveOccurred())

			cfg := &config.AgentConfig{
				Harness:     "opencode",
				Workdir:     "/home/agent/workspace",
				Restart:     config.RestartNo,
				GracePeriod: "30s",
				Session:     "opencode",
			}

			m := manager.NewManager(manager.Opts{
				Config:  cfg,
				Harness: h,
			})

			status := m.Status()
			Expect(status.Name).To(Equal("opencode"))
			Expect(status.Running).To(BeFalse())
			Expect(status.Session).To(Equal("opencode"))
			Expect(status.Restarts).To(Equal(0))
			Expect(status.Error).To(BeEmpty())
		})
	})

	Describe("Restarts", func() {
		It("should return zero initially", func() {
			h, err := harness.Get("claude-code")
			Expect(err).NotTo(HaveOccurred())

			cfg := &config.AgentConfig{
				Harness:     "claude-code",
				Workdir:     "/home/agent/workspace",
				Restart:     config.RestartNo,
				GracePeriod: "30s",
				Session:     "claude-code",
			}

			m := manager.NewManager(manager.Opts{
				Config:  cfg,
				Harness: h,
			})

			Expect(m.Restarts()).To(Equal(0))
		})
	})
})
