package server

import (
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Metrics represents server metrics
type Metrics struct {
	mu              sync.RWMutex
	StartTime       time.Time               `json:"start_time"`
	RequestCount    int64                   `json:"request_count"`
	ErrorCount      int64                   `json:"error_count"`
	ProvidersServed map[string]int64        `json:"providers_served"`
	ResponseTimes   []time.Duration         `json:"-"`
	AverageResponse time.Duration           `json:"average_response_time"`
	LastRequestTime time.Time               `json:"last_request_time"`
	DiskUsage       int64                   `json:"disk_usage_bytes"`
	SystemInfo      SystemInfo              `json:"system_info"`
	EndpointStats   map[string]EndpointStat `json:"endpoint_stats"`
}

// SystemInfo represents system information
type SystemInfo struct {
	GoVersion    string `json:"go_version"`
	Platform     string `json:"platform"`
	NumCPU       int    `json:"num_cpu"`
	NumGoroutine int    `json:"num_goroutine"`
	MemAlloc     uint64 `json:"mem_alloc"`
	MemTotal     uint64 `json:"mem_total"`
	MemSys       uint64 `json:"mem_sys"`
	NumGC        uint32 `json:"num_gc"`
}

// EndpointStat represents statistics for a specific endpoint
type EndpointStat struct {
	RequestCount    int64         `json:"request_count"`
	ErrorCount      int64         `json:"error_count"`
	AverageResponse time.Duration `json:"average_response_time"`
	LastAccess      time.Time     `json:"last_access"`
}

// NewMetrics creates a new metrics instance
func NewMetrics() *Metrics {
	return &Metrics{
		StartTime:       time.Now(),
		ProvidersServed: make(map[string]int64),
		ResponseTimes:   make([]time.Duration, 0, 100), // Keep last 100 response times
		EndpointStats:   make(map[string]EndpointStat),
		SystemInfo:      getSystemInfo(),
	}
}

// RecordRequest records a request with response time
func (m *Metrics) RecordRequest(endpoint string, duration time.Duration, isError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.RequestCount++
	m.LastRequestTime = time.Now()

	// Update response times (keep only last 100)
	if len(m.ResponseTimes) >= 100 {
		m.ResponseTimes = m.ResponseTimes[1:]
	}
	m.ResponseTimes = append(m.ResponseTimes, duration)

	// Calculate average response time
	if len(m.ResponseTimes) > 0 {
		var total time.Duration
		for _, rt := range m.ResponseTimes {
			total += rt
		}
		m.AverageResponse = total / time.Duration(len(m.ResponseTimes))
	}

	// Update endpoint statistics
	stat := m.EndpointStats[endpoint]
	stat.RequestCount++
	stat.LastAccess = time.Now()

	if isError {
		m.ErrorCount++
		stat.ErrorCount++
	}

	// Calculate endpoint average response time
	if stat.RequestCount > 0 {
		// Simple moving average approximation
		stat.AverageResponse = (stat.AverageResponse*time.Duration(stat.RequestCount-1) + duration) / time.Duration(stat.RequestCount)
	}

	m.EndpointStats[endpoint] = stat
}

// RecordProviderServed records that a provider was served
func (m *Metrics) RecordProviderServed(provider string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ProvidersServed[provider]++
}

// UpdateCounts is now a no-op (TotalProviders/Versions/Platforms removed)
func (m *Metrics) UpdateCounts(providers, versions, platforms int) {
	// No-op
}

// UpdateDiskUsage updates disk usage information
func (m *Metrics) UpdateDiskUsage(dataPath string) {
	usage := calculateDiskUsage(dataPath)

	m.mu.Lock()
	m.DiskUsage = usage
	m.SystemInfo = getSystemInfo()
	m.mu.Unlock()
}

// GetMetrics returns a copy of current metrics
func (m *Metrics) GetMetrics() *Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := &Metrics{
		StartTime:       m.StartTime,
		RequestCount:    m.RequestCount,
		ErrorCount:      m.ErrorCount,
		AverageResponse: m.AverageResponse,
		LastRequestTime: m.LastRequestTime,

		DiskUsage:       m.DiskUsage,
		SystemInfo:      m.SystemInfo,
		ProvidersServed: make(map[string]int64, len(m.ProvidersServed)),
		EndpointStats:   make(map[string]EndpointStat, len(m.EndpointStats)),
	}

	// Use maps.Copy (Go 1.21+) for copying maps
	maps.Copy(metrics.ProvidersServed, m.ProvidersServed)
	maps.Copy(metrics.EndpointStats, m.EndpointStats)

	return metrics
}

