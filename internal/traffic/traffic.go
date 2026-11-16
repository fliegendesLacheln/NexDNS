package traffic

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

type TrafficSnapshot struct {
	DownloadBps        float64   `json:"downloadBps"`
	UploadBps          float64   `json:"uploadBps"`
	DownloadFormatted  string    `json:"downloadFormatted"`
	UploadFormatted    string    `json:"uploadFormatted"`
	TotalDownloadBytes uint64    `json:"totalDownloadBytes"`
	TotalUploadBytes   uint64    `json:"totalUploadBytes"`
	TotalDownFormatted string    `json:"totalDownFormatted"`
	TotalUpFormatted   string    `json:"totalUpFormatted"`
	Timestamp          time.Time `json:"timestamp"`
}

type Monitor struct {
	mu            sync.RWMutex
	targetIfIndex uint32
	lastInOctets  uint64
	lastOutOctets uint64
	lastCheckTime time.Time
	currentStats  TrafficSnapshot
	history       []TrafficSnapshot
	stopChan      chan struct{}
	running       bool
}

var globalMonitor = &Monitor{
	history: make([]TrafficSnapshot, 0, 60),
}

func GetMonitor() *Monitor {
	return globalMonitor
}

func (m *Monitor) SetInterfaceIndex(ifIndex uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.targetIfIndex = ifIndex
	m.lastInOctets = 0
	m.lastOutOctets = 0
	m.lastCheckTime = time.Time{}
}

func queryIfOctets(ifIndex uint32) (uint64, uint64, error) {
	var row windows.MibIfRow2
	row.InterfaceIndex = ifIndex
	if err := windows.GetIfEntry2Ex(windows.MibIfEntryNormal, &row); err != nil {
		return 0, 0, fmt.Errorf("GetIfEntry2Ex failed: %w", err)
	}
	return row.InOctets, row.OutOctets, nil
}

func formatBytes(b uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)
	switch {
	case b >= TB:
		return fmt.Sprintf("%.2f TB", float64(b)/float64(TB))
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func formatSpeed(bps float64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case bps >= GB:
		return fmt.Sprintf("%.2f GB/s", bps/float64(GB))
	case bps >= MB:
		return fmt.Sprintf("%.2f MB/s", bps/float64(MB))
	case bps >= KB:
		return fmt.Sprintf("%.2f KB/s", bps/float64(KB))
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

func (m *Monitor) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.stopChan = make(chan struct{})
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-m.stopChan:
				return
			case t := <-ticker.C:
				m.tick(t)
			}
		}
	}()
}

func (m *Monitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}
	m.running = false
	close(m.stopChan)
}

func (m *Monitor) tick(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.targetIfIndex == 0 {
		return
	}

	inOctets, outOctets, err := queryIfOctets(m.targetIfIndex)
	if err != nil {
		return
	}

	if m.lastCheckTime.IsZero() || m.lastInOctets == 0 {
		m.lastInOctets = inOctets
		m.lastOutOctets = outOctets
		m.lastCheckTime = now
		m.currentStats = TrafficSnapshot{
			DownloadBps:        0,
			UploadBps:          0,
			DownloadFormatted:  "0 KB/s",
			UploadFormatted:    "0 KB/s",
			TotalDownloadBytes: inOctets,
			TotalUploadBytes:   outOctets,
			TotalDownFormatted: formatBytes(inOctets),
			TotalUpFormatted:   formatBytes(outOctets),
			Timestamp:          now,
		}
		return
	}

	elapsed := now.Sub(m.lastCheckTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	var downBps, upBps float64
	if inOctets >= m.lastInOctets {
		downBps = float64(inOctets-m.lastInOctets) / elapsed
	}
	if outOctets >= m.lastOutOctets {
		upBps = float64(outOctets-m.lastOutOctets) / elapsed
	}

	m.lastInOctets = inOctets
	m.lastOutOctets = outOctets
	m.lastCheckTime = now

	snapshot := TrafficSnapshot{
		DownloadBps:        downBps,
		UploadBps:          upBps,
		DownloadFormatted:  formatSpeed(downBps),
		UploadFormatted:    formatSpeed(upBps),
		TotalDownloadBytes: inOctets,
		TotalUploadBytes:   outOctets,
		TotalDownFormatted: formatBytes(inOctets),
		TotalUpFormatted:   formatBytes(outOctets),
		Timestamp:          now,
	}

	m.currentStats = snapshot
	m.history = append(m.history, snapshot)
	if len(m.history) > 60 {
		m.history = m.history[1:]
	}
}

func (m *Monitor) GetCurrentStats() TrafficSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentStats
}

func (m *Monitor) GetHistory() []TrafficSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]TrafficSnapshot, len(m.history))
	copy(res, m.history)
	return res
}
