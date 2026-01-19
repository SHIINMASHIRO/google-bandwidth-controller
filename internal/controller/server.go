package controller

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mashiro/google-bandwidth-controller/internal/protocol"
	"github.com/mashiro/google-bandwidth-controller/pkg/logger"
)

// Server is the controller WebSocket server
type Server struct {
	config    *Config
	upgrader  websocket.Upgrader
	clients   sync.Map // map[string]*Client (agentID -> Client)
	scheduler *Scheduler
	metrics   *MetricsAggregator
	logger    *logger.Logger
	mu        sync.RWMutex
}

// Client represents a connected agent
type Client struct {
	AgentID    string
	AgentName  string
	Conn       *websocket.Conn
	SendChan   chan *protocol.Message
	LastSeen   time.Time
	Info       *protocol.RegisterPayload
	mu         sync.Mutex
}

// NewServer creates a new controller server
func NewServer(config *Config, log *logger.Logger) *Server {
	server := &Server{
		config: config,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for simplicity
			},
		},
		logger: log,
	}

	server.metrics = NewMetricsAggregator(config, log)
	server.scheduler = NewScheduler(config, server, server.metrics, log)

	return server
}

// Start starts the WebSocket server
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)

	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.WSPort)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	s.logger.Infow("Starting WebSocket server", "address", addr)

	// Start scheduler
	go s.scheduler.Run(ctx)

	// Start client health checker
	go s.healthCheckClients(ctx)

	// Start server
	errChan := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		s.logger.Info("Shutting down WebSocket server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errChan:
		return err
	}
}

// handleWebSocket handles WebSocket connection requests
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Authenticate
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")

	if token != s.config.Server.AuthToken {
		s.logger.Warn("Unauthorized WebSocket connection attempt")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Upgrade connection
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Errorw("Failed to upgrade connection", "error", err)
		return
	}

	client := &Client{
		Conn:     conn,
		SendChan: make(chan *protocol.Message, 256),
		LastSeen: time.Now(),
	}

	s.logger.Infow("New WebSocket connection", "remote_addr", r.RemoteAddr)

	// Handle client communication
	go s.handleClient(client)
}

// handleClient handles communication with a connected agent
func (s *Server) handleClient(client *Client) {
	defer func() {
		client.Conn.Close()
		if client.AgentID != "" {
			s.clients.Delete(client.AgentID)
			s.scheduler.OnAgentDisconnect(client.AgentID)
			s.metrics.RemoveAgent(client.AgentID)
			s.logger.Infow("Agent disconnected", "agent_id", client.AgentID, "agent_name", client.AgentName)
		}
	}()

	// Start write pump
	go s.writeMessages(client)

	// Read messages
	for {
		var msg protocol.Message
		err := client.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				s.logger.Warnw("Client read error", "agent_id", client.AgentID, "error", err)
			}
			return
		}

		client.LastSeen = time.Now()
		s.processMessage(client, &msg)
	}
}

// writeMessages writes messages to client
func (s *Server) writeMessages(client *Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg := <-client.SendChan:
			client.mu.Lock()
			err := client.Conn.WriteJSON(msg)
			client.mu.Unlock()

			if err != nil {
				s.logger.Errorw("Failed to send message to agent",
					"agent_id", client.AgentID,
					"error", err,
				)
				return
			}

		case <-ticker.C:
			// Send ping
			client.mu.Lock()
			err := client.Conn.WriteMessage(websocket.PingMessage, nil)
			client.mu.Unlock()

			if err != nil {
				return
			}
		}
	}
}

