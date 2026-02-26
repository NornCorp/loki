package service

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseMemorySize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"1KB", 1024, false},
		{"1MB", 1 << 20, false},
		{"1GB", 1 << 30, false},
		{"512KB", 512 * 1024, false},
		{"256MB", 256 * (1 << 20), false},
		{"100B", 100, false},
		{"1024", 1024, false},
		{"", 0, false},
		{"1kb", 1024, false},
		{"bad", 0, true},
		{"MB", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseMemorySize(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestLoadGenerator_CPULoad(t *testing.T) {
	gen := NewLoadGenerator(LoadConfig{
		CPUCores:   1,
		CPUPercent: 1.0, // 100% of one core
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Run in goroutine since Generate blocks
	done := make(chan struct{})
	go func() {
		gen.Generate(ctx)
		close(done)
	}()

	// Should complete after context timeout
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Generate did not return after context cancellation")
	}
}

func TestLoadGenerator_MemoryLoad(t *testing.T) {
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	gen := NewLoadGenerator(LoadConfig{
		Memory: 1 << 20, // 1MB
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		gen.Generate(ctx)
		close(done)
	}()

	// Give it a moment to allocate
	time.Sleep(50 * time.Millisecond)

	var memDuring runtime.MemStats
	runtime.ReadMemStats(&memDuring)

	// Memory should have increased by at least ~1MB
	require.Greater(t, memDuring.TotalAlloc, memBefore.TotalAlloc)

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Generate did not return after context cancellation")
	}
}

func TestLoadGenerator_Empty(t *testing.T) {
	gen := NewLoadGenerator(LoadConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		gen.Generate(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Generate did not return after context cancellation")
	}
}
