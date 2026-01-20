package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mashiro/google-bandwidth-controller/internal/dashboard"
	"github.com/mashiro/google-bandwidth-controller/pkg/logger"
)

// APIServer provides HTTP API for monitoring
type APIServer struct {
	config    *Config
	server    *Server
	scheduler *Scheduler
	metrics   *MetricsAggregator
	logger    *logger.Logger
}

// NewAPIServer creates a new API server
func NewAPIServer(config *Config, server *Server, scheduler *Scheduler, metrics *MetricsAggregator, log *logger.Logger) *APIServer {
	return &APIServer{
		config:    config,
		server:    server,
		scheduler: scheduler,
		metrics:   metrics,
		logger:    log,
	}
}

// Start starts the HTTP API server
func (a *APIServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Register API routes
	mux.HandleFunc("/metrics", a.handleMetrics)
	mux.HandleFunc("/status", a.handleStatus)
	mux.HandleFunc("/agents", a.handleAgents)
	mux.HandleFunc("/history", a.handleHistory)
	mux.HandleFunc("/stats", a.handleStats)
	mux.HandleFunc("/health", a.handleHealth)

	// Register dashboard routes
	dashboardHandler, err := dashboard.NewHandler()
	if err != nil {
		a.logger.Warnw("Failed to create dashboard handler", "error", err)
	} else {
		dashboardHandler.RegisterRoutes(mux)
		a.logger.Info("Web dashboard enabled at /")
	}

	addr := fmt.Sprintf("%s:%d", a.config.Server.Host, a.config.Server.HTTPPort)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: a.corsMiddleware(a.loggingMiddleware(mux)),
	}

	a.logger.Infow("Starting HTTP API server", "address", addr)

	errChan := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	select {
	case <-ctx.Done():
		a.logger.Info("Shutting down HTTP API server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errChan:
		return err
	}
}

// handleMetrics returns current bandwidth metrics
func (a *APIServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics := a.metrics.GetAggregated()

	response := map[string]interface{}{
		"total_bandwidth_mbps": metrics.TotalBandwidth,
		"total_bandwidth_gbps": metrics.TotalBandwidth / 1000.0,
		"active_agents":        metrics.ActiveAgents,
		"total_agents":         metrics.TotalAgents,
		"agent_breakdown":      metrics.AgentBreakdown,
		"timestamp":            metrics.Timestamp,
		"target_bandwidth_gbps": a.config.Bandwidth.TargetGbps,
		"target_percentage":    (metrics.TotalBandwidth / a.config.GetTargetBandwidthMbps()) * 100,
	}

	a.sendJSON(w, response)
}

// handleStatus returns system status
func (a *APIServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state := a.scheduler.GetState()
	metrics := a.metrics.GetAggregated()

	// Build active allocations info
	activeAllocations := make([]map[string]interface{}, 0)
	for agentID, alloc := range state.ActiveAgents {
		activeAllocations = append(activeAllocations, map[string]interface{}{
			"agent_id":   agentID,
			"bandwidth":  alloc.AllocatedBW,
			"start_time": alloc.StartTime,
			"url":        alloc.URL,
			"command_id": alloc.CurrentCommand,
		})
	}

	response := map[string]interface{}{
		"phase":                state.Phase,
		"active_agents":        len(state.ActiveAgents),
		"next_rotation":        state.NextRotation,
		"time_until_rotation":  time.Until(state.NextRotation).Round(time.Second).String(),
		"last_rotation":        state.LastRotation,
		"rotation_count":       state.RotationCount,
		"active_allocations":   activeAllocations,
		"target_bandwidth":     state.TargetTotalBW,
		"actual_bandwidth":     metrics.TotalBandwidth,
		"bandwidth_percentage": (metrics.TotalBandwidth / state.TargetTotalBW) * 100,
	}

	a.sendJSON(w, response)
}

// handleAgents returns information about all agents
func (a *APIServer) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	connectedAgents := a.server.GetConnectedAgents()
	connectedMap := make(map[string]bool)
	for _, id := range connectedAgents {
		connectedMap[id] = true
	}

	agents := make([]map[string]interface{}, 0)

	for _, agent := range a.config.Agents {
		isConnected := connectedMap[agent.ID]
		var lastSeen *time.Time
		var currentBandwidth float64

		if client, ok := a.server.GetClient(agent.ID); ok {
			lastSeen = &client.LastSeen
		}

		if agentMetrics := a.metrics.GetAgentMetrics(agent.ID); agentMetrics != nil {
			currentBandwidth = agentMetrics.CurrentBandwidth
		}

		agentInfo := map[string]interface{}{
			"id":                agent.ID,
			"name":              agent.Name,
			"host":              agent.Host,
			"max_bandwidth":     agent.MaxBandwidth,
			"region":            agent.Region,
			"connected":         isConnected,
			"current_bandwidth": currentBandwidth,
		}

		if lastSeen != nil {
			agentInfo["last_seen"] = lastSeen
		}

		agents = append(agents, agentInfo)
	}

	response := map[string]interface{}{
		"agents":       agents,
		"total":        len(agents),
		"connected":    len(connectedAgents),
		"disconnected": len(agents) - len(connectedAgents),
	}

	a.sendJSON(w, response)
}

