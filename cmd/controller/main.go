package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mashiro/google-bandwidth-controller/internal/controller"
	"github.com/mashiro/google-bandwidth-controller/pkg/logger"
)

var (
	configPath = flag.String("config", "configs/controller.yaml", "Path to configuration file")
	version    = flag.Bool("version", false, "Print version and exit")
	dashboard  = flag.Bool("dashboard", true, "Show real-time dashboard")
)

const controllerVersion = "1.0.0"

func main() {
	flag.Parse()

	if *version {
		fmt.Printf("Controller version: %s\n", controllerVersion)
		os.Exit(0)
	}

	// Load configuration
	config, err := controller.LoadConfig(*configPath)
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

	log.Infow("Starting bandwidth controller",
		"version", controllerVersion,
		"ws_port", config.Server.WSPort,
		"http_port", config.Server.HTTPPort,
		"agents", len(config.Agents),
		"target_bandwidth_gbps", config.Bandwidth.TargetGbps,
	)

	// Create server
	server := controller.NewServer(config, log)

	// Create API server
	apiServer := controller.NewAPIServer(config, server, server.GetScheduler(), server.GetMetrics(), log)

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

	// Start servers
	errChan := make(chan error, 2)

	// Start WebSocket server
	go func() {
		if err := server.Start(ctx); err != nil {
			errChan <- fmt.Errorf("WebSocket server error: %w", err)
		}
	}()

	// Start API server
	go func() {
		if err := apiServer.Start(ctx); err != nil {
			errChan <- fmt.Errorf("API server error: %w", err)
		}
	}()

	// Start dashboard if enabled
	if *dashboard {
		go runDashboard(ctx, apiServer)
	}

	// Wait for error or context cancellation
	select {
	case err := <-errChan:
		log.Errorw("Server error", "error", err)
		cancel()
		time.Sleep(2 * time.Second) // Give time for graceful shutdown
		os.Exit(1)

	case <-ctx.Done():
		log.Info("Shutting down controller")
		time.Sleep(2 * time.Second) // Give time for graceful shutdown
	}

	log.Info("Controller shutdown complete")
}

// runDashboard displays real-time dashboard
func runDashboard(ctx context.Context, apiServer *controller.APIServer) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Initial display
	apiServer.PrintDashboard()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			apiServer.PrintDashboard()
		}
	}
}
