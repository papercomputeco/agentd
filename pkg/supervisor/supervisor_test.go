package supervisor_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/papercomputeco/agentd/pkg/config"
	"github.com/papercomputeco/agentd/pkg/harness"
	"github.com/papercomputeco/agentd/pkg/supervisor"
)

func TestSupervisor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Supervisor Suite")
}

var _ = Describe("Supervisor", func() {
	Describe("NewSupervisor", func() {
		It("should create a new supervisor", func() {
			h, err := harness.Get("claude-code")
			Expect(err).NotTo(HaveOccurred())

			cfg := &config.AgentConfig{
				Harness:     "claude-code",
				Workdir:     "/workspace",
				Restart:     config.RestartNo,
				GracePeriod: "30s",
				Session:     "claude-code",
			}

			sup := supervisor.NewSupervisor(supervisor.Opts{
				Config:  cfg,
				Harness: h,
				Env:     map[string]string{"FOO": "bar"},
				Prompt:  "test prompt",
			})

			Expect(sup).NotTo(BeNil())
			Expect(sup.IsRunning()).To(BeFalse())
		})
	})

	Describe("Status", func() {
		It("should return initial status as not running", func() {
			h, err := harness.Get("opencode")
			Expect(err).NotTo(HaveOccurred())

			cfg := &config.AgentConfig{
				Harness:     "opencode",
				Workdir:     "/workspace",
				Restart:     config.RestartNo,
				GracePeriod: "30s",
				Session:     "opencode",
			}

			sup := supervisor.NewSupervisor(supervisor.Opts{
				Config:  cfg,
				Harness: h,
			})

			status := sup.Status()
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
				Workdir:     "/workspace",
				Restart:     config.RestartNo,
				GracePeriod: "30s",
				Session:     "claude-code",
			}

			sup := supervisor.NewSupervisor(supervisor.Opts{
				Config:  cfg,
				Harness: h,
			})

			Expect(sup.Restarts()).To(Equal(0))
		})
	})
})
