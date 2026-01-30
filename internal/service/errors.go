package service

import (
	"math/rand"
	"net/http"
	"time"
)

// ErrorConfig defines an error injection rule
type ErrorConfig struct {
	Name    string            // Error name/identifier
	Rate    float64           // Probability (0.0-1.0)
	Status  int               // HTTP status code
	Headers map[string]string // Response headers
	Body    string            // Response body (evaluated expression result)
}

// ErrorInjector manages error injection
type ErrorInjector struct {
	errors []*ErrorConfig
	rng    *rand.Rand
}

// NewErrorInjector creates a new error injector
func NewErrorInjector(errors []*ErrorConfig) *ErrorInjector {
	return &ErrorInjector{
		errors: errors,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// ShouldInject determines if an error should be injected
// Returns the error config if an error should be injected, nil otherwise
func (e *ErrorInjector) ShouldInject() *ErrorConfig {
	if len(e.errors) == 0 {
		return nil
	}

	// Check each error in order
	for _, errCfg := range e.errors {
		if e.rng.Float64() < errCfg.Rate {
			return errCfg
		}
	}

	return nil
}

// WriteError writes an error response to the HTTP response writer
func (e *ErrorInjector) WriteError(w http.ResponseWriter, errCfg *ErrorConfig) {
	// Set headers
	for k, v := range errCfg.Headers {
		w.Header().Set(k, v)
	}

	// Set content type if not already set
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}

	// Write status code
	w.WriteHeader(errCfg.Status)

	// Write body
	if errCfg.Body != "" {
		w.Write([]byte(errCfg.Body))
	}
}

// ErrorInjectionStats tracks error injection statistics
type ErrorInjectionStats struct {
	TotalRequests int
	InjectedCount map[string]int // Error name -> count
}

// NewErrorInjectionStats creates a new stats tracker
func NewErrorInjectionStats() *ErrorInjectionStats {
	return &ErrorInjectionStats{
		InjectedCount: make(map[string]int),
	}
}

// RecordRequest records a request
func (s *ErrorInjectionStats) RecordRequest() {
	s.TotalRequests++
}

// RecordInjection records an error injection
func (s *ErrorInjectionStats) RecordInjection(errName string) {
	s.InjectedCount[errName]++
}

// Rate returns the actual injection rate for a specific error
func (s *ErrorInjectionStats) Rate(errName string) float64 {
	if s.TotalRequests == 0 {
		return 0
	}
	return float64(s.InjectedCount[errName]) / float64(s.TotalRequests)
}
