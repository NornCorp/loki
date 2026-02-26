package service

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LoadConfig defines load generation parameters
type LoadConfig struct {
	CPUCores   int     // Number of goroutines doing busy work
	CPUPercent float64 // Duty cycle per core (0.0-1.0)
	Memory     int64   // Bytes to allocate and hold
}

// LoadGenerator generates CPU and memory load during request handling.
type LoadGenerator struct {
	config LoadConfig
}

// NewLoadGenerator creates a new load generator.
func NewLoadGenerator(config LoadConfig) *LoadGenerator {
	return &LoadGenerator{config: config}
}

// Generate runs CPU and memory load for the lifetime of the context.
// It blocks until ctx is cancelled.
func (l *LoadGenerator) Generate(ctx context.Context) {
	var wg sync.WaitGroup

	// Memory load: allocate and hold bytes
	if l.config.Memory > 0 {
		// Allocate the block and touch every page (4KB) so the OS
		// actually backs it with physical memory.
		mem := make([]byte, l.config.Memory)
		for i := 0; i < len(mem); i += 4096 {
			mem[i] = 1
		}

		// Keep a reference alive until context is done
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ctx.Done()
			// Use mem to prevent the compiler from optimizing it away
			runtime.KeepAlive(mem)
		}()
	}

	// CPU load: spin goroutines with duty cycle
	if l.config.CPUCores > 0 && l.config.CPUPercent > 0 {
		const cycleTime = 10 * time.Millisecond
		busyTime := time.Duration(float64(cycleTime) * l.config.CPUPercent)
		sleepTime := cycleTime - busyTime

		for range l.config.CPUCores {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					// Busy phase
					deadline := time.Now().Add(busyTime)
					for time.Now().Before(deadline) {
						// Tight loop - burns CPU
					}

					// Sleep phase
					select {
					case <-ctx.Done():
						return
					case <-time.After(sleepTime):
					}
				}
			}()
		}
	}

	wg.Wait()
}

// ParseMemorySize parses a human-readable memory size string (e.g., "256MB", "1GB", "512KB").
func ParseMemorySize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	s = strings.ToUpper(s)

	multipliers := []struct {
		suffix string
		mult   int64
	}{
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		{"B", 1},
	}

	for _, m := range multipliers {
		if strings.HasSuffix(s, m.suffix) {
			numStr := strings.TrimSuffix(s, m.suffix)
			num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
			if err != nil {
				return 0, fmt.Errorf("invalid memory size %q: %w", s, err)
			}
			return int64(num * float64(m.mult)), nil
		}
	}

	// Plain number = bytes
	num, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory size %q: %w", s, err)
	}
	return num, nil
}
