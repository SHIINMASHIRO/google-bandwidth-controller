package agent

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mashiro/google-bandwidth-controller/pkg/logger"
)

// NetStats holds network interface statistics
type NetStats struct {
	RxBytes   uint64
	TxBytes   uint64
	Timestamp time.Time
}

// NetworkMonitor monitors network interface bandwidth
type NetworkMonitor struct {
	logger          *logger.Logger
	interfaceName   string
	lastStats       NetStats
	currentRxMbps   atomic.Value // float64
	currentTxMbps   atomic.Value // float64
	totalRxBytes    atomic.Uint64
	totalTxBytes    atomic.Uint64
	baselineRxBytes uint64
	baselineTxBytes uint64
	mu              sync.Mutex
	running         atomic.Bool
	stopCh          chan struct{}
}

// NewNetworkMonitor creates a new network monitor
func NewNetworkMonitor(log *logger.Logger) *NetworkMonitor {
	nm := &NetworkMonitor{
		logger: log,
		stopCh: make(chan struct{}),
	}
	nm.currentRxMbps.Store(0.0)
	nm.currentTxMbps.Store(0.0)
	return nm
}

// Start begins monitoring network interfaces
func (nm *NetworkMonitor) Start(sampleInterval time.Duration) error {
	if nm.running.Load() {
		return nil
	}

	// Auto-detect the primary interface
	iface, err := nm.detectInterface()
	if err != nil {
		return fmt.Errorf("failed to detect network interface: %w", err)
	}
	nm.interfaceName = iface
	nm.logger.Infow("Network monitor started", "interface", iface)

	// Get initial stats
	stats, err := nm.readInterfaceStats(iface)
	if err != nil {
		return fmt.Errorf("failed to read initial stats: %w", err)
	}
	nm.lastStats = stats
	nm.baselineRxBytes = stats.RxBytes
	nm.baselineTxBytes = stats.TxBytes

	nm.running.Store(true)
	nm.stopCh = make(chan struct{})

	go nm.monitorLoop(sampleInterval)
	return nil
}

// Stop stops the network monitor
func (nm *NetworkMonitor) Stop() {
	if !nm.running.Load() {
		return
	}
	nm.running.Store(false)
	close(nm.stopCh)
}

// GetCurrentBandwidth returns current download bandwidth in Mbps
func (nm *NetworkMonitor) GetCurrentBandwidth() float64 {
	return nm.currentRxMbps.Load().(float64)
}

// GetTotalBytesDownloaded returns total bytes downloaded since monitoring started
func (nm *NetworkMonitor) GetTotalBytesDownloaded() uint64 {
	return nm.totalRxBytes.Load()
}

// ResetBaseline resets the baseline for byte counting
func (nm *NetworkMonitor) ResetBaseline() {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.baselineRxBytes = nm.lastStats.RxBytes
	nm.baselineTxBytes = nm.lastStats.TxBytes
	nm.totalRxBytes.Store(0)
	nm.totalTxBytes.Store(0)
}

// monitorLoop continuously monitors network stats
func (nm *NetworkMonitor) monitorLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-nm.stopCh:
			return
		case <-ticker.C:
			nm.collectSample()
		}
	}
}

// collectSample reads current stats and calculates bandwidth
func (nm *NetworkMonitor) collectSample() {
	stats, err := nm.readInterfaceStats(nm.interfaceName)
	if err != nil {
		nm.logger.Warnw("Failed to read network stats", "error", err)
		return
	}

	nm.mu.Lock()
	lastStats := nm.lastStats
	nm.lastStats = stats
	nm.mu.Unlock()

	// Calculate time difference
	timeDiff := stats.Timestamp.Sub(lastStats.Timestamp).Seconds()
	if timeDiff <= 0 {
		return
	}

	// Calculate bandwidth in Mbps (bytes * 8 / 1000000 / seconds)
	rxBytesDiff := stats.RxBytes - lastStats.RxBytes
	txBytesDiff := stats.TxBytes - lastStats.TxBytes

	rxMbps := float64(rxBytesDiff) * 8 / 1000000 / timeDiff
	txMbps := float64(txBytesDiff) * 8 / 1000000 / timeDiff

	nm.currentRxMbps.Store(rxMbps)
	nm.currentTxMbps.Store(txMbps)

	// Update total bytes
	nm.totalRxBytes.Store(stats.RxBytes - nm.baselineRxBytes)
	nm.totalTxBytes.Store(stats.TxBytes - nm.baselineTxBytes)
}

// detectInterface finds the primary network interface
func (nm *NetworkMonitor) detectInterface() (string, error) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Skip header lines
	scanner.Scan()
	scanner.Scan()

	var bestIface string
	var maxBytes uint64

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		iface := strings.TrimSuffix(fields[0], ":")

		// Skip loopback and virtual interfaces
		if iface == "lo" || strings.HasPrefix(iface, "docker") ||
		   strings.HasPrefix(iface, "veth") || strings.HasPrefix(iface, "br-") ||
		   strings.HasPrefix(iface, "virbr") {
			continue
		}

		// Parse RX bytes
		rxBytes, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		// Select interface with most traffic (likely the active one)
		if rxBytes > maxBytes {
			maxBytes = rxBytes
			bestIface = iface
		}
	}

	if bestIface == "" {
		return "", fmt.Errorf("no suitable network interface found")
	}

	return bestIface, nil
}

// readInterfaceStats reads stats for a specific interface
func (nm *NetworkMonitor) readInterfaceStats(iface string) (NetStats, error) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return NetStats{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		name := strings.TrimSuffix(fields[0], ":")
		if name != iface {
			continue
		}

		// /proc/net/dev format:
		// iface: rx_bytes rx_packets rx_errs rx_drop ... tx_bytes tx_packets ...
		rxBytes, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return NetStats{}, fmt.Errorf("failed to parse rx_bytes: %w", err)
		}

		txBytes, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			return NetStats{}, fmt.Errorf("failed to parse tx_bytes: %w", err)
		}

		return NetStats{
			RxBytes:   rxBytes,
			TxBytes:   txBytes,
			Timestamp: time.Now(),
		}, nil
	}

	return NetStats{}, fmt.Errorf("interface %s not found", iface)
}
