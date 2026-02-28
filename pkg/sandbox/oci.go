package sandbox

import (
	"encoding/json"
	"fmt"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	// agentUID is the uid of the agent user inside the sandbox.
	agentUID = 1000

	// agentGID is the gid of the agent group inside the sandbox.
	agentGID = 1000
)

// GenerateSpec produces an OCI runtime specification for a sandbox.
//
// The spec configures:
//   - Process running as uid/gid 1000 (agent) with the given command
//   - Standard mounts: /proc, /dev (tmpfs), /sys (ro), /tmp (tmpfs), /home/agent (tmpfs)
//   - Read-only bind mounts for every /nix/store path in the closure
//   - Linux namespaces: pid, ipc, uts, mount (NO network — uses host network)
//   - Resource limits: memory and PID limits from config
func GenerateSpec(cfg *Config) (*specs.Spec, error) {
	if cfg.ID == "" {
		return nil, fmt.Errorf("sandbox ID is required")
	}
	if cfg.Command == "" {
		return nil, fmt.Errorf("sandbox command is required")
	}

	// Build process args: command followed by any arguments.
	args := make([]string, 0, 1+len(cfg.Args))
	args = append(args, cfg.Command)
	args = append(args, cfg.Args...)

	// Build environment variable list.
	env := buildEnvList(cfg.Env)

	// Determine working directory.
	cwd := cfg.Workdir
	if cwd == "" {
		cwd = "/home/agent"
	}

	// Determine hostname.
	hostname := cfg.Hostname
	if hostname == "" {
		hostname = cfg.ID
	}

	// Build the mount list.
	mounts := buildMounts(cfg.StorePaths)

	// Build Linux config with namespaces and resource limits.
	linux := buildLinux(cfg.MemoryLimit, cfg.PidLimit)

	spec := &specs.Spec{
		Version: specs.Version,
		Process: &specs.Process{
			Terminal: false,
			User: specs.User{
				UID: agentUID,
				GID: agentGID,
			},
			Args: args,
			Env:  env,
			Cwd:  cwd,
		},
		Root: &specs.Root{
			Path:     "rootfs",
			Readonly: false,
		},
		Hostname: hostname,
		Mounts:   mounts,
		Linux:    linux,
	}

	return spec, nil
}

// MarshalSpec serializes an OCI spec to indented JSON suitable for
// writing to a config.json file in an OCI bundle.
func MarshalSpec(spec *specs.Spec) ([]byte, error) {
	return json.MarshalIndent(spec, "", "  ")
}

// buildEnvList converts a map of environment variables to the OCI
// format: a slice of "KEY=VALUE" strings. Standard variables are
// always included.
func buildEnvList(env map[string]string) []string {
	// Start with standard environment variables.
	result := []string{
		"HOME=/home/agent",
		"PATH=/bin:/usr/bin",
		"TERM=xterm-256color",
		"USER=agent",
	}

	// Append user-specified variables. These can override the defaults
	// since runsc uses the last value for duplicate keys.
	for k, v := range env {
		result = append(result, k+"="+v)
	}

	return result
}

// buildMounts creates the OCI mount list with standard filesystem mounts
// and read-only bind mounts for each /nix/store path.
func buildMounts(storePaths []string) []specs.Mount {
	mounts := []specs.Mount{
		{
			Destination: "/proc",
			Type:        "proc",
			Source:      "proc",
		},
		{
			Destination: "/dev",
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "strictatime", "mode=755", "size=65536k"},
		},
		{
			Destination: "/sys",
			Type:        "sysfs",
			Source:      "sysfs",
			Options:     []string{"nosuid", "noexec", "nodev", "ro"},
		},
		{
			Destination: "/tmp",
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "nodev", "size=512m"},
		},
		{
			Destination: "/home/agent",
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "nodev", "size=512m"},
		},
	}

	// Add read-only bind mounts for each /nix/store path.
	for _, storePath := range storePaths {
		mounts = append(mounts, specs.Mount{
			Destination: storePath,
			Type:        "bind",
			Source:      storePath,
			Options:     []string{"rbind", "ro"},
		})
	}

	return mounts
}

// buildLinux creates the Linux-specific portion of the OCI spec with
// namespace and resource limit configuration.
func buildLinux(memoryLimit, pidLimit int64) *specs.Linux {
	linux := &specs.Linux{
		Namespaces: []specs.LinuxNamespace{
			{Type: specs.PIDNamespace},
			{Type: specs.IPCNamespace},
			{Type: specs.UTSNamespace},
			{Type: specs.MountNamespace},
			// NOTE: No network namespace — we use host networking.
			// The stereOS VM is already the network boundary.
		},
	}

	// Add resource limits if specified.
	if memoryLimit > 0 || pidLimit > 0 {
		linux.Resources = &specs.LinuxResources{}

		if memoryLimit > 0 {
			linux.Resources.Memory = &specs.LinuxMemory{
				Limit: &memoryLimit,
			}
		}

		if pidLimit > 0 {
			linux.Resources.Pids = &specs.LinuxPids{
				Limit: &pidLimit,
			}
		}
	}

	return linux
}
