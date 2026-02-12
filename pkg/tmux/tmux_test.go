package tmux_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/papercomputeco/agentd/pkg/tmux"
)

func TestTmux(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tmux Suite")
}

// tmuxAvailable checks if tmux is installed on the system.
func tmuxAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

var _ = Describe("Server", func() {
	var (
		socketPath string
		server     *tmux.Server
	)

	BeforeEach(func() {
		if !tmuxAvailable() {
			Skip("tmux not available")
		}
		dir := GinkgoT().TempDir()
		socketPath = filepath.Join(dir, "test-tmux.sock")
		server = tmux.NewServer(socketPath)
	})

	AfterEach(func() {
		if server != nil {
			_ = server.Stop()
		}
	})

	Describe("NewServer", func() {
		It("should create a server with the given socket path", func() {
			Expect(server.SocketPath()).To(Equal(socketPath))
		})
	})

	Describe("Start", func() {
		It("should succeed when tmux is available", func() {
			err := server.Start()
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("CreateSession", func() {
		It("should create a new tmux session", func() {
			err := server.CreateSession(tmux.SessionOpts{
				Name:    "test-session",
				Command: "sleep",
				Args:    []string{"60"},
			})
			Expect(err).NotTo(HaveOccurred())

			running, err := server.IsSessionRunning("test-session")
			Expect(err).NotTo(HaveOccurred())
			Expect(running).To(BeTrue())
		})

		It("should reject empty session name", func() {
			err := server.CreateSession(tmux.SessionOpts{
				Command: "sleep",
				Args:    []string{"60"},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("session name is required"))
		})

		It("should reject empty command", func() {
			err := server.CreateSession(tmux.SessionOpts{
				Name: "test",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("session command is required"))
		})

		It("should set the working directory", func() {
			dir := GinkgoT().TempDir()
			err := server.CreateSession(tmux.SessionOpts{
				Name:    "workdir-test",
				Command: "sleep",
				Args:    []string{"60"},
				Workdir: dir,
			})
			Expect(err).NotTo(HaveOccurred())

			running, err := server.IsSessionRunning("workdir-test")
			Expect(err).NotTo(HaveOccurred())
			Expect(running).To(BeTrue())
		})
	})

	Describe("ListSessions", func() {
		It("should return empty when no sessions exist", func() {
			sessions, err := server.ListSessions()
			Expect(err).NotTo(HaveOccurred())
			Expect(sessions).To(BeNil())
		})

		It("should list created sessions", func() {
			err := server.CreateSession(tmux.SessionOpts{
				Name: "session-a", Command: "sleep", Args: []string{"60"},
			})
			Expect(err).NotTo(HaveOccurred())

			err = server.CreateSession(tmux.SessionOpts{
				Name: "session-b", Command: "sleep", Args: []string{"60"},
			})
			Expect(err).NotTo(HaveOccurred())

			sessions, err := server.ListSessions()
			Expect(err).NotTo(HaveOccurred())
			Expect(sessions).To(ConsistOf("session-a", "session-b"))
		})
	})

	Describe("IsSessionRunning", func() {
		It("should return false for non-existent session", func() {
			// Need at least one session for the server to exist.
			err := server.CreateSession(tmux.SessionOpts{
				Name: "dummy", Command: "sleep", Args: []string{"60"},
			})
			Expect(err).NotTo(HaveOccurred())

			running, err := server.IsSessionRunning("nonexistent")
			Expect(err).NotTo(HaveOccurred())
			Expect(running).To(BeFalse())
		})

		It("should return true for running session", func() {
			err := server.CreateSession(tmux.SessionOpts{
				Name: "running", Command: "sleep", Args: []string{"60"},
			})
			Expect(err).NotTo(HaveOccurred())

			running, err := server.IsSessionRunning("running")
			Expect(err).NotTo(HaveOccurred())
			Expect(running).To(BeTrue())
		})
	})

	Describe("DestroySession", func() {
		It("should destroy a running session", func() {
			err := server.CreateSession(tmux.SessionOpts{
				Name: "to-destroy", Command: "sleep", Args: []string{"60"},
			})
			Expect(err).NotTo(HaveOccurred())

			err = server.DestroySession("to-destroy")
			Expect(err).NotTo(HaveOccurred())

			// Need the server to still be running for has-session to work,
			// so we check via list instead.
			sessions, err := server.ListSessions()
			Expect(err).NotTo(HaveOccurred())
			Expect(sessions).NotTo(ContainElement("to-destroy"))
		})
	})

	Describe("WaitForExit", func() {
		It("should return when session exits", func() {
			err := server.CreateSession(tmux.SessionOpts{
				Name:    "short-lived",
				Command: "sleep",
				Args:    []string{"60"},
			})
			Expect(err).NotTo(HaveOccurred())

			done := make(chan error, 1)
			go func() {
				done <- server.WaitForExit("short-lived", 200*time.Millisecond)
			}()

			// Destroy the session externally to simulate the session ending.
			err = server.DestroySession("short-lived")
			Expect(err).NotTo(HaveOccurred())

			Eventually(done, 5*time.Second).Should(Receive(BeNil()))
		})
	})

	Describe("Stop", func() {
		It("should stop the tmux server", func() {
			err := server.CreateSession(tmux.SessionOpts{
				Name: "to-stop", Command: "sleep", Args: []string{"60"},
			})
			Expect(err).NotTo(HaveOccurred())

			err = server.Stop()
			Expect(err).NotTo(HaveOccurred())

			// After stopping the server, sessions should not be running.
			sessions, err := server.ListSessions()
			Expect(err).NotTo(HaveOccurred())
			Expect(sessions).To(BeNil())
		})

		It("should not error when no server is running", func() {
			err := server.Stop()
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

func init() {
	// Ensure HOME is set for tmux.
	if os.Getenv("HOME") == "" {
		_ = os.Setenv("HOME", os.TempDir())
	}
}
