package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mashiro/google-bandwidth-controller/internal/protocol"
	"github.com/mashiro/google-bandwidth-controller/pkg/logger"
)

// MetricsCollector collects and aggregates bandwidth metrics
type MetricsCollector struct {
	jobs               sync.Map // map[string]*Job
	totalBytes         atomic.Int64
	currentBandwidth   atomic.Value // float64
	averageBandwidth   atomic.Value // float64
	logger             *logger.Logger
	config             *Config
	mu                 sync.RWMutex
	bandwidthSamples   []float64
	maxSamples         int
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(config *Config, log *logger.Logger) *MetricsCollector {
	mc := &MetricsCollector{
		logger:     log,
		config:     config,
		maxSamples: 60, // Keep 60 samples (1 minute at 1 sample/sec)
	}
	mc.currentBandwidth.Store(0.0)
	mc.averageBandwidth.Store(0.0)
	mc.bandwidthSamples = make([]float64, 0, mc.maxSamples)
	return mc
}

// RegisterJob registers a job for metrics collection
func (m *MetricsCollector) RegisterJob(commandID string, job *Job) {
	m.jobs.Store(commandID, job)
}

// DeregisterJob removes a job from metrics collection
func (m *MetricsCollector) DeregisterJob(commandID string) {
	m.jobs.Delete(commandID)
}

// Start begins collecting metrics at regular intervals
func (m *MetricsCollector) Start(ctx context.Context) {
	sampleInterval, _ := time.ParseDuration(m.config.Metrics.BandwidthSampleRate)
	if sampleInterval == 0 {
		sampleInterval = 1 * time.Second
	}

	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.collectSample()
		}
	}
}

// collectSample collects bandwidth metrics from all active jobs
func (m *MetricsCollector) collectSample() {
	totalBW := 0.0
	totalBytes := int64(0)

	m.jobs.Range(func(key, value interface{}) bool {
		job := value.(*Job)
		speed := job.CurrentSpeedMbps.Load().(float64)
		bytes := job.BytesDownloaded.Load()

		totalBW += speed
		totalBytes += bytes
		return true
	})

	// Store current bandwidth
	m.currentBandwidth.Store(totalBW)
	m.totalBytes.Store(totalBytes)

	// Update rolling average
	m.mu.Lock()
	m.bandwidthSamples = append(m.bandwidthSamples, totalBW)
	if len(m.bandwidthSamples) > m.maxSamples {
		m.bandwidthSamples = m.bandwidthSamples[1:]
	}

	// Calculate average
	if len(m.bandwidthSamples) > 0 {
		sum := 0.0
		for _, sample := range m.bandwidthSamples {
			sum += sample
		}
		avg := sum / float64(len(m.bandwidthSamples))
		m.averageBandwidth.Store(avg)
	}
	m.mu.Unlock()
}

// GetMetrics returns current metrics payload
func (m *MetricsCollector) GetMetrics() *protocol.MetricsPayload {
	currentBW := m.currentBandwidth.Load().(float64)
	avgBW := m.averageBandwidth.Load().(float64)
	totalBytes := m.totalBytes.Load()

	activeCount := 0
	m.jobs.Range(func(_, _ interface{}) bool {
		activeCount++
		return true
	})

	return &protocol.MetricsPayload{
		CurrentBandwidth: currentBW,
		AverageBandwidth: avgBW,
		BytesDownloaded:  totalBytes,
		ActiveCommands:   activeCount,
	}
}

// GetCommandMetrics returns metrics for all active commands
func (m *MetricsCollector) GetCommandMetrics() []protocol.CommandMetrics {
	var metrics []protocol.CommandMetrics

	m.jobs.Range(func(key, value interface{}) bool {
		job := value.(*Job)
		speed := job.CurrentSpeedMbps.Load().(float64)

		metrics = append(metrics, protocol.CommandMetrics{
			CommandID:       job.CommandID,
			URL:             job.URL,
			BytesDownloaded: job.BytesDownloaded.Load(),
			CurrentSpeed:    speed,
			Progress:        0,
		})
		return true
	})

	return metrics
}

// Reset resets all metrics
func (m *MetricsCollector) Reset() {
	m.totalBytes.Store(0)
	m.currentBandwidth.Store(0.0)
	m.averageBandwidth.Store(0.0)
	m.mu.Lock()
	m.bandwidthSamples = make([]float64, 0, m.maxSamples)
	m.mu.Unlock()
}
