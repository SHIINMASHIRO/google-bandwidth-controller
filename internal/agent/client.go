package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mashiro/google-bandwidth-controller/internal/protocol"
	"github.com/mashiro/google-bandwidth-controller/pkg/logger"
)

const (
	agentVersion = "1.0.0"
)

// Client is the agent WebSocket client
type Client struct {
	config         *Config
	conn           *websocket.Conn
	executor       *Executor
	metrics        *MetricsCollector
	logger         *logger.Logger
	reconnectChan  chan struct{}
	shutdownChan   chan struct{}
	sendChan       chan *protocol.Message
	startTime      time.Time
	mu             sync.Mutex
	connected      bool
	metricsCancel  context.CancelFunc
}

// NewClient creates a new agent client
func NewClient(config *Config, log *logger.Logger) *Client {
	metricsCollector := NewMetricsCollector(config, log)

	client := &Client{
		config:        config,
		logger:        log,
		metrics:       metricsCollector,
		reconnectChan: make(chan struct{}, 1),
		shutdownChan:  make(chan struct{}),
		sendChan:      make(chan *protocol.Message, 256),
		startTime:     time.Now(),
	}

	client.executor = NewExecutor(config, metricsCollector, log)

	return client
}

// Run starts the client and handles reconnection
func (c *Client) Run(ctx context.Context) error {
	c.logger.Info("Starting agent client")

	// Start metrics collection
	metricsCtx, metricsCancel := context.WithCancel(ctx)
	c.metricsCancel = metricsCancel
	go c.metrics.Start(metricsCtx)

	// Start metrics reporter
	go c.reportMetrics(ctx)

	// Initial connection
	if err := c.connect(); err != nil {
		c.logger.Errorw("Initial connection failed", "error", err)
		c.reconnectChan <- struct{}{}
	}

	// Main loop
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Shutting down agent client")
			c.disconnect()
			return nil

		case <-c.reconnectChan:
			c.handleReconnect(ctx)
		}
	}
}

// connect establishes WebSocket connection to controller
func (c *Client) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	wsURL := url.URL{
		Scheme: "ws",
		Host:   fmt.Sprintf("%s:%d", c.config.Controller.Host, c.config.Controller.Port),
		Path:   "/ws",
	}

	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Bearer %s", c.config.Controller.AuthToken))

	c.logger.Infow("Connecting to controller", "url", wsURL.String())

	conn, _, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.conn = conn
	c.connected = true

	c.logger.Info("Connected to controller")

	// Send registration
	if err := c.sendRegistration(); err != nil {
		c.logger.Errorw("Failed to send registration", "error", err)
		conn.Close()
		c.connected = false
		return err
	}

	// Start message handlers
	go c.readMessages()
	go c.writeMessages()

	return nil
}

// disconnect closes the WebSocket connection
func (c *Client) disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connected = false
}

// handleReconnect handles reconnection logic
func (c *Client) handleReconnect(ctx context.Context) {
	attempts := 0
	maxAttempts := c.config.Controller.MaxReconnectAttempts

	for {
		if maxAttempts > 0 && attempts >= maxAttempts {
			c.logger.Error("Max reconnection attempts reached, giving up")
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		attempts++
		c.logger.Infow("Attempting to reconnect",
			"attempt", attempts,
			"max_attempts", maxAttempts,
		)

		time.Sleep(c.config.Controller.ReconnectInterval)

		if err := c.connect(); err != nil {
			c.logger.Warnw("Reconnection failed", "error", err, "attempt", attempts)
			continue
		}

		c.logger.Info("Reconnected successfully")
		return
	}
}

// readMessages reads messages from the WebSocket connection
func (c *Client) readMessages() {
	defer func() {
		c.disconnect()
		c.reconnectChan <- struct{}{}
	}()

	for {
		var msg protocol.Message
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Errorw("WebSocket read error", "error", err)
			}
			return
		}

		c.processMessage(&msg)
	}
}

// writeMessages writes messages to the WebSocket connection
func (c *Client) writeMessages() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg := <-c.sendChan:
			c.mu.Lock()
			if c.conn == nil {
				c.mu.Unlock()
				return
			}
			err := c.conn.WriteJSON(msg)
			c.mu.Unlock()

			if err != nil {
				c.logger.Errorw("Failed to send message", "error", err)
				return
			}

		case <-ticker.C:
			// Send ping to keep connection alive
			c.mu.Lock()
			if c.conn != nil {
				c.conn.WriteMessage(websocket.PingMessage, nil)
			}
			c.mu.Unlock()
		}
	}
}

