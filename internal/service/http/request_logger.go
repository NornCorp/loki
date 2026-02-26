package http

import (
	"net/http"
	"sync"
	"time"
)

// RequestLog represents a single HTTP request log entry
type RequestLog struct {
	Sequence  uint64    `json:"sequence"`
	Timestamp time.Time `json:"timestamp"`
	Method    string    `json:"method"`
	Path      string    `json:"path"`
	Status    int       `json:"status"`
	Duration  int64     `json:"duration_ms"` // milliseconds
	Level     string    `json:"level"`       // "info" or "debug"
}

// RequestLogger captures and stores HTTP request logs in a ring buffer
type RequestLogger struct {
	mu       sync.RWMutex
	logs     []RequestLog
	capacity int
	sequence uint64
	writePos int
	full     bool
}

// NewRequestLogger creates a new request logger with the given capacity
func NewRequestLogger(capacity int) *RequestLogger {
	return &RequestLogger{
		logs:     make([]RequestLog, capacity),
		capacity: capacity,
		sequence: 0,
		writePos: 0,
		full:     false,
	}
}

// Log records a new request
func (rl *RequestLogger) Log(method, path string, status int, duration time.Duration, level string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.sequence++

	rl.logs[rl.writePos] = RequestLog{
		Sequence:  rl.sequence,
		Timestamp: time.Now(),
		Method:    method,
		Path:      path,
		Status:    status,
		Duration:  duration.Milliseconds(),
		Level:     level,
	}

	rl.writePos++
	if rl.writePos >= rl.capacity {
		rl.writePos = 0
		rl.full = true
	}
}

// GetLogs returns logs after the given sequence number (0 = all logs)
// Returns up to maxCount logs
func (rl *RequestLogger) GetLogs(afterSequence uint64, maxCount int) []RequestLog {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	// Determine actual number of logs in buffer
	count := rl.writePos
	if rl.full {
		count = rl.capacity
	}

	if count == 0 {
		return []RequestLog{}
	}

	// Collect all logs that match the criteria
	result := make([]RequestLog, 0, maxCount)

	// Determine where to start reading
	startPos := 0
	if rl.full {
		// If buffer is full, oldest log is at writePos
		startPos = rl.writePos
	}

	// Read logs in circular order
	for i := 0; i < count; i++ {
		pos := (startPos + i) % rl.capacity
		log := rl.logs[pos]

		if log.Sequence > afterSequence {
			result = append(result, log)
			if len(result) >= maxCount {
				break
			}
		}
	}

	return result
}

// GetLatestSequence returns the most recent sequence number
func (rl *RequestLogger) GetLatestSequence() uint64 {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return rl.sequence
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	status int
	written bool
}

func (rw *responseWriter) WriteHeader(status int) {
	if !rw.written {
		rw.status = status
		rw.written = true
		rw.ResponseWriter.WriteHeader(status)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.status = http.StatusOK
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// LoggingMiddleware wraps an http.Handler to log requests
func (rl *RequestLogger) LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status
		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		// Call the next handler
		next.ServeHTTP(wrapped, r)

		// Log the request
		duration := time.Since(start)
		rl.Log(r.Method, r.URL.Path, wrapped.status, duration, "info")
	})
}
