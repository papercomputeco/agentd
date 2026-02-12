package api_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/papercomputeco/agentd/pkg/api"
)

func TestAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "API Suite")
}

// stubProvider implements api.AgentProvider for tests.
type stubProvider struct {
	agents []api.AgentStatus
}

func (s *stubProvider) AgentStatuses() []api.AgentStatus {
	return s.agents
}

// newTestClient creates an HTTP client that dials the given Unix socket.
func newTestClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}
}

var _ = Describe("Server", func() {
	var (
		server     *api.Server
		provider   *stubProvider
		client     *http.Client
		socketPath string
	)

	BeforeEach(func() {
		socketPath = filepath.Join(GinkgoT().TempDir(), "agentd-test.sock")
		provider = &stubProvider{}
	})

	AfterEach(func() {
		if server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = server.Stop(ctx)
		}
	})

	startServer := func() {
		server = api.NewServer(socketPath, provider)
		Expect(server.Start()).To(Succeed())
		client = newTestClient(socketPath)
	}

	Describe("GET /v1/health", func() {
		It("should return running state and uptime", func() {
			startServer()

			resp, err := client.Get("http://agentd/v1/health")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get("Content-Type")).To(Equal("application/json"))

			var health api.HealthResponse
			Expect(json.NewDecoder(resp.Body).Decode(&health)).To(Succeed())
			Expect(health.State).To(Equal("running"))
			Expect(health.Uptime).To(BeNumerically(">=", 0))
		})
	})

	Describe("GET /v1/agents", func() {
		It("should return empty list when no agents", func() {
			startServer()

			resp, err := client.Get("http://agentd/v1/agents")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var agents []api.AgentStatus
			Expect(json.NewDecoder(resp.Body).Decode(&agents)).To(Succeed())
			Expect(agents).To(BeEmpty())
		})

		It("should return agent statuses from provider", func() {
			provider.agents = []api.AgentStatus{
				{Name: "claude-code", Running: true, Session: "claude-code", Restarts: 0},
				{Name: "opencode", Running: false, Session: "opencode", Restarts: 2, Error: "exited"},
			}
			startServer()

			resp, err := client.Get("http://agentd/v1/agents")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var agents []api.AgentStatus
			Expect(json.NewDecoder(resp.Body).Decode(&agents)).To(Succeed())
			Expect(agents).To(HaveLen(2))
			Expect(agents[0].Name).To(Equal("claude-code"))
			Expect(agents[0].Running).To(BeTrue())
			Expect(agents[1].Name).To(Equal("opencode"))
			Expect(agents[1].Running).To(BeFalse())
			Expect(agents[1].Error).To(Equal("exited"))
		})
	})

	Describe("GET /v1/agents/{name}", func() {
		It("should return a single agent by name", func() {
			provider.agents = []api.AgentStatus{
				{Name: "claude-code", Running: true, Session: "claude-code"},
			}
			startServer()

			resp, err := client.Get("http://agentd/v1/agents/claude-code")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var agent api.AgentStatus
			Expect(json.NewDecoder(resp.Body).Decode(&agent)).To(Succeed())
			Expect(agent.Name).To(Equal("claude-code"))
			Expect(agent.Running).To(BeTrue())
		})

		It("should return 404 for unknown agent", func() {
			provider.agents = []api.AgentStatus{}
			startServer()

			resp, err := client.Get("http://agentd/v1/agents/nonexistent")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("should match agent names case-insensitively", func() {
			provider.agents = []api.AgentStatus{
				{Name: "Claude-Code", Running: true, Session: "claude-code"},
			}
			startServer()

			resp, err := client.Get("http://agentd/v1/agents/claude-code")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("SocketPath", func() {
		It("should return the configured socket path", func() {
			server = api.NewServer(socketPath, provider)
			Expect(server.SocketPath()).To(Equal(socketPath))
		})
	})

	Describe("Start and Stop lifecycle", func() {
		It("should clean up stale socket files", func() {
			// Start and stop a first server to leave a socket file.
			startServer()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			Expect(server.Stop(ctx)).To(Succeed())

			// Starting a second server on the same path should succeed.
			server = api.NewServer(socketPath, provider)
			Expect(server.Start()).To(Succeed())
		})
	})
})