// processMessage processes incoming messages
func (c *Client) processMessage(msg *protocol.Message) {
	c.logger.Debugw("Received message", "type", msg.Type)

	switch msg.Type {
	case protocol.MsgTypeDownloadCommand:
		var cmd protocol.DownloadCommand
		if err := msg.UnmarshalPayload(&cmd); err != nil {
			c.logger.Errorw("Failed to unmarshal download command", "error", err)
			return
		}
		c.handleDownloadCommand(&cmd)

	case protocol.MsgTypeStopCommand:
		var cmd protocol.StopCommand
		if err := msg.UnmarshalPayload(&cmd); err != nil {
			c.logger.Errorw("Failed to unmarshal stop command", "error", err)
			return
		}
		c.handleStopCommand(&cmd)

	case protocol.MsgTypeHealthCheck:
		var hc protocol.HealthCheck
		if err := msg.UnmarshalPayload(&hc); err != nil {
			c.logger.Errorw("Failed to unmarshal health check", "error", err)
			return
		}
		c.handleHealthCheck(&hc)

	case protocol.MsgTypeShutdown:
		c.logger.Info("Received shutdown command")
		close(c.shutdownChan)

	default:
		c.logger.Warnw("Unknown message type", "type", msg.Type)
	}
}

// handleDownloadCommand handles a download command
func (c *Client) handleDownloadCommand(cmd *protocol.DownloadCommand) {
	c.logger.Infow("Received download command",
		"command_id", cmd.CommandID,
		"url", cmd.URL,
		"bandwidth", cmd.Bandwidth,
	)

	if err := c.executor.ExecuteDownload(cmd); err != nil {
		c.logger.Errorw("Failed to execute download", "error", err, "command_id", cmd.CommandID)
		c.sendError("EXEC_FAILED", fmt.Sprintf("Failed to execute download: %v", err))
	}
}

// handleStopCommand handles a stop command
func (c *Client) handleStopCommand(cmd *protocol.StopCommand) {
	c.logger.Infow("Received stop command", "command_id", cmd.CommandID)

	if err := c.executor.Stop(cmd.CommandID); err != nil {
		c.logger.Errorw("Failed to stop command", "error", err)
	}
}

// handleHealthCheck handles a health check
func (c *Client) handleHealthCheck(hc *protocol.HealthCheck) {
	response := protocol.HealthResponse{
		RequestID: hc.RequestID,
		Status:    "healthy",
		Timestamp: time.Now(),
	}

	msg, err := protocol.NewMessage(protocol.MsgTypeHealthResponse, c.config.Agent.ID, response)
	if err != nil {
		c.logger.Errorw("Failed to create health response", "error", err)
		return
	}

	c.sendChan <- msg
}

// sendRegistration sends registration to controller
func (c *Client) sendRegistration() error {
	payload := protocol.RegisterPayload{
		AgentID: c.config.Agent.ID,
		Name:    c.config.Agent.Name,
		Version: agentVersion,
		Capabilities: map[string]bool{
			"wget": true,
		},
		MaxBandwidth: 0, // Will be configured on controller side
	}

	msg, err := protocol.NewMessage(protocol.MsgTypeRegister, c.config.Agent.ID, payload)
	if err != nil {
		return err
	}

	return c.conn.WriteJSON(msg)
}

// reportMetrics periodically sends metrics to controller
func (c *Client) reportMetrics(ctx context.Context) {
	interval, _ := time.ParseDuration(c.config.Metrics.ReportInterval)
	if interval == 0 {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sendMetrics()
		}
	}
}

// sendMetrics sends current metrics to controller
func (c *Client) sendMetrics() {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	metrics := c.metrics.GetMetrics()
	metrics.CommandMetrics = c.metrics.GetCommandMetrics()

	msg, err := protocol.NewMessage(protocol.MsgTypeMetrics, c.config.Agent.ID, metrics)
	if err != nil {
		c.logger.Errorw("Failed to create metrics message", "error", err)
		return
	}

	c.sendChan <- msg
}

// sendError sends an error message to controller
func (c *Client) sendError(code, message string) {
	payload := protocol.ErrorPayload{
		Code:    code,
		Message: message,
	}

	msg, err := protocol.NewMessage(protocol.MsgTypeError, c.config.Agent.ID, payload)
	if err != nil {
		c.logger.Errorw("Failed to create error message", "error", err)
		return
	}

	select {
	case c.sendChan <- msg:
	default:
		c.logger.Warn("Send channel full, dropping error message")
	}
}

// GetStatus returns current agent status
func (c *Client) GetStatus() *protocol.StatusPayload {
	state := "idle"
	if c.executor.GetActiveJobs() > 0 {
		state = "downloading"
	}

	var activeCommands []string
	metrics := c.metrics.GetCommandMetrics()
	for _, m := range metrics {
		activeCommands = append(activeCommands, m.CommandID)
	}

	return &protocol.StatusPayload{
		State:          state,
		ActiveCommands: activeCommands,
		UptimeSeconds:  int64(time.Since(c.startTime).Seconds()),
	}
}
