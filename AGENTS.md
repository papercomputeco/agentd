# AGENTS.md

### Project Overview

`agentd` is the AI agent management daemon for StereOS, a Linux based operating system
purpose built for AI agents.

**Language:** Go 1.25+
**Go Module:** `github.com/papercomputeco/agentd`

### Do

- Always use the Ginkgo/Gomega testing frameworks when writing tests.
- Always use `make` operations for development: use `make help` to understand
  the various operations available.
- Follow idiomatic Go and prefer using the `func NewExampleStruct() *ExampleStruct`
  paradigm seen throughout.

### Project Structure

- `pkg/` - Go packages. Use the `go doc` command to get the documentation on the various
  package's public API.
- `.github/` - GitHub metadata and action workflows.
- `flake.nix` - The development Nix flake which bundles all necessary dependencies for development.

### Build System

The project uses a Makefile for all build and dev operations. Utilize `make help`
to see all available commands.

Build artifacts land in the `build/` directory.
