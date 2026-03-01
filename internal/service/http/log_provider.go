package http

import (
	"sync"

	"github.com/jumppad-labs/polymorph/internal/meta"
)

// ServiceLogRegistry manages request loggers for all services
type ServiceLogRegistry struct {
	mu      sync.RWMutex
	loggers map[string]*RequestLogger
}

// NewServiceLogRegistry creates a new registry
func NewServiceLogRegistry() *ServiceLogRegistry {
	return &ServiceLogRegistry{
		loggers: make(map[string]*RequestLogger),
	}
}

// Register adds a service's request logger to the registry
func (r *ServiceLogRegistry) Register(serviceName string, logger interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rl, ok := logger.(*RequestLogger); ok {
		r.loggers[serviceName] = rl
	}
}

// GetLogs implements meta.RequestLogProvider
func (r *ServiceLogRegistry) GetLogs(serviceName string, afterSequence uint64, limit int32) ([]meta.RequestLog, uint64) {
	r.mu.RLock()
	logger, ok := r.loggers[serviceName]
	r.mu.RUnlock()

	if !ok {
		return []meta.RequestLog{}, 0
	}

	// Get logs from the service's logger
	logs := logger.GetLogs(afterSequence, int(limit))

	// Convert to meta.RequestLog format
	result := make([]meta.RequestLog, 0, len(logs))
	for _, log := range logs {
		result = append(result, meta.RequestLog{
			Sequence:   log.Sequence,
			Timestamp:  log.Timestamp.UnixMilli(),
			Method:     log.Method,
			Path:       log.Path,
			Status:     int32(log.Status),
			DurationMs: log.Duration,
			Level:      log.Level,
		})
	}

	latestSeq := logger.GetLatestSequence()

	return result, latestSeq
}
