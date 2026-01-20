package controller

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mashiro/google-bandwidth-controller/internal/bandwidth"
	"github.com/mashiro/google-bandwidth-controller/internal/protocol"
	"github.com/mashiro/google-bandwidth-controller/pkg/logger"
)

// Scheduler manages bandwidth scheduling across agents
type Scheduler struct {
	config      *Config
	server      *Server
	metrics     *MetricsAggregator
	logger      *logger.Logger
	state       *SchedulerState
	mu          sync.RWMutex
	startTime   time.Time
	agentStatus map[string]*AgentStatus
}

// SchedulerState represents current scheduler state
type SchedulerState struct {
	Phase            string                      // ramping_up, stable, ramping_down
	ActiveAgents     map[string]*AgentAllocation // Currently active agent allocations
	NextRotation     time.Time
	TargetTotalBW    float64
	CurrentTotalBW   float64
	LastRotation     time.Time
	RotationCount    int
}

// AgentAllocation represents bandwidth allocation for an agent
type AgentAllocation struct {
	AgentID         string
	AllocatedBW     int64
	StartTime       time.Time
	PlannedDuration time.Duration
	CurrentCommand  string
	URL             string
}

// AgentStatus tracks agent usage statistics
type AgentStatus struct {
	AgentID      string
	LastUsed     time.Time
	TotalRuntime time.Duration
	UseCount     int
	Region       string
}

// NewScheduler creates a new scheduler
func NewScheduler(config *Config, server *Server, metrics *MetricsAggregator, log *logger.Logger) *Scheduler {
	state := &SchedulerState{
		Phase:          "idle",
		ActiveAgents:   make(map[string]*AgentAllocation),
		TargetTotalBW:  config.GetTargetBandwidthMbps(),
		NextRotation:   time.Now(),
	}

	agentStatus := make(map[string]*AgentStatus)
	for _, agent := range config.Agents {
		agentStatus[agent.ID] = &AgentStatus{
			AgentID:  agent.ID,
			LastUsed: time.Now().Add(-24 * time.Hour), // Start as if not used recently
			Region:   agent.Region,
		}
	}

	return &Scheduler{
		config:      config,
		server:      server,
		metrics:     metrics,
		logger:      log,
		state:       state,
		startTime:   time.Now(),
		agentStatus: agentStatus,
	}
}

// Run starts the scheduler main loop
func (s *Scheduler) Run(ctx context.Context) {
	s.logger.Info("Starting scheduler")

	// Initial schedule
	s.performRotation()

	// Snapshot metrics periodically
	go s.snapshotMetrics(ctx)

	// Main evaluation loop
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Stopping scheduler")
			s.stopAllAgents()
			return

		case <-ticker.C:
			s.evaluateAndAdjust()
		}
	}
}

// evaluateAndAdjust checks if rotation is needed or fine-tuning required
func (s *Scheduler) evaluateAndAdjust() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if it's time for rotation
	if time.Now().After(s.state.NextRotation) {
		s.logger.Info("Rotation time reached, performing rotation")
		go s.performRotation()
		return
	}

	// Update current bandwidth from metrics
	agg := s.metrics.GetAggregated()
	s.state.CurrentTotalBW = agg.TotalBandwidth

	// Adaptive bandwidth adjustment: check if agents need more tasks
	s.adjustBandwidthIfNeeded(agg)
}

// adjustBandwidthIfNeeded sends additional download tasks if agents are underperforming
func (s *Scheduler) adjustBandwidthIfNeeded(agg AggregatedMetrics) {
	// Only adjust during stable phase
	if s.state.Phase != "stable" {
		return
	}

	// Check each active agent
	for agentID, alloc := range s.state.ActiveAgents {
		// Get current bandwidth for this agent
		agentBW, exists := agg.AgentBreakdown[agentID]
		if !exists {
			continue
		}

		targetBW := float64(alloc.AllocatedBW)
		tolerance := s.config.Bandwidth.Tolerance // e.g., 0.15 = 15%

		// Calculate how much bandwidth is missing
		minAcceptable := targetBW * (1 - tolerance)

		if agentBW < minAcceptable {
			// Agent is underperforming, calculate deficit
			deficit := targetBW - agentBW
			deficitPercent := (deficit / targetBW) * 100

			// Only log and adjust if deficit is significant (> 20%)
			if deficitPercent > 20 {
				s.logger.Infow("Agent bandwidth deficit detected",
					"agent_id", agentID,
					"target", targetBW,
					"current", agentBW,
					"deficit", deficit,
					"deficit_percent", deficitPercent,
				)

				// Send additional download task to boost bandwidth
				go s.boostAgentBandwidth(agentID, int64(deficit))
			}
		}
	}
}

