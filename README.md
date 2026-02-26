# `agentd` 🏃

AI agent management daemon for [stereOS](https://github.com/papercomputeco/stereos).
Starts, manages, and stops AI agents.

Each agent runs in its own `tmux` session, allowing operators to
`tmux attach` and observe the agent in real time. `agentd` handles restart
policies, timeouts, graceful shutdown, and serves a read-only API for status
polling.

`agentd` operates on a **reconciliation loop**. Every few seconds it re-reads the
`jcard.toml` configuration and secrets directory from disk. If either has changed,
the running agent is stopped and relaunched with the new
configuration.

## Harnesses

A harness maps a `jcard.toml` harness name to a binary and argument format.
The `Harness` interface:

```go
type Harness interface {
    Name() string
    BuildCommand(prompt string) (bin string, args []string)
}
```

Built-in harnesses:

| Name | Binary | Prompt flag | Interactive (no prompt) |
|------|--------|-------------|------------------------|
| `claude-code` | `claude` | `-p <prompt>` | `claude` |
| `opencode` | `opencode` | `--prompt <prompt>` | `opencode` |
| `gemini-cli` | `gemini` | `<prompt>` (positional) | `gemini` |
| `custom` | `agent` (configurable) | `<prompt>` (positional) | `agent` |

Adding a new harness: implement the interface, register it in `harness.go`'s
`registry` map, and add the name to config validation.

## Manager

The manager handles agent lifecycle within a tmux session.

**Start:** If `timeout` is set, wraps context with `context.WithTimeout`.
Creates a tmux session, launches the harness command via `send-keys`, spawns
a monitoring goroutine.

**Monitor loop:** Polls `tmux.IsSessionRunning()` every 2 seconds. On agent
exit, evaluates `shouldRestart()`:

| Restart policy | Behavior |
|----------------|----------|
| `no` | Never restart |
| `on-failure` | Restart unless `max_restarts` reached |
| `always` | Restart unless `max_restarts` reached |

Restart backoff: 3 seconds between attempts.

**Graceful stop:** Sends `C-c` (SIGINT) to the tmux session. Waits up to the
grace period (default 30s) for the session to exit. If the grace period
expires, forcibly destroys the session.

## tmux sessions

`agentd` uses a dedicated tmux server socket (`/run/agentd/tmux.sock`) isolated
from user sessions. All tmux commands are wrapped in `sudo -u agent --` because
tmux enforces UID-based ownership checks on socket connections.

Session creation:
1. Create a detached session with a default shell (`tmux new-session -d`)
2. Set environment variables via `-e` flags
3. Set working directory via `-c`
4. Use `send-keys` to type the command + press Enter

This approach keeps the underlying shell alive after the agent exits, allowing
inspection. Socket permissions are set to `0770` with `admin` group ownership
so admin users can attach.

```bash
# As admin user inside the VM:
sudo tmux -S /run/agentd/tmux.sock attach -t claude-code
```

## Configuration

`agentd` reads only the `[agent]` section from `jcard.toml`, ignoring everything
else.

```toml
[agent]
harness = "claude-code"
prompt = "review the code and fix failing tests"
# prompt_file = "./prompts/review.md"    # takes precedence over prompt
workdir = "/home/agent/workspace"
restart = "on-failure"
max_restarts = 5
timeout = "2h"
grace_period = "30s"
session = "my-session"

[agent.env]
MY_VAR = "my_value"
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `harness` | string | (required) | `claude-code`, `opencode`, `gemini-cli`, `custom` |
| `prompt` | string | `""` | Prompt given to the agent (empty = interactive) |
| `prompt_file` | string | | Path to a prompt file (takes precedence over `prompt`) |
| `workdir` | string | `/home/agent/workspace` | Agent working directory |
| `restart` | string | `"no"` | `no`, `on-failure`, `always` |
| `max_restarts` | int | `0` (unlimited) | Max restart attempts (0 = no limit) |
| `timeout` | string | | Agent timeout as Go duration (e.g. `"2h"`) |
| `grace_period` | string | `"30s"` | SIGTERM grace period before force kill |
| `session` | string | harness name | tmux session name |
| `env` | map | `{}` | Environment variables for the agent process |

## Secrets

Secrets are files on disk in a directory (default `/run/stereos/secrets/`),
written by `stereosd` to an `admin` accessible tmpfs.
Each file represents one secret: **filename = env var
name**, **content = value**.

- Hidden files (`.` prefix) and directories are skipped
- Trailing newlines are trimmed
- If the directory does not exist, an empty map is returned
- Secrets are merged into the agent environment; `[agent.env]` values override
  secrets with the same name

## API

HTTP over Unix domain socket (`/run/stereos/agentd.sock`, mode `0660`,
group `admin`). This API is read-only.

| Method | Path | Response |
|--------|------|----------|
| `GET` | `/v1/health` | `{"state":"running","uptime_seconds":123}` |
| `GET` | `/v1/agents` | `[{"name":"claude-code","running":true,"session":"claude-code","restarts":0}]` |
| `GET` | `/v1/agents/{name}` | Single agent status (404 if not found, case-insensitive match) |


## NixOS module

The flake exports `nixosModules.default`:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `services.agentd.enable` | bool | `false` | Enable the `agentd` daemon |
| `services.agentd.package` | package | flake default | The `agentd` package |
| `services.agentd.extraArgs` | list of str | `[]` | Additional CLI arguments |

The systemd unit includes `tmux` and `sudo` in its `PATH` (required for
session management as the agent user). `DynamicUser=true` in the base module;
overridden to `false` by the stereOS NixOS module since `agentd` needs root to
manage tmux sessions for the agent user.

### Local development

`agentd` is designed to run inside stereOS managed by `systemd`. For local
testing, override the paths:

```bash
./build/agentd \
    -config /tmp/agentd-test/jcard.toml \
    -secret-dir /tmp/agentd-test/secrets \
    -tmux-socket /tmp/agentd-test/tmux.sock \
    -api-socket /tmp/agentd-test/agentd.sock \
    -debug
```

Debug mode logs the full command, working directory, environment key names,
and captures tmux pane output on exit.
