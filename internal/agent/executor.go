package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/mashiro/google-bandwidth-controller/internal/protocol"
	"github.com/mashiro/google-bandwidth-controller/pkg/logger"
)

// Executor handles download command execution
type Executor struct {
	config     *Config
	activeJobs sync.Map // map[string]*Job
	logger     *logger.Logger
	metrics    *MetricsCollector
}

// Job represents a running download job
type Job struct {
	CommandID       string
	URL             string
	Cmd             *exec.Cmd
	StartTime       time.Time
	BytesDownloaded atomic.Int64
	CurrentSpeedMbps atomic.Value // float64
	Cancel          context.CancelFunc
	mu              sync.Mutex
}

// NewExecutor creates a new command executor
func NewExecutor(config *Config, metrics *MetricsCollector, log *logger.Logger) *Executor {
	// Ensure output directory exists
	os.MkdirAll(config.Download.OutputDir, 0755)

	return &Executor{
		config:  config,
		logger:  log,
		metrics: metrics,
	}
}

// ExecuteDownload starts a download command
func (e *Executor) ExecuteDownload(cmd *protocol.DownloadCommand) error {
	// Check if command already exists
	if _, exists := e.activeJobs.Load(cmd.CommandID); exists {
		return fmt.Errorf("command %s already running", cmd.CommandID)
	}

	ctx, cancel := context.WithCancel(context.Background())

	job := &Job{
		CommandID: cmd.CommandID,
		URL:       cmd.URL,
		StartTime: time.Now(),
		Cancel:    cancel,
	}
	job.CurrentSpeedMbps.Store(0.0)

	e.activeJobs.Store(cmd.CommandID, job)
	e.logger.Infow("Starting download command",
		"command_id", cmd.CommandID,
		"url", cmd.URL,
		"bandwidth", cmd.Bandwidth,
		"duration", cmd.Duration,
	)

	go e.runDownload(ctx, cmd, job)

	return nil
}

// Stop stops a specific command or all commands if commandID is empty
func (e *Executor) Stop(commandID string) error {
	if commandID == "" {
		// Stop all commands
		e.activeJobs.Range(func(key, value interface{}) bool {
			job := value.(*Job)
			job.Cancel()
			return true
		})
		e.logger.Info("Stopping all download commands")
		return nil
	}

	// Stop specific command
	if jobVal, exists := e.activeJobs.Load(commandID); exists {
		job := jobVal.(*Job)
		job.Cancel()
		e.logger.Infow("Stopping download command", "command_id", commandID)
		return nil
	}

	return fmt.Errorf("command %s not found", commandID)
}