// boostAgentBandwidth sends an additional download task to increase bandwidth
func (s *Scheduler) boostAgentBandwidth(agentID string, additionalBW int64) {
	// Minimum boost is 100 Mbps
	if additionalBW < 100 {
		additionalBW = 100
	}

	// Cap at 500 Mbps per boost to avoid overwhelming
	if additionalBW > 500 {
		additionalBW = 500
	}

	url := s.selectRandomURL()
	commandID := uuid.New().String()

	// Duration: until next rotation
	s.mu.RLock()
	duration := time.Until(s.state.NextRotation)
	if duration < 30*time.Second {
		s.mu.RUnlock()
		return // Too close to rotation, skip
	}
	s.mu.RUnlock()

	cmd := protocol.DownloadCommand{
		CommandID: commandID,
		URL:       url,
		Duration:  duration.String(),
		Bandwidth: additionalBW,
	}

	msg, err := protocol.NewMessage(protocol.MsgTypeDownloadCommand, agentID, cmd)
	if err != nil {
		s.logger.Errorw("Failed to create boost command", "error", err)
		return
	}

	if err := s.server.SendToAgent(agentID, msg); err != nil {
		s.logger.Errorw("Failed to send boost command to agent",
			"agent_id", agentID,
			"error", err,
		)
		return
	}

	s.logger.Infow("Sent bandwidth boost to agent",
		"agent_id", agentID,
		"additional_bandwidth", additionalBW,
		"url", url,
	)
}

// performRotation performs a full scheduling rotation
func (s *Scheduler) performRotation() {
	s.mu.Lock()
	s.state.Phase = "ramping_down"
	s.mu.Unlock()

	s.logger.Info("Starting rotation cycle")

	// Phase 1: Calculate new schedule
	newConcurrency := s.calculateConcurrency()
	selectedAgents := s.selectAgents(newConcurrency)
	allocations := s.allocateBandwidth(selectedAgents)

	s.logger.Infow("New schedule calculated",
		"concurrency", newConcurrency,
		"selected_agents", len(selectedAgents),
	)

	// Phase 2: Ramp down agents that should stop
	s.mu.Lock()
	toStop := s.findAgentsToStop(allocations)
	s.mu.Unlock()

	if len(toStop) > 0 {
		s.rampDownAgents(toStop)
	}

	// Phase 3: Ramp up new agents
	s.mu.Lock()
	s.state.Phase = "ramping_up"
	s.mu.Unlock()

	toStart := s.findAgentsToStart(allocations)
	if len(toStart) > 0 {
		s.rampUpAgents(toStart)
	}

	// Phase 4: Update state
	s.mu.Lock()
	s.state.Phase = "stable"
	s.state.ActiveAgents = allocations
	s.state.LastRotation = time.Now()
	s.state.RotationCount++
	s.mu.Unlock()

	// Phase 5: Schedule next rotation
	s.scheduleNextRotation()

	s.logger.Infow("Rotation cycle completed",
		"active_agents", len(allocations),
		"rotation_count", s.state.RotationCount,
	)
}

// calculateConcurrency calculates number of concurrent agents using sine waves
func (s *Scheduler) calculateConcurrency() int {
	elapsed := time.Since(s.startTime).Seconds()
	return bandwidth.CalculateConcurrency(
		elapsed,
		s.config.Scheduler.MinConcurrent,
		s.config.Scheduler.MaxConcurrent,
		s.config.Scheduler.TimingRandomness,
	)
}

// selectAgents selects which agents to use based on weighted random selection
func (s *Scheduler) selectAgents(count int) []AgentConfig {
	connectedAgents := s.server.GetConnectedAgents()
	available := make([]AgentConfig, 0)

	// Filter to only connected agents
	for _, agent := range s.config.Agents {
		for _, connectedID := range connectedAgents {
			if agent.ID == connectedID {
				available = append(available, agent)
				break
			}
		}
	}

	if len(available) == 0 {
		s.logger.Warn("No connected agents available")
		return []AgentConfig{}
	}

	if count > len(available) {
		count = len(available)
	}

	// Calculate weights
	weights := make([]float64, len(available))
	for i, agent := range available {
		weight := 1.0

		// Higher weight for higher capacity
		weight *= float64(agent.MaxBandwidth) / 1000.0

		// Higher weight if not recently used
		status := s.agentStatus[agent.ID]
		timeSinceUse := time.Since(status.LastUsed).Minutes()
		weight *= bandwidth.ClampFloat(timeSinceUse/10.0, 0.5, 2.0)

		// Geographic diversity bonus
		if !s.isRegionOverused(agent.Region) {
			weight *= 1.5
		}

		weights[i] = weight
	}

	// Weighted random selection
	selectedIndices := bandwidth.WeightedRandomSelection(count, weights)

	selected := make([]AgentConfig, len(selectedIndices))
	for i, idx := range selectedIndices {
		selected[i] = available[idx]
	}

	return selected
}

