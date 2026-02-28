// Package api provides a read-only HTTP API for agentd, served over a
// Unix domain socket. External consumers (stereosd, CLI tools, monitoring)
// pull agent state from this API rather than agentd pushing status outward.
//
// Endpoints:
// - GET /v1/health          – daemon health and uptime
// - GET /v1/agents          – list all managed agents and their status
// - GET /v1/agents/{name}   – single agent status
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"
)

const (
	// adminGroup is the group that gets read/write access to the agentd
	// API socket, matching the StereOS admin group convention.
	adminGroup = "admin"
)

// AgentStatus describes the runtime state of a single agent harness.
type AgentStatus struct {
	Name     string `json:"name"`
	Running  bool   `json:"running"`
	Session  string `json:"session,omitempty"`
	Restarts int    `json:"restarts"`
	Error    string `json:"error,omitempty"`
	Type     string `json:"type,omitempty"`
}

// HealthResponse is returned by GET /v1/health.
type HealthResponse struct {
	State  string `json:"state"`
	Uptime int64  `json:"uptime_seconds"`
}

// AgentProvider is the interface the API server uses to query current
// agent state. The Daemon implements this.
type AgentProvider interface {
	// AgentStatuses returns the status of all managed agents.
	AgentStatuses() []AgentStatus
}

// Server is the agentd API server.
type Server struct {
	socketPath string
	provider   AgentProvider
	startTime  time.Time
	httpServer *http.Server
	listener   net.Listener
}

// NewServer creates a new API server that will listen on the given Unix
// domain socket path and query the provider for agent state.
func NewServer(socketPath string, provider AgentProvider) *Server {
	s := &Server{
		socketPath: socketPath,
		provider:   provider,
		startTime:  time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/agents", s.handleAgents)
	mux.HandleFunc("GET /v1/agents/{name}", s.handleAgent)

	s.httpServer = &http.Server{Handler: mux}
	return s
}

// Start begins listening on the Unix socket. It is non-blocking — the
// server runs in a background goroutine.
func (s *Server) Start() error {
	// Remove stale socket file if it exists.
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing stale socket %s: %w", s.socketPath, err)
	}

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.socketPath, err)
	}
	s.listener = ln

	// Set socket permissions so admin group members can query agent status.
	if err := os.Chmod(s.socketPath, 0660); err != nil {
		log.Printf("api: warning: chmod %s: %v", s.socketPath, err)
	}
	if err := chownToGroup(s.socketPath, adminGroup); err != nil {
		log.Printf("api: warning: chown %s to group %s: %v", s.socketPath, adminGroup, err)
	}

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("api: server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the API server.
func (s *Server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// SocketPath returns the socket path the server is listening on.
func (s *Server) SocketPath() string {
	return s.socketPath
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	resp := HealthResponse{
		State:  "running",
		Uptime: int64(time.Since(s.startTime).Seconds()),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAgents(w http.ResponseWriter, _ *http.Request) {
	statuses := s.provider.AgentStatuses()
	if statuses == nil {
		statuses = []AgentStatus{}
	}
	writeJSON(w, http.StatusOK, statuses)
}

func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	statuses := s.provider.AgentStatuses()
	for _, st := range statuses {
		if strings.EqualFold(st.Name, name) {
			writeJSON(w, http.StatusOK, st)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// chownToGroup sets the group ownership of a file to the named group,
// keeping the current owner unchanged.
func chownToGroup(path, groupName string) error {
	grp, err := user.LookupGroup(groupName)
	if err != nil {
		return fmt.Errorf("lookup group %s: %w", groupName, err)
	}
	gid, err := strconv.Atoi(grp.Gid)
	if err != nil {
		return fmt.Errorf("parse gid %s: %w", grp.Gid, err)
	}
	if err := os.Chown(path, -1, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	return nil
}
