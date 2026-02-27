package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/norncorp/loki/internal/config"
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
	log.Printf("Loading configuration from %s", serverConfigPath)
	cfg, err := config.ParseFile(serverConfigPath)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Validate config
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Initialize tracing (uses OTEL_EXPORTER_OTLP_ENDPOINT env var if set)
	tp, err := tracing.Init(context.Background(), tracing.Config{
		ServiceName: "loki",
	})
	if err != nil {
		log.Printf("Warning: failed to initialize tracing: %v", err)
	}

	// Create services
	services, err := service.CreateServices(cfg)
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

	log.Println("All services started successfully")

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	<-sigCh
	log.Println("Shutdown signal received, stopping services...")

	// Stop services
	if err := registry.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop services: %w", err)
	}

	// Shutdown tracing
	if tp != nil {
		if err := tp.Shutdown(ctx); err != nil {
			log.Printf("Warning: failed to shutdown tracing: %v", err)
		}
	}

	log.Println("All services stopped successfully")

	return nil
}
