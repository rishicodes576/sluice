// Command sluice is a high-performance, OpenAI-compatible LLM inference gateway
// with semantic caching, smart routing, rate limiting and observability.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rishicodes576/sluice/internal/config"
	"github.com/rishicodes576/sluice/internal/observability"
	"github.com/rishicodes576/sluice/internal/server"
)

// build metadata, overridable via -ldflags "-X main.version=..."
var (
	version = "dev"
	commit  = "none"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		os.Exit(cmdServe(os.Args[2:]))
	case "config-check":
		os.Exit(cmdConfigCheck(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Printf("sluice %s (commit %s)\n", version, commit)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `sluice — LLM inference gateway

Usage:
  sluice serve        [--config path] [--addr :8080]
  sluice config-check [--config path]
  sluice version

Environment:
  SLUICE_ADDR, SLUICE_LOG_LEVEL, SLUICE_LOG_FORMAT override config.
  ${VAR} references in the config file are expanded from the environment.
`)
}

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", os.Getenv("SLUICE_CONFIG"), "path to config file (yaml or json)")
	addr := fs.String("addr", "", "listen address override (e.g. :8080)")
	_ = fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		return 1
	}
	if *addr != "" {
		cfg.Server.Addr = *addr
	}

	log := observability.NewLogger(cfg.Logging.Level, cfg.Logging.Format)
	server.Version = version
	srv, err := server.New(cfg, log)
	if err != nil {
		log.Error("failed to build server", "err", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := srv.Run(ctx); err != nil {
		log.Error("server exited with error", "err", err)
		return 1
	}
	return 0
}

func cmdConfigCheck(args []string) int {
	fs := flag.NewFlagSet("config-check", flag.ExitOnError)
	cfgPath := fs.String("config", os.Getenv("SLUICE_CONFIG"), "path to config file")
	_ = fs.Parse(args)
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid:", err)
		return 1
	}
	fmt.Printf("ok: %d provider(s), strategy=%q, cache=%v\n",
		len(cfg.Providers), cfg.Router.Strategy, cfg.Cache.Enabled)
	return 0
}
