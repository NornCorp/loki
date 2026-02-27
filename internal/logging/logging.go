package logging

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/norncorp/loki/internal/config"
)

// Config holds resolved logging configuration (not HCL-tagged).
type Config struct {
	Level  slog.Level // default: slog.LevelInfo
	Format string     // "text" or "json"
	Output string     // "stdout", "stderr", or file path
}

// DefaultConfig returns Config with all defaults applied.
func DefaultConfig() Config {
	return Config{
		Level:  slog.LevelInfo,
		Format: "text",
		Output: "stderr",
	}
}

// ResolveConfig merges an HCL LoggingConfig (pointer fields) onto defaults.
func ResolveConfig(defaults Config, hcl *config.LoggingConfig) Config {
	if hcl == nil {
		return defaults
	}
	cfg := defaults
	if hcl.Level != nil {
		cfg.Level = parseLevel(*hcl.Level)
	}
	if hcl.Format != nil {
		cfg.Format = *hcl.Format
	}
	if hcl.Output != nil {
		cfg.Output = *hcl.Output
	}
	return cfg
}

// Init creates the global default slog.Logger from config.
// Returns a cleanup function that closes any open file handles.
func Init(cfg Config) (cleanup func(), err error) {
	w, closeFunc, err := openOutput(cfg.Output)
	if err != nil {
		return nil, fmt.Errorf("logging: failed to open output %q: %w", cfg.Output, err)
	}

	handler := newHandler(w, cfg)
	slog.SetDefault(slog.New(handler))

	return closeFunc, nil
}

// ForService creates a per-service logger that inherits defaults
// from the global config but overrides with service-specific settings.
// The returned logger always includes a "service" attribute.
// Returns a cleanup function for file handles.
func ForService(name string, global Config, override *Config) (*slog.Logger, func(), error) {
	cfg := global
	if override != nil {
		cfg = *override
	}

	w, closeFunc, err := openOutput(cfg.Output)
	if err != nil {
		return nil, nil, fmt.Errorf("logging: service %q: failed to open output %q: %w", name, cfg.Output, err)
	}

	handler := newHandler(w, cfg)
	logger := slog.New(handler).With("service", name)

	return logger, closeFunc, nil
}

// newHandler creates a slog.Handler for the given writer and config.
func newHandler(w *os.File, cfg Config) slog.Handler {
	opts := &slog.HandlerOptions{Level: cfg.Level}
	if cfg.Format == "json" {
		return slog.NewJSONHandler(w, opts)
	}
	return slog.NewTextHandler(w, opts)
}

// openOutput opens the output destination. Returns the file, a cleanup
// function, and any error. For stdout/stderr the cleanup is a no-op.
func openOutput(output string) (*os.File, func(), error) {
	noop := func() {}
	switch strings.ToLower(output) {
	case "stdout":
		return os.Stdout, noop, nil
	case "stderr", "":
		return os.Stderr, noop, nil
	default:
		f, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, nil, err
		}
		return f, func() { f.Close() }, nil
	}
}

// parseLevel converts a string log level to slog.Level.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
