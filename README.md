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
graceful shutdown, and reports agent status back to
[`stereosd`](https://github.com/papercomputeco/stereosd) over a local socket.


## Quick Start

### Prerequisites

- [Nix](https://nixos.org/) (dev flake provides Go 1.25+ and gnumake)

### Build

```bash
direnv allow
make build
```

### Run

`agentd` is designed to run inside a StereOS VM, managed by systemd after
`stereosd.service` has started and reported readiness. For local development/testing:

```bash
# Point at a local jcard.toml
./build/agentd --config ./path/to/jcard.toml
```

The minimal `jcard.toml` for agentd:

```toml
[agent]
harness = "claude-code"
# prompt = "review the code in /workspace and fix failing tests"
# workdir = "/workspace"
# restart = "on-failure"
# max_restarts = 5
# timeout = "2h"
# grace_period = "30s"
```

### Test

```bash
make test    # run all tests
make lint    # go vet
make fmt     # gofmt
make clean   # remove build artifacts
```

## `/pkg/` packages overview


| Package | Description |
|---------|-------------|
| `pkg/` | Daemon orchestrator -- wires everything together |
| `pkg/config/` | TOML parser for `[agent]` section of jcard.toml |
| `pkg/secrets/` | Reads secret files from stereosd's tmpfs |
| `pkg/stereosd/` | HTTP client for the stereosd Unix socket API |
| `pkg/tmux/` | tmux server and session management |
| `pkg/harness/` | Agent harness interface and built-in implementations |
| `pkg/supervisor/` | Agent process lifecycle, restart policies, timeouts |


## Architecture

```
 Host
 +----------------------------------------------------------+
 |  Masterblaster (mb)                                      |
 |    mb up / mb down / mb status                           |
 +---------------------------+------------------------------+
                             |
                             | virtio-vsock
                             |
 +---------------------------|------------------------------+
 |  StereOS                  |                              |
 |                           v                              |
 |  +-----------------------------------------------------+ |
 |  | stereosd                                            | |
 |  |   - vsock <-> host communication                    | |
 |  |   - mounts shared directories                       | |
 |  |   - writes secrets to tmpfs                         | |
 |  |   - lifecycle state machine                         | |
 |  |   - listens on /run/stereos/stereosd.sock           | |
 |  +-------------------------+---------------------------+ |
 |                            | HTTP over Unix socket       |
 |  +-------------------------v---------------------------+ |
 |  | agentd                                              | |
 |  |   - reads [agent] config from jcard.toml            | |
 |  |   - reads secrets from /run/stereos/secrets/ tmpfs  | |
 |  |   - starts dedicated tmux server                    | |
 |  |   - launches agent harness in tmux session          | |
 |  |   - supervises agent (restart, timeout, grace)      | |
 |  |   - reports status to stereosd                      | |
 |  +-------------------------+---------------------------+ |
 |                            |                             |
 |  +-------------------------v---------------------------+ |
 |  | tmux session: "claude-code"                         | |
 |  |                                                     | |
 |  |   $ claude -p "fix the failing tests"               | |
 |  |                                                     | |
 |  |   (admin can: tmux attach -t claude-code)           | |
 |  +-----------------------------------------------------+ |
 +----------------------------------------------------------+
```

## StereOS multi-verse

- **[StereOS](https://github.com/papercomputeco/stereos)** -- Linux based OS for agents
- **[Masterblaster (`mb`)](https://github.com/papercomputeco/masterblaster)** -- Host CLI for managing StereOS VMs
- **[stereosd](https://github.com/papercomputeco/stereosd)** -- StereOS system daemon (infra, mounts, secrets, vsock)
- **[Tapes](https://github.com/papercomputeco/tapes)** -- Agent telemetry capture and replay
