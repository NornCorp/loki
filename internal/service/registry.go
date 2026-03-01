package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jumppad-labs/polymorph/internal/config"
	"github.com/jumppad-labs/polymorph/internal/meta"
	"github.com/jumppad-labs/polymorph/internal/serf"
)

// Service represents a service that can be started and stopped
type Service interface {
	// Start starts the service
	Start(ctx context.Context) error
	// Stop gracefully stops the service
	Stop(ctx context.Context) error
	// Name returns the service name
	Name() string
	// Type returns the service type
	Type() string
	// Address returns the service listen address
	Address() string
	// Upstreams returns the list of upstream service dependencies
	Upstreams() []string
}

// RequestLogRegistry is an interface for managing request logs
type RequestLogRegistry interface {
	Register(serviceName string, logger interface{})
	meta.RequestLogProvider
}

// Registry manages multiple services and optionally registers with Heimdall
type Registry struct {
	services           []Service
	serfClient         *serf.Client
	requestLogRegistry RequestLogRegistry
	mu                 sync.Mutex
}

// NewRegistry creates a new service registry with a request log registry
func NewRegistry(logRegistry RequestLogRegistry) *Registry {
	return &Registry{
		services:           make([]Service, 0),
		requestLogRegistry: logRegistry,
	}
}

// Register adds a service to the registry
func (r *Registry) Register(svc Service) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services = append(r.services, svc)
}

// Start starts all registered services and optionally joins Heimdall mesh
func (r *Registry) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Start all services first
	for _, svc := range r.services {
		if err := svc.Start(ctx); err != nil {
			return fmt.Errorf("failed to start service %q: %w", svc.Name(), err)
		}
	}

	// Join Heimdall mesh if serf client is configured
	if r.serfClient != nil {
		if err := r.serfClient.Start(ctx); err != nil {
			// Stop all services on failure
			for i := len(r.services) - 1; i >= 0; i-- {
				r.services[i].Stop(ctx)
			}
			return fmt.Errorf("failed to join heimdall mesh: %w", err)
		}
	}

	return nil
}

// Stop stops all registered services in reverse order and leaves Heimdall mesh
func (r *Registry) Stop(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error

	// Leave Heimdall mesh first
	if r.serfClient != nil {
		if err := r.serfClient.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("failed to leave heimdall mesh: %w", err))
		}
	}

	// Stop services in reverse order
	for i := len(r.services) - 1; i >= 0; i-- {
		svc := r.services[i]
		if err := svc.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop service %q: %w", svc.Name(), err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping services: %v", errs)
	}

	return nil
}

// Services returns all registered services
func (r *Registry) Services() []Service {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Service{}, r.services...)
}

// ServiceInfo represents service metadata for Serf tags
// Only includes basic discovery info - resource metadata is fetched via RPC
type ServiceInfo struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Address   string   `json:"address"`
	Upstreams []string `json:"upstreams,omitempty"`
}

// ConfigureHeimdall configures the registry to join the Heimdall mesh
func (r *Registry) ConfigureHeimdall(heimdallCfg *config.HeimdallConfig, allConfigs []config.Service) error {
	if heimdallCfg == nil {
		// No Heimdall configuration, run in standalone mode
		return nil
	}

	if heimdallCfg.Address == "" {
		return fmt.Errorf("heimdall address is required")
	}

	// Build service info for all services (basic discovery only)
	serviceInfos := make([]ServiceInfo, 0, len(r.services))
	for _, svc := range r.services {
		serviceInfos = append(serviceInfos, ServiceInfo{
			Name:      svc.Name(),
			Type:      svc.Type(),
			Address:   svc.Address(),
			Upstreams: svc.Upstreams(),
		})
	}

	// Encode all services as JSON
	servicesJSON, err := json.Marshal(serviceInfos)
	if err != nil {
		return fmt.Errorf("failed to encode services: %w", err)
	}

	// Build tags with all services encoded
	tags := map[string]string{
		"services": string(servicesJSON),
	}

	// Create serf client
	client, err := serf.NewClient(serf.ClientConfig{
		NodeName: heimdallCfg.NodeName, // Use custom node name if specified, otherwise defaults to hostname
		JoinAddr: heimdallCfg.Address,
		Tags:     tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create serf client: %w", err)
	}

	r.serfClient = client

	// Register HTTP service loggers and configure meta service
	// This allows them to expose resource metadata via RPC with forwarding
	for _, svc := range r.services {
		// Register request logger if this is an HTTP service
		if httpSvc, ok := svc.(interface {
			GetRequestLogger() interface{}
		}); ok && r.requestLogRegistry != nil {
			logger := httpSvc.GetRequestLogger()
			r.requestLogRegistry.Register(svc.Name(), logger)
		}

		// Configure meta service (RPC endpoint)
		if httpSvc, ok := svc.(interface {
			ConfigureMetaService([]config.Service, *serf.Client, meta.RequestLogProvider)
		}); ok {
			httpSvc.ConfigureMetaService(allConfigs, client, r.requestLogRegistry)
		}
	}

	return nil
}

// Factory is a function that creates a service from a typed config
type Factory func(config.Service, *slog.Logger) (Service, error)

// factories maps service types to their factory functions
var factories = make(map[string]Factory)

// RegisterFactory registers a factory function for a service type
func RegisterFactory(serviceType string, factory Factory) {
	factories[serviceType] = factory
}

// CreateService creates a service from a config using the registered factory
func CreateService(cfg config.Service, logger *slog.Logger) (Service, error) {
	factory, ok := factories[cfg.ServiceType()]
	if !ok {
		return nil, fmt.Errorf("unknown service type: %q", cfg.ServiceType())
	}

	return factory(cfg, logger)
}

// CreateServices creates all services from a config
func CreateServices(cfg *config.Config, loggers map[string]*slog.Logger) ([]Service, error) {
	services := make([]Service, 0, len(cfg.Services))

	for _, svcCfg := range cfg.Services {
		logger := loggers[svcCfg.ServiceName()]
		if logger == nil {
			logger = slog.Default()
		}
		svc, err := CreateService(svcCfg, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create service %q: %w", svcCfg.ServiceName(), err)
		}
		services = append(services, svc)
	}

	return services, nil
}