// allocateBandwidth allocates bandwidth across selected agents
func (s *Scheduler) allocateBandwidth(agents []AgentConfig) map[string]*AgentAllocation {
	if len(agents) == 0 {
		return make(map[string]*AgentAllocation)
	}

	targetTotal := s.state.TargetTotalBW
	numAgents := len(agents)

	allocations := make(map[string]*AgentAllocation)

	// Get bandwidth allocations
	bandwidthAllocations := bandwidth.AllocateBandwidth(
		targetTotal,
		numAgents,
		s.config.Scheduler.ServerBandwidthMin,
		s.config.Scheduler.ServerBandwidthMax,
		s.config.Scheduler.BandwidthRandomness,
	)

	// Apply agent capacity constraints
	for i, agent := range agents {
		allocated := bandwidthAllocations[i]

		// Don't exceed agent max capacity
		if allocated > agent.MaxBandwidth {
			allocated = agent.MaxBandwidth
		}

		allocations[agent.ID] = &AgentAllocation{
			AgentID:     agent.ID,
			AllocatedBW: allocated,
			StartTime:   time.Now(),
		}
	}

	return allocations
}

// findAgentsToStop finds agents that should be stopped
func (s *Scheduler) findAgentsToStop(newAllocations map[string]*AgentAllocation) []string {
	var toStop []string

	for agentID := range s.state.ActiveAgents {
		if _, shouldContinue := newAllocations[agentID]; !shouldContinue {
			toStop = append(toStop, agentID)
		}
	}

	return toStop
}

// findAgentsToStart finds agents that should be started
func (s *Scheduler) findAgentsToStart(newAllocations map[string]*AgentAllocation) []*AgentAllocation {
	var toStart []*AgentAllocation

	for agentID, alloc := range newAllocations {
		if _, alreadyActive := s.state.ActiveAgents[agentID]; !alreadyActive {
			toStart = append(toStart, alloc)
		}
	}

	return toStart
}

// rampDownAgents gradually stops agents
func (s *Scheduler) rampDownAgents(agentIDs []string) {
	if len(agentIDs) == 0 {
		return
	}

	s.logger.Infow("Ramping down agents", "count", len(agentIDs))

	delays := bandwidth.CalculateStagger(len(agentIDs), s.config.Scheduler.RampDownDuration.Seconds())

	for i, agentID := range agentIDs {
		delay := time.Duration(delays[i] * float64(time.Second))

		go func(id string, d time.Duration) {
			time.Sleep(d)
			s.stopAgent(id)
		}(agentID, delay)
	}

	// Wait for all to complete
	time.Sleep(s.config.Scheduler.RampDownDuration)
}

// rampUpAgents gradually starts agents
func (s *Scheduler) rampUpAgents(allocations []*AgentAllocation) {
	if len(allocations) == 0 {
		return
	}

	s.logger.Infow("Ramping up agents", "count", len(allocations))

	delays := bandwidth.CalculateStagger(len(allocations), s.config.Scheduler.RampUpDuration.Seconds())

	for i, alloc := range allocations {
		delay := time.Duration(delays[i] * float64(time.Second))

		go func(a *AgentAllocation, d time.Duration) {
			time.Sleep(d)
			s.startAgent(a)
		}(alloc, delay)
	}
}

