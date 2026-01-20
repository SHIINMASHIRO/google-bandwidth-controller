package agent

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mashiro/google-bandwidth-controller/internal/protocol"
	"github.com/mashiro/google-bandwidth-controller/pkg/logger"
)

const (
	// Number of concurrent download threads
	DefaultConcurrentDownloads = 4
)

// Executor handles download command execution
type Executor struct {
	config     *Config
	activeJobs sync.Map // map[string]*Job
	logger     *logger.Logger
	metrics    *MetricsCollector
}

// Job represents a running download job with multiple threads
type Job struct {
	CommandID        string
	URL              string
	StartTime        time.Time
	BytesDownloaded  atomic.Int64
	CurrentSpeedMbps atomic.Value // float64
	Cancel           context.CancelFunc
	wg               sync.WaitGroup
	threads          []*downloadThread
	mu               sync.Mutex
	DownloadType     protocol.DownloadType
}

// downloadThread represents a single download thread
type downloadThread struct {
	id     int
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// NewExecutor creates a new command executor
func NewExecutor(config *Config, metrics *MetricsCollector, log *logger.Logger) *Executor {
	return &Executor{
		config:  config,
		logger:  log,
		metrics: metrics,
	}
}

// ExecuteDownload starts a download command with multiple threads
func (e *Executor) ExecuteDownload(cmd *protocol.DownloadCommand) error {
	// Check if command already exists
	if _, exists := e.activeJobs.Load(cmd.CommandID); exists {
		return fmt.Errorf("command %s already running", cmd.CommandID)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Determine download type (default to wget for backward compatibility)
	downloadType := cmd.Type
	if downloadType == "" {
		downloadType = protocol.DownloadTypeWget
	}

	job := &Job{
		CommandID:    cmd.CommandID,
		URL:          cmd.URL,
		StartTime:    time.Now(),
		Cancel:       cancel,
		threads:      make([]*downloadThread, 0, DefaultConcurrentDownloads),
		DownloadType: downloadType,
	}
	job.CurrentSpeedMbps.Store(0.0)

	e.activeJobs.Store(cmd.CommandID, job)

	// Register job with metrics collector
	e.metrics.RegisterJob(cmd.CommandID, job)

	// Choose execution method based on download type
	switch downloadType {
	case protocol.DownloadTypeYtDlp:
		e.logger.Infow("Starting yt-dlp download",
			"command_id", cmd.CommandID,
			"url", cmd.URL,
			"bandwidth", cmd.Bandwidth,
		)
		go e.runYtDlpDownload(ctx, cmd, job)
	default:
		e.logger.Infow("Starting multi-threaded wget download",
			"command_id", cmd.CommandID,
			"url", cmd.URL,
			"bandwidth", cmd.Bandwidth,
			"threads", DefaultConcurrentDownloads,
		)
		go e.runMultiThreadDownload(ctx, cmd, job)
	}

	return nil
}

// Stop stops a specific command or all commands if commandID is empty
func (e *Executor) Stop(commandID string) error {
	if commandID == "" {
		// Stop all commands
		e.activeJobs.Range(func(key, value interface{}) bool {
			job := value.(*Job)
			e.stopJob(job)
			return true
		})
		e.logger.Info("Stopping all download commands")
		return nil
	}

	// Stop specific command
	if jobVal, exists := e.activeJobs.Load(commandID); exists {
		job := jobVal.(*Job)
		e.stopJob(job)
		e.logger.Infow("Stopping download command", "command_id", commandID)
		return nil
	}

	return fmt.Errorf("command %s not found", commandID)
}

// stopJob stops all threads in a job
func (e *Executor) stopJob(job *Job) {
	job.Cancel()

	job.mu.Lock()
	for _, thread := range job.threads {
		if thread.cmd != nil && thread.cmd.Process != nil {
			thread.cmd.Process.Signal(syscall.SIGTERM)
		}
	}
	job.mu.Unlock()
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
			Progress:        0,
		})
		return true
	})

	return metrics
}