// getSystemInfo returns current system information
func getSystemInfo() SystemInfo {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return SystemInfo{
		GoVersion:    runtime.Version(),
		Platform:     runtime.GOOS + "/" + runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		MemAlloc:     memStats.Alloc,
		MemTotal:     memStats.TotalAlloc,
		MemSys:       memStats.Sys,
		NumGC:        memStats.NumGC,
	}
}

// calculateDiskUsage calculates disk usage of a directory
func calculateDiskUsage(path string) int64 {
	var size int64

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors and continue
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	if err != nil {
		return 0
	}

	return size
}

// handleMetrics handles the /metrics endpoint in Prometheus exporter format
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	// Update disk usage before returning metrics
	go s.metrics.UpdateDiskUsage(s.config.DataPath)

	// Update provider counts (no-op, metrics removed)
	providers, _ := s.scanProviders()
	totalVersions, totalPlatforms := s.countVersionsAndPlatforms()
	s.metrics.UpdateCounts(len(providers), totalVersions, totalPlatforms)

	metrics := s.metrics.GetMetrics()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	// Prometheus metrics exposition
	// HELP and TYPE lines for each metric
	sb := &strings.Builder{}

	// Uptime
	uptime := time.Since(metrics.StartTime).Seconds()
	sb.WriteString("# HELP tfmirror_uptime_seconds Uptime of the server in seconds\n")
	sb.WriteString("# TYPE tfmirror_uptime_seconds gauge\n")
	sb.WriteString("tfmirror_uptime_seconds ")
	sb.WriteString(formatFloat(uptime))
	sb.WriteString("\n")

	// Request count
	sb.WriteString("# HELP tfmirror_requests_total Total number of HTTP requests\n")
	sb.WriteString("# TYPE tfmirror_requests_total counter\n")
	sb.WriteString("tfmirror_requests_total ")
	sb.WriteString(formatInt(metrics.RequestCount))
	sb.WriteString("\n")

	// Error count
	sb.WriteString("# HELP tfmirror_errors_total Total number of HTTP errors\n")
	sb.WriteString("# TYPE tfmirror_errors_total counter\n")
	sb.WriteString("tfmirror_errors_total ")
	sb.WriteString(formatInt(metrics.ErrorCount))
	sb.WriteString("\n")

	// Average response time
	sb.WriteString("# HELP tfmirror_average_response_seconds Average response time (last 100 requests)\n")
	sb.WriteString("# TYPE tfmirror_average_response_seconds gauge\n")
	sb.WriteString("tfmirror_average_response_seconds ")
	sb.WriteString(formatFloat(metrics.AverageResponse.Seconds()))
	sb.WriteString("\n")

	// Last request time (as unix timestamp)
	sb.WriteString("# HELP tfmirror_last_request_unixtime Last request time as unix timestamp\n")
	sb.WriteString("# TYPE tfmirror_last_request_unixtime gauge\n")
	sb.WriteString("tfmirror_last_request_unixtime ")
	sb.WriteString(formatFloat(float64(metrics.LastRequestTime.Unix())))
	sb.WriteString("\n")

	// Providers served (per provider)
	sb.WriteString("# HELP tfmirror_providers_served_total Number of times each provider was served\n")
	sb.WriteString("# TYPE tfmirror_providers_served_total counter\n")
	for provider, count := range metrics.ProvidersServed {
		sb.WriteString("tfmirror_providers_served_total{provider=\"")
		sb.WriteString(escapeLabel(provider))
		sb.WriteString("\"} ")
		sb.WriteString(formatInt(count))
		sb.WriteString("\n")
	}

	// Disk usage
	sb.WriteString("# HELP tfmirror_disk_usage_bytes Disk usage of mirror data path in bytes\n")
	sb.WriteString("# TYPE tfmirror_disk_usage_bytes gauge\n")
	sb.WriteString("tfmirror_disk_usage_bytes ")
	sb.WriteString(formatInt(metrics.DiskUsage))
	sb.WriteString("\n")

	// System info as labels (static gauge)
	sb.WriteString("# HELP tfmirror_system_info System info as labels\n")
	sb.WriteString("# TYPE tfmirror_system_info gauge\n")
	sb.WriteString("tfmirror_system_info{")
	sb.WriteString("go_version=\"")
	sb.WriteString(escapeLabel(metrics.SystemInfo.GoVersion))
	sb.WriteString("\",")
	sb.WriteString("platform=\"")
	sb.WriteString(escapeLabel(metrics.SystemInfo.Platform))
	sb.WriteString("\",")
	sb.WriteString("num_cpu=\"")
	sb.WriteString(formatInt(int64(metrics.SystemInfo.NumCPU)))
	sb.WriteString("\",")
	sb.WriteString("num_goroutine=\"")
	sb.WriteString(formatInt(int64(metrics.SystemInfo.NumGoroutine)))
	sb.WriteString("\",")
	sb.WriteString("mem_alloc=\"")
	sb.WriteString(formatInt(int64(metrics.SystemInfo.MemAlloc)))
	sb.WriteString("\",")
	sb.WriteString("mem_total=\"")
	sb.WriteString(formatInt(int64(metrics.SystemInfo.MemTotal)))
	sb.WriteString("\",")
	sb.WriteString("mem_sys=\"")
	sb.WriteString(formatInt(int64(metrics.SystemInfo.MemSys)))
	sb.WriteString("\",")
	sb.WriteString("num_gc=\"")
	sb.WriteString(formatInt(int64(metrics.SystemInfo.NumGC)))
	sb.WriteString("\"")
	sb.WriteString("} 1\n")

	// Endpoint stats
	sb.WriteString("# HELP tfmirror_endpoint_requests_total Total requests per endpoint\n")
	sb.WriteString("# TYPE tfmirror_endpoint_requests_total counter\n")
	sb.WriteString("# HELP tfmirror_endpoint_errors_total Total errors per endpoint\n")
	sb.WriteString("# TYPE tfmirror_endpoint_errors_total counter\n")
	sb.WriteString("# HELP tfmirror_endpoint_average_response_seconds Average response time per endpoint\n")
	sb.WriteString("# TYPE tfmirror_endpoint_average_response_seconds gauge\n")
	sb.WriteString("# HELP tfmirror_endpoint_last_access_unixtime Last access time per endpoint (unix timestamp)\n")
	sb.WriteString("# TYPE tfmirror_endpoint_last_access_unixtime gauge\n")
	for endpoint, stat := range metrics.EndpointStats {
		ep := escapeLabel(endpoint)
		sb.WriteString("tfmirror_endpoint_requests_total{endpoint=\"")
		sb.WriteString(ep)
		sb.WriteString("\"} ")
		sb.WriteString(formatInt(stat.RequestCount))
		sb.WriteString("\n")
		sb.WriteString("tfmirror_endpoint_errors_total{endpoint=\"")
		sb.WriteString(ep)
		sb.WriteString("\"} ")
		sb.WriteString(formatInt(stat.ErrorCount))
		sb.WriteString("\n")
		sb.WriteString("tfmirror_endpoint_average_response_seconds{endpoint=\"")
		sb.WriteString(ep)
		sb.WriteString("\"} ")
		sb.WriteString(formatFloat(stat.AverageResponse.Seconds()))
		sb.WriteString("\n")
		sb.WriteString("tfmirror_endpoint_last_access_unixtime{endpoint=\"")
		sb.WriteString(ep)
		sb.WriteString("\"} ")
		sb.WriteString(formatFloat(float64(stat.LastAccess.Unix())))
		sb.WriteString("\n")
	}

	w.Write([]byte(sb.String()))
}