// handleHistory returns historical bandwidth data
func (a *APIServer) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse duration parameter (default: 1 hour)
	durationStr := r.URL.Query().Get("duration")
	if durationStr == "" {
		durationStr = "1h"
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		http.Error(w, "Invalid duration parameter", http.StatusBadRequest)
		return
	}

	history := a.metrics.GetHistory(duration)

	response := map[string]interface{}{
		"history":       history,
		"duration":      duration.String(),
		"sample_count":  len(history),
	}

	a.sendJSON(w, response)
}

// handleStats returns statistical information
func (a *APIServer) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse duration parameter (default: 1 hour)
	durationStr := r.URL.Query().Get("duration")
	if durationStr == "" {
		durationStr = "1h"
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		http.Error(w, "Invalid duration parameter", http.StatusBadRequest)
		return
	}

	stats := a.metrics.GetStats(duration)

	response := map[string]interface{}{
		"duration":            duration.String(),
		"average_bandwidth":   stats.Average,
		"min_bandwidth":       stats.Min,
		"max_bandwidth":       stats.Max,
		"std_deviation":       stats.StandardDeviation,
		"sample_count":        stats.SampleCount,
		"target_bandwidth":    a.config.GetTargetBandwidthMbps(),
		"average_vs_target":   (stats.Average / a.config.GetTargetBandwidthMbps()) * 100,
	}

	a.sendJSON(w, response)
}

// handleHealth returns health check
func (a *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
	}

	a.sendJSON(w, response)
}

// sendJSON sends a JSON response
func (a *APIServer) sendJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		a.logger.Errorw("Failed to encode JSON response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// loggingMiddleware logs HTTP requests
func (a *APIServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create custom response writer to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		duration := time.Since(start)

		a.logger.Infow("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration_ms", duration.Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	})
}

// corsMiddleware adds CORS headers
func (a *APIServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// PrintDashboard prints a console dashboard
func (a *APIServer) PrintDashboard() {
	metrics := a.metrics.GetAggregated()
	state := a.scheduler.GetState()

	// Clear screen
	fmt.Print("\033[H\033[2J")

	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("          Google Bandwidth Controller Dashboard")
	fmt.Println("═══════════════════════════════════════════════════════════════")

	targetGbps := a.config.Bandwidth.TargetGbps
	currentGbps := metrics.TotalBandwidth / 1000.0
	percentage := (currentGbps / targetGbps) * 100

	fmt.Printf("\nTarget: %.2f Gbps | Current: %.2f Gbps (%.1f%%)\n",
		targetGbps, currentGbps, percentage)

	fmt.Printf("Active Agents: %d/%d\n", metrics.ActiveAgents, metrics.TotalAgents)
	fmt.Printf("Next Rotation: %s (%s)\n",
		state.NextRotation.Format("15:04:05"),
		time.Until(state.NextRotation).Round(time.Second))
	fmt.Printf("Phase: %s | Rotations: %d\n\n", state.Phase, state.RotationCount)

	fmt.Println("Agent Bandwidth Breakdown:")
	fmt.Println("─────────────────────────────────────────────────────────────")

	// Get agent names
	agentNames := make(map[string]string)
	for _, agent := range a.config.Agents {
		agentNames[agent.ID] = agent.Name
	}

	for agentID, bw := range metrics.AgentBreakdown {
		if bw > 0 {
			name := agentNames[agentID]
			if name == "" {
				name = agentID
			}

			bar := makeProgressBar(int(bw/50), 20)
			fmt.Printf("%-20s [%-20s] %7.0f Mbps\n", name, bar, bw)
		}
	}

	fmt.Println("\n═══════════════════════════════════════════════════════════════")
	fmt.Printf("Metrics API: http://%s:%d/metrics\n",
		a.config.Server.Host, a.config.Server.HTTPPort)
	fmt.Println("═══════════════════════════════════════════════════════════════")
}

// makeProgressBar creates a visual progress bar
func makeProgressBar(filled, total int) string {
	if filled > total {
		filled = total
	}
	if filled < 0 {
		filled = 0
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", total-filled)
	return bar
}
