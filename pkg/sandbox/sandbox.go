// Package sandbox implements gVisor-based agent sandboxing for agentd.
//
// Sandboxed agents run inside gVisor (runsc) containers with read-only
// /nix/store bind mounts from the host and writable tmpfs overlays for
// the agent's home directory and /tmp. This provides process-level
// isolation while sharing the host's Nix closure for fast cold starts.
//
// The package provides:
//   - OCI runtime spec generation (oci.go)
//   - Minimal rootfs preparation with /nix/store symlinks (rootfs.go)
//   - Nix store closure computation (closure.go)
//   - A runsc binary wrapper for container lifecycle (runsc.go)
//   - A Manager that orchestrates the full sandbox lifecycle (manager.go)
package sandbox

// Config holds the configuration for creating a sandbox.
type Config struct {
	// ID is the unique identifier for this sandbox (e.g. "agent-claude-code").
	ID string

	// Command is the executable to run inside the sandbox.
	Command string

	// Args are the command-line arguments for the process.
	Args []string

	// Env holds environment variables for the sandboxed process.
	Env map[string]string

	// Workdir is the working directory inside the sandbox.
	Workdir string

	// StorePaths is the list of /nix/store paths to bind-mount (read-only).
	StorePaths []string

	// MemoryLimit is the memory limit in bytes. 0 means no limit.
	MemoryLimit int64

	// PidLimit is the maximum number of PIDs in the sandbox. 0 means no limit.
	PidLimit int64

	// Hostname is the hostname inside the sandbox.
	Hostname string
}
