package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mashiro/google-bandwidth-controller/internal/agent"
	"github.com/mashiro/google-bandwidth-controller/pkg/logger"
)

var (
	configPath = flag.String("config", "configs/agent.yaml", "Path to configuration file")
	version    = flag.Bool("version", false, "Print version and exit")
)

const agentVersion = "1.0.0"

func main() {
	flag.Parse()

	if *version {
		fmt.Printf("Agent version: %s\n", agentVersion)
		os.Exit(0)
	}

	// Load configuration
	config, err := agent.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log, err := logger.New(config.Logging.Level, config.Logging.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync()

	log.Infow("Starting bandwidth agent",
		"version", agentVersion,
		"agent_id", config.Agent.ID,
		"agent_name", config.Agent.Name,
		"controller", fmt.Sprintf("%s:%d", config.Controller.Host, config.Controller.Port),
	)

	// Create agent client
	client := agent.NewClient(config, log)

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Infow("Received shutdown signal", "signal", sig)
		cancel()
	}()

	// Run client
	if err := client.Run(ctx); err != nil {
		log.Errorw("Client error", "error", err)
		os.Exit(1)
	}

	log.Info("Agent shutdown complete")
}
