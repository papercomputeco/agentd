// Package main is the entry point for agentd (agent daemon).
//
// agentd is a service that starts, supervises, and stops configured agent
// harnesses (Claude Code, OpenCode, Gemini CLI, etc.) based on the
// jcard.toml [agent] configuration.
//
// Agent runtime management launches tmux sessions as the main harness for
// the AI agent user, which also allows the admin user to "tmux attach"
// to introspect the running agent.
//
// agentd communicates with stereosd over a local unix socket. stereosd writes
// secrets to a tmpfs for agentd to consume. agentd is started after
// stereosd via After=stereosd.service.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	agentd "github.com/papercomputeco/agentd/pkg"
)

func main() {
	configPath := flag.String("config", agentd.DefaultConfigPath, "path to jcard.toml configuration file")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("agentd: starting agent daemon")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	daemon := agentd.NewDaemon(*configPath)
	if err := daemon.Run(ctx); err != nil {
		log.Fatalf("agentd: fatal: %v", err)
		os.Exit(1)
	}

	log.Println("agentd: shutdown complete")
}
