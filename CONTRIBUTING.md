# Contributing to `agentd`

## Development Environment

### Using Nix

The repo uses a `flake.nix` that provides all build dependencies. With `direnv`,
enter the dev shell via:

```bash
direnv allow
```

This drops you into a shell with:

- Go 1.25+
- Go tools (goimports, etc.)
- GNU Make

## Building

```bash
make build
```

This produces a static binary at `./build/agentd` (CGO disabled).

The `--config` flag controls which `jcard.toml` is loaded:

```bash
./build/agentd --config /path/to/jcard.toml
```

## Testing

All tests use the
[Ginkgo](https://onsi.github.io/ginkgo/) and
[Gomega](https://onsi.github.io/gomega/) testing frameworks.
To run tests:

```bash
make test
```

### tmux Tests

The `pkg/tmux/` tests require `tmux` to be installed. They create real tmux
sessions using isolated server sockets in temp directories. If tmux is not
available, these tests are automatically skipped.

### stereosd Client Tests

The `pkg/stereosd/` tests spin up a mock HTTP server on a temporary Unix socket
to simulate the `stereosd` API. No actual stereosd instance is needed.

## Linting and Formatting

```bash
make lint
make format
```

## Adding a New Harness

To add support for a new agent harness:

1. Create a new file in `pkg/harness/` (e.g., `myharness.go`)
2. Implement the `Harness` interface:

```go
type MyHarness struct{}

func (m *MyHarness) Name() string { return "my-harness" }

func (m *MyHarness) BuildCommand(prompt string) (string, []string) {
    if prompt == "" {
        return "mybin", nil
    }
    return "mybin", []string{"--prompt", prompt}
}
```

3. Register it in the `registry` map in `pkg/harness/harness.go`:

```go
var registry = map[string]func() Harness{
    // ...existing entries...
    "my-harness": func() Harness { return &MyHarness{} },
}
```

4. Add `"my-harness"` to `validHarnesses` in `pkg/config/config.go`
5. Add tests in `pkg/harness/harness_test.go`
