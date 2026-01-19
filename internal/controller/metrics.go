package controller

import (
	"sync"
	"time"

	"github.com/mashiro/google-bandwidth-controller/internal/protocol"
	"github.com/mashiro/google-bandwidth-controller/pkg/logger"
)

// MetricsAggregator aggregates metrics from all agents
type MetricsAggregator struct {
	config       *Config
	logger       *logger.Logger
	mu           sync.RWMutex
	agentMetrics map[string]*AgentMetrics
	history      []AggregatedMetrics
	maxHistory   int
}

// AgentMetrics contains metrics for a single agent
type AgentMetrics struct {
	AgentID          string
	LastUpdate       time.Time
	CurrentBandwidth float64
	AverageBandwidth float64
	BytesTotal       int64
	ActiveCommands   int
	CommandMetrics   []protocol.CommandMetrics
}

// AggregatedMetrics contains aggregated metrics from all agents
type AggregatedMetrics struct {
	Timestamp        time.Time
	TotalBandwidth   float64
	AverageBandwidth float64
	ActiveAgents     int
	TotalAgents      int
	AgentBreakdown   map[string]float64
}

// NewMetricsAggregator creates a new metrics aggregator
func NewMetricsAggregator(config *Config, log *logger.Logger) *MetricsAggregator {
	return &MetricsAggregator{
		config:       config,
		logger:       log,
		agentMetrics: make(map[string]*AgentMetrics),
		history:      make([]AggregatedMetrics, 0),
		maxHistory:   1440, // 24 hours at 1 minute intervals
	}
}

// UpdateAgentMetrics updates metrics for a specific agent
func (m *MetricsAggregator) UpdateAgentMetrics(agentID string, metrics *protocol.MetricsPayload) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.agentMetrics[agentID] = &AgentMetrics{
		AgentID:          agentID,
		LastUpdate:       time.Now(),
		CurrentBandwidth: metrics.CurrentBandwidth,
		AverageBandwidth: metrics.AverageBandwidth,
		BytesTotal:       metrics.BytesDownloaded,
		ActiveCommands:   metrics.ActiveCommands,
		CommandMetrics:   metrics.CommandMetrics,
	}
}

// RemoveAgent removes an agent from metrics tracking
func (m *MetricsAggregator) RemoveAgent(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.agentMetrics, agentID)
}

// GetAggregated returns aggregated metrics from all agents
func (m *MetricsAggregator) GetAggregated() AggregatedMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agg := AggregatedMetrics{
		Timestamp:      time.Now(),
		AgentBreakdown: make(map[string]float64),
		TotalAgents:    len(m.agentMetrics),
	}

	totalBW := 0.0
	totalAvgBW := 0.0
	activeCount := 0

	for agentID, metrics := range m.agentMetrics {
		agg.AgentBreakdown[agentID] = metrics.CurrentBandwidth
		totalBW += metrics.CurrentBandwidth
		totalAvgBW += metrics.AverageBandwidth

		if metrics.ActiveCommands > 0 {
			activeCount++
		}
	}

	agg.TotalBandwidth = totalBW
	agg.ActiveAgents = activeCount

	if len(m.agentMetrics) > 0 {
		agg.AverageBandwidth = totalAvgBW / float64(len(m.agentMetrics))
	}

	return agg
}

// GetAgentMetrics returns metrics for a specific agent
func (m *MetricsAggregator) GetAgentMetrics(agentID string) *AgentMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.agentMetrics[agentID]
}

// GetAllAgentMetrics returns metrics for all agents
func (m *MetricsAggregator) GetAllAgentMetrics() map[string]*AgentMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to avoid race conditions
	copy := make(map[string]*AgentMetrics)
	for k, v := range m.agentMetrics {
		metricsCopy := *v
		copy[k] = &metricsCopy
	}

	return copy
}

// RecordSnapshot records current metrics to history
func (m *MetricsAggregator) RecordSnapshot() {
	agg := m.GetAggregated()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.history = append(m.history, agg)

	// Trim history if too large
	if len(m.history) > m.maxHistory {
		m.history = m.history[len(m.history)-m.maxHistory:]
	}
}

// GetHistory returns historical metrics
func (m *MetricsAggregator) GetHistory(duration time.Duration) []AggregatedMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cutoff := time.Now().Add(-duration)
	var result []AggregatedMetrics

	for _, snapshot := range m.history {
		if snapshot.Timestamp.After(cutoff) {
			result = append(result, snapshot)
		}
	}

	return result
}

// GetRecentHistory returns the last N snapshots
func (m *MetricsAggregator) GetRecentHistory(count int) []AggregatedMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if count > len(m.history) {
		count = len(m.history)
	}

	if count == 0 {
		return []AggregatedMetrics{}
	}

	start := len(m.history) - count
	result := make([]AggregatedMetrics, count)
	copy(result, m.history[start:])

	return result
}

// GetStats returns statistical information about bandwidth
func (m *MetricsAggregator) GetStats(duration time.Duration) BandwidthStats {
	history := m.GetHistory(duration)

	if len(history) == 0 {
		return BandwidthStats{}
	}

	var total, min, max float64
	min = history[0].TotalBandwidth
	max = history[0].TotalBandwidth

	for _, snapshot := range history {
		bw := snapshot.TotalBandwidth
		total += bw

		if bw < min {
			min = bw
		}
		if bw > max {
			max = bw
		}
	}

	avg := total / float64(len(history))

	// Calculate standard deviation
	var varianceSum float64
	for _, snapshot := range history {
		diff := snapshot.TotalBandwidth - avg
		varianceSum += diff * diff
	}
	stdDev := 0.0
	if len(history) > 1 {
		stdDev = varianceSum / float64(len(history)-1)
	}

	return BandwidthStats{
		Average:          avg,
		Min:              min,
		Max:              max,
		StandardDeviation: stdDev,
		SampleCount:      len(history),
	}
}

// BandwidthStats contains statistical information
type BandwidthStats struct {
	Average           float64 `json:"average"`
	Min               float64 `json:"min"`
	Max               float64 `json:"max"`
	StandardDeviation float64 `json:"std_dev"`
	SampleCount       int     `json:"sample_count"`
}