// processMessage processes incoming messages from agents
func (s *Server) processMessage(client *Client, msg *protocol.Message) {
	switch msg.Type {
	case protocol.MsgTypeRegister:
		var payload protocol.RegisterPayload
		if err := msg.UnmarshalPayload(&payload); err != nil {
			s.logger.Errorw("Failed to unmarshal register payload", "error", err)
			return
		}
		s.handleRegister(client, &payload)

	case protocol.MsgTypeMetrics:
		var payload protocol.MetricsPayload
		if err := msg.UnmarshalPayload(&payload); err != nil {
			s.logger.Errorw("Failed to unmarshal metrics payload", "error", err)
			return
		}
		s.handleMetrics(client, &payload)

	case protocol.MsgTypeHealthResponse:
		// Just update last seen time (already done above)

	case protocol.MsgTypeStatus:
		var payload protocol.StatusPayload
		if err := msg.UnmarshalPayload(&payload); err != nil {
			s.logger.Errorw("Failed to unmarshal status payload", "error", err)
			return
		}
		s.handleStatus(client, &payload)

	case protocol.MsgTypeError:
		var payload protocol.ErrorPayload
		if err := msg.UnmarshalPayload(&payload); err != nil {
			s.logger.Errorw("Failed to unmarshal error payload", "error", err)
			return
		}
		s.handleError(client, &payload)

	default:
		s.logger.Warnw("Unknown message type from agent",
			"agent_id", client.AgentID,
			"type", msg.Type,
		)
	}
}

// handleRegister handles agent registration
func (s *Server) handleRegister(client *Client, payload *protocol.RegisterPayload) {
	client.AgentID = payload.AgentID
	client.AgentName = payload.Name
	client.Info = payload

	// Store client
	s.clients.Store(payload.AgentID, client)

	s.logger.Infow("Agent registered",
		"agent_id", payload.AgentID,
		"agent_name", payload.Name,
		"version", payload.Version,
	)

	// Notify scheduler
	s.scheduler.OnAgentConnect(payload.AgentID)
}

// handleMetrics handles metrics from agents
func (s *Server) handleMetrics(client *Client, payload *protocol.MetricsPayload) {
	if client.AgentID == "" {
		s.logger.Warn("Received metrics from unregistered agent")
		return
	}

	s.metrics.UpdateAgentMetrics(client.AgentID, payload)
}

// handleStatus handles status updates from agents
func (s *Server) handleStatus(client *Client, payload *protocol.StatusPayload) {
	s.logger.Debugw("Received status from agent",
		"agent_id", client.AgentID,
		"state", payload.State,
		"active_commands", len(payload.ActiveCommands),
	)
}

// handleError handles error messages from agents
func (s *Server) handleError(client *Client, payload *protocol.ErrorPayload) {
	s.logger.Errorw("Agent reported error",
		"agent_id", client.AgentID,
		"code", payload.Code,
		"message", payload.Message,
		"details", payload.Details,
	)
}

// SendToAgent sends a message to a specific agent
func (s *Server) SendToAgent(agentID string, msg *protocol.Message) error {
	clientVal, ok := s.clients.Load(agentID)
	if !ok {
		return fmt.Errorf("agent %s not connected", agentID)
	}

	client := clientVal.(*Client)

	select {
	case client.SendChan <- msg:
		return nil
	default:
		return fmt.Errorf("agent %s send channel full", agentID)
	}
}

// GetConnectedAgents returns a list of connected agent IDs
func (s *Server) GetConnectedAgents() []string {
	var agents []string
	s.clients.Range(func(key, value interface{}) bool {
		agents = append(agents, key.(string))
		return true
	})
	return agents
}

// GetClient returns a client by agent ID
func (s *Server) GetClient(agentID string) (*Client, bool) {
	clientVal, ok := s.clients.Load(agentID)
	if !ok {
		return nil, false
	}
	return clientVal.(*Client), true
}

// healthCheckClients periodically checks client health
func (s *Server) healthCheckClients(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkStaleClients()
		}
	}
}

// checkStaleClients removes clients that haven't been seen recently
func (s *Server) checkStaleClients() {
	staleThreshold := 120 * time.Second

	s.clients.Range(func(key, value interface{}) bool {
		client := value.(*Client)
		if time.Since(client.LastSeen) > staleThreshold {
			s.logger.Warnw("Removing stale client",
				"agent_id", client.AgentID,
				"last_seen", client.LastSeen,
			)
			client.Conn.Close()
			s.clients.Delete(key)
		}
		return true
	})
}

// GetScheduler returns the scheduler instance
func (s *Server) GetScheduler() *Scheduler {
	return s.scheduler
}

// GetMetrics returns the metrics aggregator instance
func (s *Server) GetMetrics() *MetricsAggregator {
	return s.metrics
}