// GetActiveJobs returns the number of active jobs
func (e *Executor) GetActiveJobs() int {
	count := 0
	e.activeJobs.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// GetJobMetrics returns metrics for all active jobs
func (e *Executor) GetJobMetrics() []protocol.CommandMetrics {
	var metrics []protocol.CommandMetrics

	e.activeJobs.Range(func(key, value interface{}) bool {
		job := value.(*Job)
		speed := job.CurrentSpeedMbps.Load().(float64)

		metrics = append(metrics, protocol.CommandMetrics{
			CommandID:       job.CommandID,
			URL:             job.URL,
			BytesDownloaded: job.BytesDownloaded.Load(),
			CurrentSpeed:    speed,
			Progress:        0, // wget doesn't provide accurate progress
		})
		return true
	})

	return metrics
}

// runDownload executes the actual download using wget
func (e *Executor) runDownload(ctx context.Context, cmd *protocol.DownloadCommand, job *Job) {
	defer e.activeJobs.Delete(cmd.CommandID)
	defer e.metrics.DeregisterJob(cmd.CommandID)

	// Parse duration
	duration, err := time.ParseDuration(cmd.Duration)
	if err != nil {
		e.logger.Errorw("Invalid duration", "command_id", cmd.CommandID, "error", err)
		return
	}

	// Handle start delay if specified
	if cmd.StartDelay != "" {
		delay, err := time.ParseDuration(cmd.StartDelay)
		if err == nil && delay > 0 {
			e.logger.Infow("Delaying start", "command_id", cmd.CommandID, "delay", delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
		}
	}

	// Create timeout context
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, duration)
	defer timeoutCancel()

	// Generate unique output filename
	outputFilename := fmt.Sprintf("%s_%s.tmp", cmd.CommandID, uuid.New().String()[:8])
	outputPath := filepath.Join(e.config.Download.OutputDir, outputFilename)

	// Build wget command
	// wget --limit-rate=<bandwidth>M -O <output> -c --tries=0 --timeout=10 <url>
	limitRate := fmt.Sprintf("%dM", cmd.Bandwidth)

	wgetCmd := exec.CommandContext(timeoutCtx, "wget",
		"--limit-rate", limitRate,
		"-O", outputPath,
		"-c", // Continue partial downloads
		"--progress=dot:mega",
		"--tries", "0", // Infinite retries
		"--timeout", "30", // Connection timeout
		"--no-check-certificate", // Skip SSL verification for some GCS buckets
		cmd.URL,
	)

	// Capture stderr for progress monitoring
	stderr, err := wgetCmd.StderrPipe()
	if err != nil {
		e.logger.Errorw("Failed to create stderr pipe", "command_id", cmd.CommandID, "error", err)
		return
	}

	job.Cmd = wgetCmd

	// Register job with metrics collector
	e.metrics.RegisterJob(cmd.CommandID, job)

	// Start command
	if err := wgetCmd.Start(); err != nil {
		e.logger.Errorw("Failed to start wget", "command_id", cmd.CommandID, "error", err)
		return
	}

	// Monitor progress in separate goroutine
	monitorDone := make(chan struct{})
	go e.monitorProgress(stderr, job, monitorDone)

	// Wait for completion or timeout
	waitErr := make(chan error, 1)
	go func() {
		waitErr <- wgetCmd.Wait()
	}()

	select {
	case <-timeoutCtx.Done():
		// Timeout reached, gracefully terminate
		e.logger.Infow("Download duration reached, stopping",
			"command_id", cmd.CommandID,
			"duration", duration,
		)
		if wgetCmd.Process != nil {
			wgetCmd.Process.Signal(syscall.SIGTERM)
			// Give it 5 seconds to terminate gracefully
			time.Sleep(5 * time.Second)
			if wgetCmd.ProcessState == nil || !wgetCmd.ProcessState.Exited() {
				wgetCmd.Process.Kill()
			}
		}
	case err := <-waitErr:
		if err != nil && timeoutCtx.Err() == nil {
			e.logger.Warnw("Download command exited with error",
				"command_id", cmd.CommandID,
				"error", err,
			)
		}
	}

	// Wait for monitor to finish
	close(monitorDone)

	// Cleanup downloaded file if configured
	if e.config.Download.Cleanup {
		if err := os.Remove(outputPath); err != nil && !os.IsNotExist(err) {
			e.logger.Warnw("Failed to cleanup file",
				"command_id", cmd.CommandID,
				"path", outputPath,
				"error", err,
			)
		}
	}

	e.logger.Infow("Download command completed",
		"command_id", cmd.CommandID,
		"bytes_downloaded", job.BytesDownloaded.Load(),
		"duration", time.Since(job.StartTime),
	)
}

// monitorProgress parses wget output to extract bandwidth metrics
func (e *Executor) monitorProgress(reader io.Reader, job *Job, done chan struct{}) {
	scanner := bufio.NewScanner(reader)

	// Parse wget progress output
	// Example: "2024-01-19 10:30:15 (850 MB/s) - ..."
	// Also: "     0K .......... .......... ..........  0%  850M 1s"
	speedRegex := regexp.MustCompile(`\(([0-9.]+)\s*([KMG]?)B/s\)`)
	progressRegex := regexp.MustCompile(`([0-9.]+)\s*([KMG]?)B/s`)

	for scanner.Scan() {
		select {
		case <-done:
			return
		default:
		}

		line := scanner.Text()

		// Try to extract speed from either format
		var speed float64
		var unit string

		matches := speedRegex.FindStringSubmatch(line)
		if len(matches) >= 3 {
			speed, _ = strconv.ParseFloat(matches[1], 64)
			unit = matches[2]
		} else {
			matches = progressRegex.FindStringSubmatch(line)
			if len(matches) >= 3 {
				speed, _ = strconv.ParseFloat(matches[1], 64)
				unit = matches[2]
			}
		}

		if speed > 0 {
			// Convert to Mbps
			mbps := convertToMbps(speed, unit)
			job.CurrentSpeedMbps.Store(mbps)
		}
	}
}

// convertToMbps converts speed to Mbps
func convertToMbps(speed float64, unit string) float64 {
	switch unit {
	case "K":
		return speed * 8 / 1000 // KB/s to Mbps
	case "M":
		return speed * 8 // MB/s to Mbps
	case "G":
		return speed * 8 * 1000 // GB/s to Mbps
	default:
		return speed * 8 / 1000000 // B/s to Mbps
	}
}