// runMultiThreadDownload runs multiple download threads in parallel
func (e *Executor) runMultiThreadDownload(ctx context.Context, cmd *protocol.DownloadCommand, job *Job) {
	defer e.activeJobs.Delete(cmd.CommandID)
	defer e.metrics.DeregisterJob(cmd.CommandID)

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

	// Calculate bandwidth per thread
	bandwidthPerThread := cmd.Bandwidth / DefaultConcurrentDownloads
	if bandwidthPerThread < 1 {
		bandwidthPerThread = 1
	}

	e.logger.Infow("Starting download threads",
		"command_id", cmd.CommandID,
		"total_bandwidth", cmd.Bandwidth,
		"bandwidth_per_thread", bandwidthPerThread,
		"threads", DefaultConcurrentDownloads,
	)

	// Start download threads
	for i := 0; i < DefaultConcurrentDownloads; i++ {
		job.wg.Add(1)
		go e.downloadThread(ctx, cmd, job, i, bandwidthPerThread)
	}

	// Wait for all threads to complete (they will run until cancelled)
	job.wg.Wait()

	e.logger.Infow("All download threads stopped",
		"command_id", cmd.CommandID,
		"duration", time.Since(job.StartTime),
	)
}

// downloadThread runs a single download thread that loops continuously
func (e *Executor) downloadThread(ctx context.Context, cmd *protocol.DownloadCommand, job *Job, threadID int, bandwidthMbps int64) {
	defer job.wg.Done()

	limitRate := fmt.Sprintf("%dM", bandwidthMbps)

	for {
		select {
		case <-ctx.Done():
			e.logger.Infow("Download thread stopping",
				"command_id", cmd.CommandID,
				"thread_id", threadID,
			)
			return
		default:
		}

		// Create a new wget command for this iteration
		threadCtx, threadCancel := context.WithCancel(ctx)

		wgetCmd := exec.CommandContext(threadCtx, "wget",
			"--limit-rate", limitRate,
			"-O", "/dev/null",
			"--progress=dot:mega",
			"--tries", "3",
			"--timeout", "30",
			"--no-check-certificate",
			cmd.URL,
		)

		// Register thread
		thread := &downloadThread{
			id:     threadID,
			cmd:    wgetCmd,
			cancel: threadCancel,
		}

		job.mu.Lock()
		job.threads = append(job.threads, thread)
		job.mu.Unlock()

		// Start wget
		if err := wgetCmd.Start(); err != nil {
			e.logger.Warnw("Failed to start wget thread",
				"command_id", cmd.CommandID,
				"thread_id", threadID,
				"error", err,
			)
			threadCancel()
			// Brief pause before retry
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}

		// Wait for wget to complete
		err := wgetCmd.Wait()
		threadCancel()

		// Check if we should stop
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Log completion and restart
		if err != nil {
			e.logger.Debugw("Download iteration completed with error, restarting",
				"command_id", cmd.CommandID,
				"thread_id", threadID,
				"error", err,
			)
		} else {
			e.logger.Debugw("Download iteration completed, restarting",
				"command_id", cmd.CommandID,
				"thread_id", threadID,
			)
		}

		// Small delay before restarting to avoid hammering
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// runYtDlpDownload runs a yt-dlp download task for YouTube videos using multiple threads
func (e *Executor) runYtDlpDownload(ctx context.Context, cmd *protocol.DownloadCommand, job *Job) {
	defer e.activeJobs.Delete(cmd.CommandID)
	defer e.metrics.DeregisterJob(cmd.CommandID)

	// Handle start delay if specified
	if cmd.StartDelay != "" {
		delay, err := time.ParseDuration(cmd.StartDelay)
		if err == nil && delay > 0 {
			e.logger.Infow("Delaying yt-dlp start", "command_id", cmd.CommandID, "delay", delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
		}
	}

	// Calculate bandwidth per thread (use same threading as wget)
	bandwidthPerThread := cmd.Bandwidth / DefaultConcurrentDownloads
	if bandwidthPerThread < 1 {
		bandwidthPerThread = 1
	}

	e.logger.Infow("Starting yt-dlp download threads",
		"command_id", cmd.CommandID,
		"url", cmd.URL,
		"total_bandwidth", cmd.Bandwidth,
		"bandwidth_per_thread", bandwidthPerThread,
		"threads", DefaultConcurrentDownloads,
	)

	// Start multiple yt-dlp threads
	for i := 0; i < DefaultConcurrentDownloads; i++ {
		job.wg.Add(1)
		go e.ytdlpThread(ctx, cmd, job, i, bandwidthPerThread)
	}

	// Wait for all threads to complete
	job.wg.Wait()

	e.logger.Infow("All yt-dlp threads stopped",
		"command_id", cmd.CommandID,
		"duration", time.Since(job.StartTime),
	)
}

// ytdlpThread runs a single yt-dlp download thread that loops continuously
func (e *Executor) ytdlpThread(ctx context.Context, cmd *protocol.DownloadCommand, job *Job, threadID int, bandwidthMbps int64) {
	defer job.wg.Done()

	// Calculate limit rate for yt-dlp
	// yt-dlp uses bytes/s with K/M suffix, we convert Mbps to MB/s (approximate)
	// Mbps / 8 = MB/s, but yt-dlp M suffix means MiB, so we use a factor
	limitRate := fmt.Sprintf("%dM", bandwidthMbps/8)
	if bandwidthMbps < 8 {
		limitRate = fmt.Sprintf("%dK", bandwidthMbps*125) // Mbps * 125 = KB/s
	}

	for {
		select {
		case <-ctx.Done():
			e.logger.Infow("yt-dlp thread stopping",
				"command_id", cmd.CommandID,
				"thread_id", threadID,
			)
			return
		default:
		}

		// Create a new yt-dlp command for this iteration
		threadCtx, threadCancel := context.WithCancel(ctx)

		ytdlpCmd := exec.CommandContext(threadCtx, "yt-dlp",
			// Output to /dev/null - don't save the file
			"-o", "/dev/null",
			// Limit download speed
			"--limit-rate", limitRate,
			// Don't check certificate (consistent with wget)
			"--no-check-certificate",
			// Don't download entire playlist, only the specified video
			"--no-playlist",
			// Select best video only (no audio, no merge needed)
			"-f", "bestvideo",
			// Quiet mode, reduce output
			"--quiet",
			"--no-warnings",
			// Don't save any metadata
			"--no-write-info-json",
			"--no-write-thumbnail",
			"--no-write-description",
			"--no-write-comments",
			// Retry settings
			"--retries", "3",
			"--fragment-retries", "3",
			// Socket timeout
			"--socket-timeout", "30",
			// The URL to download
			cmd.URL,
		)

		// Register thread
		thread := &downloadThread{
			id:     threadID,
			cmd:    ytdlpCmd,
			cancel: threadCancel,
		}

		job.mu.Lock()
		job.threads = append(job.threads, thread)
		job.mu.Unlock()

		// Start yt-dlp
		if err := ytdlpCmd.Start(); err != nil {
			e.logger.Warnw("Failed to start yt-dlp thread",
				"command_id", cmd.CommandID,
				"thread_id", threadID,
				"error", err,
			)
			threadCancel()
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		// Wait for yt-dlp to complete
		err := ytdlpCmd.Wait()
		threadCancel()

		// Check if we should stop
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Log completion and restart
		if err != nil {
			e.logger.Debugw("yt-dlp thread completed with error, restarting",
				"command_id", cmd.CommandID,
				"thread_id", threadID,
				"error", err,
			)
		} else {
			e.logger.Debugw("yt-dlp thread completed successfully, restarting",
				"command_id", cmd.CommandID,
				"thread_id", threadID,
			)
		}

		// Small delay before restarting
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}
