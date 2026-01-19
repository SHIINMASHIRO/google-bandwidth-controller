package agent

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents agent configuration
type Config struct {
	Agent      AgentConfig      `yaml:"agent"`
	Controller ControllerConfig `yaml:"controller"`
	Download   DownloadConfig   `yaml:"download"`
	Metrics    MetricsConfig    `yaml:"metrics"`
	Logging    LoggingConfig    `yaml:"logging"`
}

// AgentConfig contains agent identification
type AgentConfig struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

// ControllerConfig contains controller connection settings
type ControllerConfig struct {
	Host                 string        `yaml:"host"`
	Port                 int           `yaml:"port"`
	AuthToken            string        `yaml:"auth_token"`
	ReconnectInterval    time.Duration `yaml:"reconnect_interval"`
	MaxReconnectAttempts int           `yaml:"max_reconnect_attempts"` // 0 = infinite
}

// DownloadConfig contains download settings
type DownloadConfig struct {
	Tool      string        `yaml:"tool"`
	OutputDir string        `yaml:"output_dir"`
	Cleanup   bool          `yaml:"cleanup"`
	Timeout   time.Duration `yaml:"timeout"`
}

// MetricsConfig contains metrics reporting settings
type MetricsConfig struct {
	ReportInterval      string `yaml:"report_interval"`
	BandwidthSampleRate string `yaml:"bandwidth_sample_rate"`
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
	if config.Controller.Port == 0 {
		config.Controller.Port = 8080
	}
	if config.Controller.ReconnectInterval == 0 {
		config.Controller.ReconnectInterval = 5 * time.Second
	}
	if config.Download.Tool == "" {
		config.Download.Tool = "wget"
	}
	if config.Download.OutputDir == "" {
		config.Download.OutputDir = "/tmp/bandwidth-test"
	}
	if config.Download.Timeout == 0 {
		config.Download.Timeout = 300 * time.Second
	}
	if config.Metrics.ReportInterval == "" {
		config.Metrics.ReportInterval = "5s"
	}
	if config.Metrics.BandwidthSampleRate == "" {
		config.Metrics.BandwidthSampleRate = "1s"
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
	if c.Agent.ID == "" {
		return fmt.Errorf("agent.id is required")
	}
	if c.Controller.Host == "" {
		return fmt.Errorf("controller.host is required")
	}
	if c.Controller.AuthToken == "" {
		return fmt.Errorf("controller.auth_token is required")
	}
	return nil
}
