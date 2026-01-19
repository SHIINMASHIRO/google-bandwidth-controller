package protocol

import (
	"encoding/json"
	"time"
)

// MessageType defines the type of WebSocket message
type MessageType string

const (
	// Controller -> Agent messages
	MsgTypeDownloadCommand MessageType = "download_command"
	MsgTypeStopCommand     MessageType = "stop_command"
	MsgTypeHealthCheck     MessageType = "health_check"
	MsgTypeShutdown        MessageType = "shutdown"

	// Agent -> Controller messages
	MsgTypeRegister       MessageType = "register"
	MsgTypeMetrics        MessageType = "metrics"
	MsgTypeHealthResponse MessageType = "health_response"
	MsgTypeStatus         MessageType = "status"
	MsgTypeError          MessageType = "error"
)

// Message is the base structure for all WebSocket messages
type Message struct {
	Type      MessageType     `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	AgentID   string          `json:"agent_id"`
	Payload   json.RawMessage `json:"payload"`
}

// NewMessage creates a new message with the current timestamp
func NewMessage(msgType MessageType, agentID string, payload interface{}) (*Message, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &Message{
		Type:      msgType,
		Timestamp: time.Now(),
		AgentID:   agentID,
		Payload:   payloadBytes,
	}, nil
}

// UnmarshalPayload unmarshals the payload into the provided struct
func (m *Message) UnmarshalPayload(v interface{}) error {
	return json.Unmarshal(m.Payload, v)
}

// Command Payloads (Controller -> Agent)

// DownloadCommand instructs an agent to start downloading from a URL
type DownloadCommand struct {
	CommandID  string `json:"command_id"`
	URL        string `json:"url"`
	Duration   string `json:"duration"`    // e.g., "5m", "300s"
	Bandwidth  int64  `json:"bandwidth"`   // Mbps
	StartDelay string `json:"start_delay"` // Optional delay before starting
}

// StopCommand instructs an agent to stop downloading
type StopCommand struct {
	CommandID string `json:"command_id,omitempty"` // Empty = stop all
}

// HealthCheck is a ping message to check agent health
type HealthCheck struct {
	RequestID string `json:"request_id"`
}

// Response Payloads (Agent -> Controller)

// RegisterPayload is sent when an agent connects to register itself
type RegisterPayload struct {
	AgentID      string            `json:"agent_id"`
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Capabilities map[string]bool   `json:"capabilities"`
	MaxBandwidth int64             `json:"max_bandwidth"` // Mbps
}

// MetricsPayload contains bandwidth metrics from an agent
type MetricsPayload struct {
	CurrentBandwidth float64          `json:"current_bandwidth_mbps"` // Current Mbps
	AverageBandwidth float64          `json:"average_bandwidth_mbps"` // Over last interval
	BytesDownloaded  int64            `json:"bytes_downloaded"`
	ActiveCommands   int              `json:"active_commands"`
	CommandMetrics   []CommandMetrics `json:"command_metrics,omitempty"`
}

// CommandMetrics contains metrics for a specific download command
type CommandMetrics struct {
	CommandID       string  `json:"command_id"`
	URL             string  `json:"url"`
	BytesDownloaded int64   `json:"bytes_downloaded"`
	CurrentSpeed    float64 `json:"current_speed_mbps"`
	Progress        float64 `json:"progress"` // 0-100
}

// StatusPayload contains the current status of an agent
type StatusPayload struct {
	State          string   `json:"state"` // idle, downloading, stopping
	ActiveCommands []string `json:"active_commands"`
	UptimeSeconds  int64    `json:"uptime_seconds"`
	LastError      string   `json:"last_error,omitempty"`
}

// HealthResponse is the response to a health check
type HealthResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// ErrorPayload contains error information from an agent
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}
