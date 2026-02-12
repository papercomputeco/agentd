// Package main is the entry point for agentd (agent daemon).
//
// agentd is a service that starts, manages, and stops configured agent
// harnesses (Claude Code, OpenCode, Gemini CLI, etc.) based on the
// jcard.toml [agent] configuration.
//
// Agent runtime management launches tmux sessions as the main harness for
// the AI agent user, which also allows an admin user to "tmux attach"
// to introspect a running agent.
//
// agentd serves its own API over a Unix domain socket so that external
// consumers (stereosd, CLI tools, monitoring) can pull agent state.
// Configuration and secrets are read from disk via a reconciliation loop.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/papercomputeco/agentd/agentd"
)

func main() {
	configPath := flag.String("config", agentd.DefaultConfigPath, "path to jcard.toml configuration file")
	apiSocket := flag.String("api-socket", agentd.DefaultAPISocketPath, "path to agentd API unix socket")
	secretDir := flag.String("secret-dir", agentd.SecretDir, "path to secrets directory")
	tmuxSocket := flag.String("tmux-socket", agentd.TmuxSocketPath, "path to tmux server socket")
	debug := flag.Bool("debug", false, "enable debug logging (logs commands, env keys, captures pane output on exit)")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("agentd: starting agent daemon")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	daemon := agentd.NewDaemon(*configPath)
	if *apiSocket != agentd.DefaultAPISocketPath {
		daemon.SetAPISocketPath(*apiSocket)
	}
	if *secretDir != agentd.SecretDir {
		daemon.SetSecretDir(*secretDir)
	}
	if *tmuxSocket != agentd.TmuxSocketPath {
		daemon.SetTmuxSocketPath(*tmuxSocket)
	}
	if *debug {
		daemon.SetDebug(true)
	}

	if err := daemon.Run(ctx); err != nil {
		log.Fatalf("agentd: fatal: %v", err)
		os.Exit(1)
	}

	log.Println("agentd: shutdown complete")
}