// startAgent sends download command to an agent
func (s *Scheduler) startAgent(alloc *AgentAllocation) {
	// Select random URL
	url := s.selectRandomURL()

	// Duration: until next rotation + buffer
	s.mu.RLock()
	duration := time.Until(s.state.NextRotation) + 60*time.Second
	s.mu.RUnlock()

	commandID := uuid.New().String()

	cmd := protocol.DownloadCommand{
		CommandID: commandID,
		URL:       url,
		Duration:  duration.String(),
		Bandwidth: alloc.AllocatedBW,
	}

	msg, err := protocol.NewMessage(protocol.MsgTypeDownloadCommand, alloc.AgentID, cmd)
	if err != nil {
		s.logger.Errorw("Failed to create download command", "error", err)
		return
	}

	if err := s.server.SendToAgent(alloc.AgentID, msg); err != nil {
		s.logger.Errorw("Failed to send download command to agent",
			"agent_id", alloc.AgentID,
			"error", err,
		)
		return
	}

	// Update allocation
	alloc.CurrentCommand = commandID
	alloc.URL = url
	alloc.PlannedDuration = duration

	// Update agent status
	if status, ok := s.agentStatus[alloc.AgentID]; ok {
		status.LastUsed = time.Now()
		status.UseCount++
	}

	s.logger.Infow("Started agent",
		"agent_id", alloc.AgentID,
		"bandwidth", alloc.AllocatedBW,
		"duration", duration.Round(time.Second),
		"url", url,
	)
}

// stopAgent sends stop command to an agent
func (s *Scheduler) stopAgent(agentID string) {
	cmd := protocol.StopCommand{
		CommandID: "", // Empty = stop all
	}

	msg, err := protocol.NewMessage(protocol.MsgTypeStopCommand, agentID, cmd)
	if err != nil {
		s.logger.Errorw("Failed to create stop command", "error", err)
		return
	}

	if err := s.server.SendToAgent(agentID, msg); err != nil {
		s.logger.Warnw("Failed to send stop command to agent",
			"agent_id", agentID,
			"error", err,
		)
		return
	}

	s.logger.Infow("Stopped agent", "agent_id", agentID)
}

// stopAllAgents stops all active agents
func (s *Scheduler) stopAllAgents() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for agentID := range s.state.ActiveAgents {
		s.stopAgent(agentID)
	}
}

// selectRandomURL selects a random download URL
func (s *Scheduler) selectRandomURL() string {
	if len(s.config.URLs) == 0 {
		return "https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb"
	}

	return s.config.URLs[rand.Intn(len(s.config.URLs))]
}

// scheduleNextRotation schedules the next rotation
func (s *Scheduler) scheduleNextRotation() {
	minInterval := s.config.Scheduler.RotationIntervalMin.Seconds()
	maxInterval := s.config.Scheduler.RotationIntervalMax.Seconds()
	jitterFactor := s.config.Scheduler.TimingRandomness

	durationSeconds := bandwidth.RandomDuration(minInterval, maxInterval, jitterFactor)
	nextInterval := time.Duration(durationSeconds * float64(time.Second))

	s.mu.Lock()
	s.state.NextRotation = time.Now().Add(nextInterval)
	s.mu.Unlock()

	s.logger.Infow("Next rotation scheduled",
		"interval", nextInterval.Round(time.Second),
		"next_time", s.state.NextRotation.Format("15:04:05"),
	)
}

// isRegionOverused checks if a region is overused in current active agents
func (s *Scheduler) isRegionOverused(region string) bool {
	if region == "" {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	regionCount := 0
	totalActive := len(s.state.ActiveAgents)

	for agentID := range s.state.ActiveAgents {
		if status, ok := s.agentStatus[agentID]; ok {
			if status.Region == region {
				regionCount++
			}
		}
	}

	if totalActive == 0 {
		return false
	}

	// Consider overused if more than 50% are from same region
	return float64(regionCount)/float64(totalActive) > 0.5
}

// OnAgentConnect is called when an agent connects
func (s *Scheduler) OnAgentConnect(agentID string) {
	s.logger.Infow("Agent connected to scheduler", "agent_id", agentID)

	// Agent will be included in next rotation
}

// OnAgentDisconnect is called when an agent disconnects
func (s *Scheduler) OnAgentDisconnect(agentID string) {
	s.logger.Infow("Agent disconnected from scheduler", "agent_id", agentID)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove from active agents
	delete(s.state.ActiveAgents, agentID)
}

// GetState returns current scheduler state (for API)
func (s *Scheduler) GetState() SchedulerState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy
	state := *s.state
	state.ActiveAgents = make(map[string]*AgentAllocation)
	for k, v := range s.state.ActiveAgents {
		allocCopy := *v
		state.ActiveAgents[k] = &allocCopy
	}

	return state
}

// snapshotMetrics periodically records metrics snapshots
func (s *Scheduler) snapshotMetrics(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.metrics.RecordSnapshot()
		}
	}
}
