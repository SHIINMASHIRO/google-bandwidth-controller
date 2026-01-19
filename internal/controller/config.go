package controller

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents controller configuration
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Bandwidth BandwidthConfig `yaml:"bandwidth"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
	Agents    []AgentConfig   `yaml:"agents"`
	URLs      []string        `yaml:"download_urls"`
	Metrics   MetricsConfig   `yaml:"metrics"`
	Logging   LoggingConfig   `yaml:"logging"`
}

// ServerConfig contains server settings
type ServerConfig struct {
	Host      string `yaml:"host"`
	WSPort    int    `yaml:"ws_port"`
	HTTPPort  int    `yaml:"http_port"`
	AuthToken string `yaml:"auth_token"`
}

// BandwidthConfig contains bandwidth target settings
type BandwidthConfig struct {
	TargetGbps float64 `yaml:"target_gbps"`
	Tolerance  float64 `yaml:"tolerance"`
}

// SchedulerConfig contains scheduling parameters
type SchedulerConfig struct {
	MinConcurrent        int           `yaml:"min_concurrent"`
	MaxConcurrent        int           `yaml:"max_concurrent"`
	RotationIntervalMin  time.Duration `yaml:"rotation_interval_min"`
	RotationIntervalMax  time.Duration `yaml:"rotation_interval_max"`
	ServerBandwidthMin   int64         `yaml:"server_bandwidth_min"`
	ServerBandwidthMax   int64         `yaml:"server_bandwidth_max"`
	RampUpDuration       time.Duration `yaml:"ramp_up_duration"`
	RampDownDuration     time.Duration `yaml:"ramp_down_duration"`
	TimingRandomness     float64       `yaml:"timing_randomness"`
	BandwidthRandomness  float64       `yaml:"bandwidth_randomness"`
}

// AgentConfig contains agent pool configuration
type AgentConfig struct {
	ID           string `yaml:"id"`
	Host         string `yaml:"host"`
	Name         string `yaml:"name"`
	MaxBandwidth int64  `yaml:"max_bandwidth"` // Mbps
	Region       string `yaml:"region,omitempty"`
}

// MetricsConfig contains metrics settings
type MetricsConfig struct {
	CollectionInterval string `yaml:"collection_interval"`
	RetentionPeriod    string `yaml:"retention_period"`
	AggregationWindow  string `yaml:"aggregation_window"`
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	Output string `yaml:"output"`
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Set defaults
	if config.Server.Host == "" {
		config.Server.Host = "0.0.0.0"
	}
	if config.Server.WSPort == 0 {
		config.Server.WSPort = 8080
	}
	if config.Server.HTTPPort == 0 {
		config.Server.HTTPPort = 9090
	}
	if config.Bandwidth.TargetGbps == 0 {
		config.Bandwidth.TargetGbps = 10.0
	}
	if config.Bandwidth.Tolerance == 0 {
		config.Bandwidth.Tolerance = 0.15
	}
	if config.Scheduler.MinConcurrent == 0 {
		config.Scheduler.MinConcurrent = 2
	}
	if config.Scheduler.MaxConcurrent == 0 {
		config.Scheduler.MaxConcurrent = 8
	}
	if config.Scheduler.RotationIntervalMin == 0 {
		config.Scheduler.RotationIntervalMin = 30 * time.Second
	}
	if config.Scheduler.RotationIntervalMax == 0 {
		config.Scheduler.RotationIntervalMax = 180 * time.Second
	}
	if config.Scheduler.ServerBandwidthMin == 0 {
		config.Scheduler.ServerBandwidthMin = 400
	}
	if config.Scheduler.ServerBandwidthMax == 0 {
		config.Scheduler.ServerBandwidthMax = 1200
	}
	if config.Scheduler.RampUpDuration == 0 {
		config.Scheduler.RampUpDuration = 15 * time.Second
	}
	if config.Scheduler.RampDownDuration == 0 {
		config.Scheduler.RampDownDuration = 20 * time.Second
	}
	if config.Scheduler.TimingRandomness == 0 {
		config.Scheduler.TimingRandomness = 0.3
	}
	if config.Scheduler.BandwidthRandomness == 0 {
		config.Scheduler.BandwidthRandomness = 0.25
	}
	if config.Metrics.CollectionInterval == "" {
		config.Metrics.CollectionInterval = "5s"
	}
	if config.Metrics.RetentionPeriod == "" {
		config.Metrics.RetentionPeriod = "24h"
	}
	if config.Metrics.AggregationWindow == "" {
		config.Metrics.AggregationWindow = "1m"
	}
	if config.Logging.Level == "" {
		config.Logging.Level = "info"
	}
	if config.Logging.Format == "" {
		config.Logging.Format = "json"
	}

	return &config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Server.AuthToken == "" {
		return fmt.Errorf("server.auth_token is required")
	}
	if len(c.Agents) == 0 {
		return fmt.Errorf("at least one agent must be configured")
	}
	if len(c.URLs) == 0 {
		return fmt.Errorf("at least one download URL must be configured")
	}
	if c.Scheduler.MinConcurrent > c.Scheduler.MaxConcurrent {
		return fmt.Errorf("scheduler.min_concurrent cannot be greater than max_concurrent")
	}
	if c.Scheduler.MinConcurrent > len(c.Agents) {
		return fmt.Errorf("scheduler.min_concurrent cannot be greater than number of agents")
	}

	// Validate agents
	agentIDs := make(map[string]bool)
	for _, agent := range c.Agents {
		if agent.ID == "" {
			return fmt.Errorf("agent ID is required")
		}
		if agentIDs[agent.ID] {
			return fmt.Errorf("duplicate agent ID: %s", agent.ID)
		}
		agentIDs[agent.ID] = true

		if agent.MaxBandwidth <= 0 {
			return fmt.Errorf("agent %s must have max_bandwidth > 0", agent.ID)
		}
	}

	return nil
}

// GetTargetBandwidthMbps returns target bandwidth in Mbps
func (c *Config) GetTargetBandwidthMbps() float64 {
	return c.Bandwidth.TargetGbps * 1000
}
