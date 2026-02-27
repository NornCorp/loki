package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/norncorp/loki/internal/config"
	"github.com/norncorp/loki/internal/logging"
	"github.com/norncorp/loki/internal/metrics"
	"github.com/norncorp/loki/internal/service"
	_ "github.com/norncorp/loki/internal/service/connect"  // Register Connect-RPC service
	"github.com/norncorp/loki/internal/service/http"       // Need for log registry
	_ "github.com/norncorp/loki/internal/service/postgres" // Register PostgreSQL service
	_ "github.com/norncorp/loki/internal/service/proxy"    // Register Proxy service
	_ "github.com/norncorp/loki/internal/service/tcp"      // Register TCP service
	"github.com/norncorp/loki/internal/tracing"
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the Loki server",
	Long:  `Start the Loki server with the specified configuration file.`,
	RunE:  runServer,
}

var serverConfigPath string

func init() {
	serverCmd.Flags().StringVarP(&serverConfigPath, "config", "c", "", "path to configuration file (required)")
	serverCmd.MarkFlagRequired("config")
	rootCmd.AddCommand(serverCmd)
}

func runServer(cmd *cobra.Command, args []string) error {
	// Check if config file exists
	if _, err := os.Stat(serverConfigPath); os.IsNotExist(err) {
		return fmt.Errorf("configuration file not found: %s", serverConfigPath)
	}

	// Parse config
	cfg, err := config.ParseFile(serverConfigPath)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Validate config
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Initialize logging
	logCfg := logging.DefaultConfig()
	if cfg.Logging != nil {
		logCfg = logging.ResolveConfig(logCfg, cfg.Logging)
	}
	logCleanup, err := logging.Init(logCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logging: %v\n", err)
		os.Exit(1)
	}
	defer logCleanup()

	slog.Info("loading configuration", "path", serverConfigPath)

	// Build per-service loggers
	serviceLoggers := make(map[string]*slog.Logger)
	var serviceLogCleanups []func()
	for _, svc := range cfg.Services {
		var override *logging.Config
		if svc.Logging != nil {
			resolved := logging.ResolveConfig(logCfg, svc.Logging)
			override = &resolved
		}
		logger, cleanup, err := logging.ForService(svc.Name, logCfg, override)
		if err != nil {
			slog.Error("failed to create service logger", "service", svc.Name, "error", err)
			os.Exit(1)
		}
		serviceLoggers[svc.Name] = logger
		serviceLogCleanups = append(serviceLogCleanups, cleanup)
	}
	defer func() {
		for _, cleanup := range serviceLogCleanups {
			cleanup()
		}
	}()

	// Initialize metrics
	metricsCfg := metrics.Config{Enabled: true, Path: "/metrics"}
	if cfg.Metrics != nil {
		if cfg.Metrics.Enabled != nil {
			metricsCfg.Enabled = *cfg.Metrics.Enabled
		}
		if cfg.Metrics.Path != nil {
			metricsCfg.Path = *cfg.Metrics.Path
		}
	}
	metrics.Init(metricsCfg)

	// Initialize tracing
	tracingCfg := tracing.Config{
		Enabled:     true,
		ServiceName: "loki",
		Sampler:     "always_on",
	}
	if cfg.Tracing != nil {
		if cfg.Tracing.Enabled != nil {
			tracingCfg.Enabled = *cfg.Tracing.Enabled
		}
		if cfg.Tracing.Endpoint != nil {
			tracingCfg.Endpoint = *cfg.Tracing.Endpoint
		}
		if cfg.Tracing.Sampler != nil {
			tracingCfg.Sampler = *cfg.Tracing.Sampler
		}
		if cfg.Tracing.Ratio != nil {
			tracingCfg.Ratio = *cfg.Tracing.Ratio
		}
	}
	tp, err := tracing.Init(context.Background(), tracingCfg)
	if err != nil {
		slog.Warn("failed to initialize tracing", "error", err)
	}

	// Create services
	services, err := service.CreateServices(cfg, serviceLoggers)
	if err != nil {
		return fmt.Errorf("failed to create services: %w", err)
	}

	// Create request log registry
	logRegistry := http.NewServiceLogRegistry()

	// Create registry and register services
	registry := service.NewRegistry(logRegistry)
	for _, svc := range services {
		registry.Register(svc)
	}

	// Configure Heimdall integration if specified
	if err := registry.ConfigureHeimdall(cfg.Heimdall, cfg.Services); err != nil {
		return fmt.Errorf("failed to configure heimdall: %w", err)
	}

	// Start services
	ctx := context.Background()
	if err := registry.Start(ctx); err != nil {
		return fmt.Errorf("failed to start services: %w", err)
	}

	slog.Info("all services started")

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	<-sigCh
	slog.Info("shutdown signal received, stopping services")

	// Stop services
	if err := registry.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop services: %w", err)
	}

	// Shutdown tracing
	if tp != nil {
		if err := tp.Shutdown(ctx); err != nil {
			slog.Warn("failed to shutdown tracing", "error", err)
		}
	}

	slog.Info("all services stopped")

	return nil
}