// formatInt formats int64 as string
func formatInt(i int64) string {
	return strconv.FormatInt(i, 10)
}

// formatFloat formats float64 as string with 6 decimal places
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', 6, 64)
}

// escapeLabel escapes backslashes and double quotes for Prometheus label values
func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// countVersionsAndPlatforms counts total versions and platforms
func (s *Server) countVersionsAndPlatforms() (int, int) {
	totalVersions := 0
	totalPlatforms := 0

	err := filepath.Walk(s.config.DataPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			relPath, err := filepath.Rel(s.config.DataPath, path)
			if err != nil {
				return nil
			}

			parts := strings.Split(relPath, string(os.PathSeparator))
			if len(parts) == 3 { // namespace/name/version
				totalVersions++
			} else if len(parts) == 4 && len(parts[3]) > 0 { // namespace/name/version/platform
				totalPlatforms++
			}
		}

		return nil
	})

	if err != nil {
		return 0, 0
	}

	return totalVersions, totalPlatforms
}

// metricsMiddleware wraps handlers to collect metrics
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapped := &responseWriterWrapper{ResponseWriter: w}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		endpoint := r.URL.Path
		isError := wrapped.statusCode >= 400

		// Record metrics
		s.metrics.RecordRequest(endpoint, duration, isError)

		// Record provider served for download endpoints
		if r.URL.Path != "" && len(r.URL.Path) > 1 {
			// Check if this is a provider download
			pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(pathParts) >= 2 {
				provider := pathParts[0] + "/" + pathParts[1]
				s.metrics.RecordProviderServed(provider)
			}
		}
	})
}
