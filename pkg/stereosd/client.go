// Package stereosd provides an HTTP client for communicating with the
// stereosd daemon over a Unix domain socket. agentd uses this client to
// report agent status, check health, and coordinate graceful shutdown.
package stereosd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	// baseURL is the base URL for HTTP requests over the Unix socket.
	// The host is ignored when dialing over Unix; "stereosd" is used
	// for clarity in logs.
	baseURL = "http://stereosd"

	// defaultTimeout is the default HTTP client timeout.
	defaultTimeout = 5 * time.Second

	// defaultDialTimeout is the timeout for establishing the socket connection.
	defaultDialTimeout = 2 * time.Second
)

// AgentStatus represents the status of an agent harness, matching
// stereosd's AgentStatusPayload.
type AgentStatus struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Session string `json:"session,omitempty"`
	Error   string `json:"error,omitempty"`
}

// HealthPayload represents stereosd's health response.
type HealthPayload struct {
	State  string        `json:"state"`
	Agents []AgentStatus `json:"agents,omitempty"`
	Uptime int64         `json:"uptime_seconds"`
}

// statusResponse is the common response for simple status endpoints.
type statusResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// secretsResponse is the response for the list secrets endpoint.
type secretsResponse struct {
	Secrets []string `json:"secrets"`
}

// Client communicates with stereosd over a Unix domain socket HTTP API.
type Client struct {
	httpClient *http.Client
	socketPath string
}

// NewClient creates a new stereosd client that connects to the given
// Unix domain socket path.
func NewClient(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					d := net.Dialer{Timeout: defaultDialTimeout}
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
			Timeout: defaultTimeout,
		},
	}
}

// Close releases resources held by the client.
func (c *Client) Close() {
	c.httpClient.CloseIdleConnections()
}

// Ping checks connectivity to stereosd.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/ping", nil)
	if err != nil {
		return fmt.Errorf("creating ping request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pinging stereosd: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping returned status %d", resp.StatusCode)
	}

	return nil
}

// Health returns the full health status from stereosd.
func (c *Client) Health(ctx context.Context) (*HealthPayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/health", nil)
	if err != nil {
		return nil, fmt.Errorf("creating health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getting health: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health returned status %d", resp.StatusCode)
	}

	var health HealthPayload
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("decoding health response: %w", err)
	}

	return &health, nil
}

// ReportAgentStatus sends the current agent status to stereosd.
// This triggers stereosd's lifecycle transition from "ready" to "healthy".
func (c *Client) ReportAgentStatus(ctx context.Context, status AgentStatus) error {
	body, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshaling agent status: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/agents/status", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating agent status request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("reporting agent status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent status returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// NotifyAgentsStopped tells stereosd that all agents have been stopped.
// This unblocks stereosd's shutdown coordinator to proceed with unmounting
// and poweroff.
func (c *Client) NotifyAgentsStopped(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/agents/stopped", bytes.NewReader([]byte("{}")))
	if err != nil {
		return fmt.Errorf("creating agents stopped request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("notifying agents stopped: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agents stopped returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ListSecrets returns the names of secrets available from stereosd.
func (c *Client) ListSecrets(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/secrets", nil)
	if err != nil {
		return nil, fmt.Errorf("creating list secrets request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing secrets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list secrets returned status %d", resp.StatusCode)
	}

	var result secretsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding secrets response: %w", err)
	}

	return result.Secrets, nil
}
