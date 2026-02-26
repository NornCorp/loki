package connect

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/norncorp/loki/internal/config"
	"github.com/norncorp/loki/internal/resource"
	"github.com/norncorp/loki/internal/service"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// ConnectService implements a Connect-RPC service
type ConnectService struct {
	name             string
	config           *config.ServiceConfig
	resourceStore    *resource.Store
	resourceHandlers []*ResourceHandler
	customHandlers   []*CustomMethodHandler
	server           *http.Server
	listener         net.Listener
	mux              *http.ServeMux
}

// NewConnectService creates a new Connect-RPC service
func NewConnectService(cfg *config.ServiceConfig) (*ConnectService, error) {
	if cfg.Package == "" {
		return nil, fmt.Errorf("package is required for connect service")
	}

	// Create resource store if we have resources
	var resourceStore *resource.Store
	var resourceHandlers []*ResourceHandler

	if len(cfg.Resources) > 0 {
		resourceStore = resource.NewStore()

		// Create resource handlers
		for _, res := range cfg.Resources {
			rh, err := NewResourceHandler(res, resourceStore, cfg.Package)
			if err != nil {
				return nil, fmt.Errorf("failed to create resource handler for %q: %w", res.Name, err)
			}

			// Initialize the resource (create table and generate data)
			if err := rh.Initialize(); err != nil {
				return nil, fmt.Errorf("failed to initialize resource %q: %w", res.Name, err)
			}

			resourceHandlers = append(resourceHandlers, rh)
		}
	}

	svc := &ConnectService{
		name:             cfg.Name,
		config:           cfg,
		resourceStore:    resourceStore,
		resourceHandlers: resourceHandlers,
		mux:              http.NewServeMux(),
	}

	// Determine service name for custom methods
	// If there are resources, use the first one; otherwise derive from service name
	serviceName := cfg.Name + "Service"
	if len(cfg.Resources) > 0 {
		serviceName = capitalizeFirst(cfg.Resources[0].Name) + "Service"
	}

	// Create custom method handlers from handle blocks without routes
	var customHandlers []*CustomMethodHandler
	for _, handler := range cfg.Handlers {
		if handler.Route != "" {
			continue // Skip route-based handlers (not Connect-RPC methods)
		}
		mh, err := NewCustomMethodHandler(handler, cfg.Package, serviceName, cfg.ServiceVars)
		if err != nil {
			return nil, fmt.Errorf("failed to create custom method handler for %q: %w", handler.Name, err)
		}
		customHandlers = append(customHandlers, mh)
	}
	svc.customHandlers = customHandlers

	// Register all resource handlers as Connect-RPC endpoints
	for _, rh := range resourceHandlers {
		_, handler := rh.RegisterHandlers()
		// Wrap handler with h2c for HTTP/2 without TLS
		svc.mux.Handle("/", h2c.NewHandler(handler, &http2.Server{}))
	}

	// Register custom method handlers
	for _, mh := range customHandlers {
		path, handler := mh.RegisterHandler()
		svc.mux.HandleFunc(path, handler)
		log.Printf("Registered custom method at path: %s", path)
	}

	return svc, nil
}

// Name returns the service name
func (s *ConnectService) Name() string {
	return s.name
}

// Type returns the service type
func (s *ConnectService) Type() string {
	return "connect"
}

// Address returns the service listen address
func (s *ConnectService) Address() string {
	return s.config.Listen
}

// Upstreams returns the list of upstream service dependencies
func (s *ConnectService) Upstreams() []string {
	return s.config.InferredUpstreams
}

// Start starts the Connect-RPC server
func (s *ConnectService) Start(ctx context.Context) error {
	// Create listener
	listener, err := net.Listen("tcp", s.config.Listen)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	s.listener = listener

	// Create HTTP server with h2c handler
	s.server = &http.Server{
		Handler: s.mux,
	}

	// Start server in background
	go func() {
		log.Printf("Connect-RPC service %q listening on %s", s.name, s.config.Listen)
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("Connect-RPC server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully stops the Connect-RPC server
func (s *ConnectService) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	log.Printf("Stopping Connect-RPC service %q", s.name)

	// Use a timeout context for shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	return nil
}

// init registers the Connect service factory
func init() {
	service.RegisterFactory("connect", func(cfg *config.ServiceConfig) (service.Service, error) {
		return NewConnectService(cfg)
	})
}
