package logging

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norncorp/loki/internal/config"
	"github.com/stretchr/testify/require"
)

func TestInit_TextFormat(t *testing.T) {
	// Write to a temp file so we can inspect output
	dir := t.TempDir()
	path := filepath.Join(dir, "text.log")

	cleanup, err := Init(Config{
		Level:  slog.LevelInfo,
		Format: "text",
		Output: path,
	})
	require.NoError(t, err)
	defer cleanup()

	slog.Info("hello", "key", "value")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "hello")
	require.Contains(t, string(data), "key=value")
}

func TestInit_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "json.log")

	cleanup, err := Init(Config{
		Level:  slog.LevelInfo,
		Format: "json",
		Output: path,
	})
	require.NoError(t, err)
	defer cleanup()

	slog.Info("structured", "num", 42)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var entry map[string]any
	err = json.Unmarshal(data, &entry)
	require.NoError(t, err)
	require.Equal(t, "structured", entry["msg"])
	require.EqualValues(t, 42, entry["num"])
}

func TestInit_FileOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	// File should not exist yet
	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err))

	cleanup, err := Init(Config{
		Level:  slog.LevelInfo,
		Format: "text",
		Output: path,
	})
	require.NoError(t, err)

	slog.Info("first")
	cleanup()

	// Re-open in append mode -- second init should append, not truncate
	cleanup2, err := Init(Config{
		Level:  slog.LevelInfo,
		Format: "text",
		Output: path,
	})
	require.NoError(t, err)

	slog.Info("second")
	cleanup2()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "first")
	require.Contains(t, string(data), "second")
}

func TestInit_Levels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "levels.log")

	cleanup, err := Init(Config{
		Level:  slog.LevelInfo,
		Format: "text",
		Output: path,
	})
	require.NoError(t, err)
	defer cleanup()

	slog.Debug("should-not-appear")
	slog.Info("should-appear")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(data), "should-not-appear")
	require.Contains(t, string(data), "should-appear")
}

func TestForService_InheritsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "svc.log")

	global := Config{
		Level:  slog.LevelInfo,
		Format: "text",
		Output: path,
	}

	logger, cleanup, err := ForService("api", global, nil)
	require.NoError(t, err)
	defer cleanup()

	logger.Info("hello")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "hello")
	require.Contains(t, string(data), "service=api")
}

func TestForService_OverridesLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "svc.log")

	global := Config{
		Level:  slog.LevelInfo,
		Format: "text",
		Output: path,
	}
	override := Config{
		Level:  slog.LevelWarn,
		Format: "text",
		Output: path,
	}

	logger, cleanup, err := ForService("noisy", global, &override)
	require.NoError(t, err)
	defer cleanup()

	logger.Info("suppressed")
	logger.Warn("visible")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(data), "suppressed")
	require.Contains(t, string(data), "visible")
}

func TestForService_OverridesOutput(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.log")
	svcPath := filepath.Join(dir, "svc.log")

	global := Config{
		Level:  slog.LevelInfo,
		Format: "text",
		Output: globalPath,
	}
	override := Config{
		Level:  slog.LevelInfo,
		Format: "text",
		Output: svcPath,
	}

	logger, cleanup, err := ForService("worker", global, &override)
	require.NoError(t, err)
	defer cleanup()

	logger.Info("service-only")

	// Should NOT be in global log
	globalData, _ := os.ReadFile(globalPath)
	require.NotContains(t, string(globalData), "service-only")

	// Should be in service log
	svcData, err := os.ReadFile(svcPath)
	require.NoError(t, err)
	require.Contains(t, string(svcData), "service-only")
	require.Contains(t, string(svcData), "service=worker")
}

func TestForService_IncludesServiceAttr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "attr.log")

	global := Config{
		Level:  slog.LevelInfo,
		Format: "json",
		Output: path,
	}

	logger, cleanup, err := ForService("my-svc", global, nil)
	require.NoError(t, err)
	defer cleanup()

	logger.Info("test")

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var entry map[string]any
	err = json.Unmarshal(data, &entry)
	require.NoError(t, err)
	require.Equal(t, "my-svc", entry["service"])
}

func TestResolveConfig_NilOverride(t *testing.T) {
	defaults := DefaultConfig()
	result := ResolveConfig(defaults, nil)
	require.Equal(t, defaults, result)
}

func TestResolveConfig_PartialOverride(t *testing.T) {
	defaults := DefaultConfig()

	level := "warn"
	hclCfg := &config.LoggingConfig{
		Level: &level,
		// Format and Output left nil
	}

	result := ResolveConfig(defaults, hclCfg)
	require.Equal(t, slog.LevelWarn, result.Level)
	require.Equal(t, "text", result.Format)   // unchanged
	require.Equal(t, "stderr", result.Output)  // unchanged
}

func TestResolveConfig_FullOverride(t *testing.T) {
	defaults := DefaultConfig()

	level := "debug"
	format := "json"
	output := "/tmp/app.log"
	hclCfg := &config.LoggingConfig{
		Level:  &level,
		Format: &format,
		Output: &output,
	}

	result := ResolveConfig(defaults, hclCfg)
	require.Equal(t, slog.LevelDebug, result.Level)
	require.Equal(t, "json", result.Format)
	require.Equal(t, "/tmp/app.log", result.Output)
}

func TestInit_StdoutStderr(t *testing.T) {
	// Verify stdout and stderr don't error
	for _, output := range []string{"stdout", "stderr"} {
		t.Run(output, func(t *testing.T) {
			cleanup, err := Init(Config{
				Level:  slog.LevelError, // high level to suppress actual output
				Format: "text",
				Output: output,
			})
			require.NoError(t, err)
			cleanup()
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"INFO", slog.LevelInfo},    // case insensitive
		{"unknown", slog.LevelInfo}, // defaults to info
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			require.Equal(t, tt.want, parseLevel(tt.input))
		})
	}
}

// Verify openOutput returns the correct file descriptors for well-known outputs.
func TestOpenOutput_WellKnown(t *testing.T) {
	for _, name := range []string{"stdout", "stderr", "STDOUT", "STDERR"} {
		t.Run(name, func(t *testing.T) {
			f, cleanup, err := openOutput(name)
			require.NoError(t, err)
			cleanup()
			if strings.EqualFold(name, "stdout") {
				require.Equal(t, os.Stdout, f)
			} else {
				require.Equal(t, os.Stderr, f)
			}
		})
	}
}
