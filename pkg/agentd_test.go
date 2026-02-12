package agentd_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/papercomputeco/agentd/pkg"
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
	})

	Describe("Constants", func() {
		It("should reference the correct stereosd socket path", func() {
			Expect(agentd.StereosdSocketPath).To(Equal("/run/stereos/stereosd.sock"))
		})

		It("should reference the correct secret directory", func() {
			Expect(agentd.SecretDir).To(Equal("/run/stereos/secrets"))
		})
	})
})
