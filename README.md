# `agentd`

AI agent management daemon for [StereOS](https://github.com/papercomputeco/stereos),
a Linux-based operating system purpose-built for AI agents.

> [!WARNING]
> 🚧🏗️ StereOS is in active development - APIs will break 🚜🚧

---

`agentd` is a daemon responsible for
starting, managing, and stopping AI agent harnesses (Claude Code, OpenCode,
Gemini CLI, OpenClaw, etc.) based on the `[agent]` section of a
[`jcard.toml`]() configuration file.

Each agent runs in its own `tmux` session, allowing operators to `tmux attach` and
observe the agent in real time. `agentd` handles restart policies, timeouts,
graceful shutdown, and serves an API for polling over a socket.

`agentd` operates on a reconciliation loop. It periodically looks for updates to
the configured `jcard.toml` and secrets in the configured secrets directory.

## Quick Start

### Prerequisites

- [Nix](https://nixos.org/) - dev flake provides Go and `make`
- `tmux` - used for managing agent sessions

### Build

```bash
direnv allow
make build
```

### Run

`agentd` is designed to run inside StereOS, managed by `systemd`.
For local development and testing, you can designate a config `jcard.toml`,
a directory for mounting secrets, a tmux socket, and an API socket:

```bash
./build/agentd \
    -config /tmp/agentd-tests/jcard.toml \
    -secret-dir /tmp/agentd-tests/secrets \
    -tmux-socket /tmp/agentd-tests/agentd-tmux.sock \
    -api-socket /tmp/agentd-tests/agentd.sock \
    -debug
```

### `jcard.toml`

The `[agent]` key in a `jcard.toml`

```toml
[agent]
harness = "claude-code"
prompt = "review the code and fix failing tests"
workdir = "/home/agent/workspace"
restart = "on-failure"
max_restarts = 5
timeout = "2h"
grace_period = "30s"
```

### Test

```bash
make test    # run all tests
make lint    # go vet
make format  # gofmt
make clean   # remove ./build artifacts
```

## `/pkg/` packages overview


| Package | Description |
|---------|-------------|
| `agentd/` | Daemon orchestrator -- wires everything together |
| `pkg/api/` | API for serving polling |
| `pkg/config/` | TOML parser for `[agent]` section of jcard.toml |
| `pkg/harness/` | Agent harness interface and built-in implementations |
| `pkg/secrets/` | Reads secret files from stereosd's tmpfs |
| `pkg/supervisor/` | Agent process lifecycle, restart policies, timeouts |
| `pkg/tmux/` | tmux server and session management |


## API

`agentd` serves an HTTP API over a socket with the following endpoints:

- `GET /v1/health` – daemon health and uptime
- `GET /v1/agents` – list all managed agents and their status
- `GET /v1/agents/{name}` – single agent status

## The StereOS multiverse of projects

- **[StereOS](https://github.com/papercomputeco/stereos)** -- Linux based OS for agents
- **[Masterblaster (`mb`)](https://github.com/papercomputeco/masterblaster)** -- Host CLI for managing StereOS VMs
- **[stereosd](https://github.com/papercomputeco/stereosd)** -- StereOS system daemon (infra, mounts, secrets, vsock)
- **[Tapes](https://github.com/papercomputeco/tapes)** -- Agent telemetry capture and replay
