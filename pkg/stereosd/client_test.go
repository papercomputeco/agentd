package stereosd_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/papercomputeco/agentd/pkg/stereosd"
)

func TestStereosd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Stereosd Client Suite")
}

// mockStereosd creates a mock HTTP server on a Unix socket that
// simulates stereosd's API.
func mockStereosd(socketPath string, mux *http.ServeMux) (cleanup func()) {
	listener, err := net.Listen("unix", socketPath)
	Expect(err).NotTo(HaveOccurred())

	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()

	return func() {
		_ = server.Close()
		_ = listener.Close()
	}
}

var _ = Describe("Client", func() {
	var (
		socketPath string
		client     *stereosd.Client
		mux        *http.ServeMux
		cleanup    func()
	)

	BeforeEach(func() {
		dir := GinkgoT().TempDir()
		socketPath = filepath.Join(dir, "stereosd.sock")
		mux = http.NewServeMux()
	})

	AfterEach(func() {
		if client != nil {
			client.Close()
		}
		if cleanup != nil {
			cleanup()
		}
	})

	startServer := func() {
		cleanup = mockStereosd(socketPath, mux)
		client = stereosd.NewClient(socketPath)
	}

	Describe("Ping", func() {
		It("should succeed when stereosd responds OK", func() {
			mux.HandleFunc("/v1/ping", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			})
			startServer()

			err := client.Ping(ctx)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error when stereosd is not running", func() {
			client = stereosd.NewClient(socketPath)

			err := client.Ping(ctx)
			Expect(err).To(HaveOccurred())
		})

		It("should return error on non-200 status", func() {
			mux.HandleFunc("/v1/ping", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			})
			startServer()

			err := client.Ping(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("500"))
		})
	})

	Describe("Health", func() {
		It("should return health payload", func() {
			mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
				resp := stereosd.HealthPayload{
					State:  "healthy",
					Uptime: 120,
					Agents: []stereosd.AgentStatus{
						{Name: "claude-code", Running: true, Session: "claude"},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			})
			startServer()

			health, err := client.Health(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(health.State).To(Equal("healthy"))
			Expect(health.Uptime).To(Equal(int64(120)))
			Expect(health.Agents).To(HaveLen(1))
			Expect(health.Agents[0].Name).To(Equal("claude-code"))
			Expect(health.Agents[0].Running).To(BeTrue())
		})
	})

	Describe("ReportAgentStatus", func() {
		It("should send agent status and succeed", func() {
			var received stereosd.AgentStatus
			mux.HandleFunc("/v1/agents/status", func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodPost))
				Expect(r.Header.Get("Content-Type")).To(Equal("application/json"))
				_ = json.NewDecoder(r.Body).Decode(&received)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			})
			startServer()

			status := stereosd.AgentStatus{
				Name:    "opencode",
				Running: true,
				Session: "opencode-session",
			}
			err := client.ReportAgentStatus(ctx, status)
			Expect(err).NotTo(HaveOccurred())
			Expect(received.Name).To(Equal("opencode"))
			Expect(received.Running).To(BeTrue())
			Expect(received.Session).To(Equal("opencode-session"))
		})

		It("should return error on non-200 status", func() {
			mux.HandleFunc("/v1/agents/status", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"error":"bad request"}`))
			})
			startServer()

			err := client.ReportAgentStatus(ctx, stereosd.AgentStatus{Name: "test"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("422"))
		})
	})

	Describe("NotifyAgentsStopped", func() {
		It("should send notification and succeed", func() {
			var called bool
			mux.HandleFunc("/v1/agents/stopped", func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodPost))
				called = true
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			})
			startServer()

			err := client.NotifyAgentsStopped(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(called).To(BeTrue())
		})
	})

	Describe("ListSecrets", func() {
		It("should return list of secret names", func() {
			mux.HandleFunc("/v1/secrets", func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodGet))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"secrets":["API_KEY","TOKEN"]}`))
			})
			startServer()

			names, err := client.ListSecrets(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(names).To(ConsistOf("API_KEY", "TOKEN"))
		})

		It("should return empty list when no secrets", func() {
			mux.HandleFunc("/v1/secrets", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"secrets":[]}`))
			})
			startServer()

			names, err := client.ListSecrets(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(names).To(BeEmpty())
		})
	})
})

// ctx is a background context used across all tests.
var ctx = context.Background()
