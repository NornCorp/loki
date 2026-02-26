package http

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/norncorp/loki/internal/config"
	"github.com/norncorp/loki/internal/meta"
	"github.com/norncorp/loki/internal/resource"
	"github.com/norncorp/loki/internal/service"
	"github.com/norncorp/loki/internal/serf"
	"github.com/norncorp/loki/internal/step"
	"github.com/norncorp/loki/pkg/api/meta/v1/metaapiconnect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// HTTPService implements an HTTP service
type HTTPService struct {
	name             string
	config           *config.ServiceConfig
	router           *Router
	resourceHandlers []*ResourceHandler
	resourceStore    *resource.Store
	server           *http.Server
	listener         net.Listener
	latencyInjector  *service.LatencyInjector
	errorInjector    *service.ErrorInjector
	mux              *http.ServeMux
	allConfigs       []*config.ServiceConfig // All services for meta API
	requestLogger    *RequestLogger          // Request log ring buffer
}

// NewHTTPService creates a new HTTP service
func NewHTTPService(cfg *config.ServiceConfig) (*HTTPService, error) {
	router := NewRouter()

	// Add all handlers to the router
	for _, handler := range cfg.Handlers {
		if err := router.AddHandler(handler); err != nil {
			return nil, fmt.Errorf("failed to add handler: %w", err)
		}
	}

	// Create resource store if we have resources
	var resourceStore *resource.Store
	var resourceHandlers []*ResourceHandler

	if len(cfg.Resources) > 0 {
		resourceStore = resource.NewStore()

		// Create resource handlers
		for _, res := range cfg.Resources {
			rh, err := NewResourceHandler(res, resourceStore)
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

	// Initialize timing injector if configured
	var latencyInjector *service.LatencyInjector
	if cfg.Timing != nil {
		p50, err := service.ParseDuration(cfg.Timing.P50)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timing.p50: %w", err)
		}
		p90, err := service.ParseDuration(cfg.Timing.P90)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timing.p90: %w", err)
		}
		p99, err := service.ParseDuration(cfg.Timing.P99)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timing.p99: %w", err)
		}

		latencyInjector = service.NewLatencyInjector(service.TimingConfig{
			P50:      p50,
			P90:      p90,
			P99:      p99,
			Variance: cfg.Timing.Variance,
		})
	}

	// Initialize error injector if configured
	var errorInjector *service.ErrorInjector
	if len(cfg.Errors) > 0 {
		errorConfigs := make([]*service.ErrorConfig, 0, len(cfg.Errors))
		for _, errCfg := range cfg.Errors {
			// Evaluate error response body if present
			var bodyStr string
			if errCfg.Response != nil && errCfg.Response.BodyExpr != nil {
				// Create a minimal eval context with just functions (no request)
				evalCtx := &hcl.EvalContext{
					Functions: config.Functions(),
				}
				value, diags := errCfg.Response.BodyExpr.Value(evalCtx)
				if diags.HasErrors() {
					return nil, fmt.Errorf("failed to evaluate error %q body: %s", errCfg.Name, diags.Error())
				}
				bodyStr = value.AsString()
			}

			headers := make(map[string]string)
			if errCfg.Response != nil && errCfg.Response.HeadersExpr != nil {
				// Create a minimal eval context with just functions (no request)
				headersEvalCtx := &hcl.EvalContext{
					Functions: config.Functions(),
				}
				// Evaluate headers expression
				headersVal, diags := errCfg.Response.HeadersExpr.Value(headersEvalCtx)
				if diags.HasErrors() {
					return nil, fmt.Errorf("failed to evaluate error %q headers: %s", errCfg.Name, diags.Error())
				}
				// Convert to map[string]string (check for null first)
				if !headersVal.IsNull() {
					for key, val := range headersVal.AsValueMap() {
						headers[key] = val.AsString()
					}
				}
			}

			errorConfigs = append(errorConfigs, &service.ErrorConfig{
				Name:    errCfg.Name,
				Rate:    errCfg.Rate,
				Status:  errCfg.Status,
				Headers: headers,
				Body:    bodyStr,
			})
		}
		errorInjector = service.NewErrorInjector(errorConfigs)
	}

	svc := &HTTPService{
		name:             cfg.Name,
		config:           cfg,
		router:           router,
		resourceStore:    resourceStore,
		resourceHandlers: resourceHandlers,
		latencyInjector:  latencyInjector,
		errorInjector:    errorInjector,
		requestLogger:    NewRequestLogger(1000), // Store last 1000 requests
	}

	return svc, nil
}

// Name returns the service name
func (s *HTTPService) Name() string {
	return s.name
}

// Type returns the service type
func (s *HTTPService) Type() string {
	return "http"
}

// Address returns the service listen address
func (s *HTTPService) Address() string {
	return s.config.Listen
}

// Upstreams returns the list of upstream service dependencies
func (s *HTTPService) Upstreams() []string {
	return s.config.InferredUpstreams
}

// ConfigureMetaService sets up the meta service RPC handler
func (s *HTTPService) ConfigureMetaService(allConfigs []*config.ServiceConfig, serfClient *serf.Client, logProvider meta.RequestLogProvider) {
	metaSvc := meta.NewMetaService(allConfigs, serfClient, logProvider)
	path, handler := metaapiconnect.NewLokiMetaServiceHandler(metaSvc)

	// Create mux if not exists
	if s.mux == nil {
		s.mux = http.NewServeMux()
	}

	// Register the Connect-RPC handler at its path
	// Connect handlers need h2c wrapper for HTTP/2 without TLS
	s.mux.Handle(path, h2c.NewHandler(handler, &http2.Server{}))

	s.allConfigs = allConfigs
	log.Printf("Meta service registered at path: %s", path)
}

// GetRequestLogger returns the service's request logger for registration
func (s *HTTPService) GetRequestLogger() interface{} {
	return s.requestLogger
}

// Start starts the HTTP server
func (s *HTTPService) Start(ctx context.Context) error {
	// Create listener
	listener, err := net.Listen("tcp", s.config.Listen)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	s.listener = listener

	// Create HTTP server
	s.server = &http.Server{
		Handler: s,
	}

	// Start server in background
	go func() {
		log.Printf("HTTP service %q listening on %s", s.name, s.config.Listen)
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully stops the HTTP server
func (s *HTTPService) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	log.Printf("Stopping HTTP service %q", s.name)

	// Use a timeout context for shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	return nil
}

// ServeHTTP handles incoming HTTP requests
func (s *HTTPService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Wrap response writer to capture status code
	wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

	// Add CORS headers for browser access
	wrapped.Header().Set("Access-Control-Allow-Origin", "*")
	wrapped.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	wrapped.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// Handle preflight requests
	if r.Method == "OPTIONS" {
		wrapped.WriteHeader(http.StatusOK)
		// Log the OPTIONS request
		s.requestLogger.Log(r.Method, r.URL.Path, wrapped.status, time.Since(start), getLogLevel(r.URL.Path, wrapped.status))
		return
	}

	// Try mux first (for Connect-RPC and other registered handlers)
	if s.mux != nil {
		_, pattern := s.mux.Handler(r)
		if pattern != "" {
			s.mux.ServeHTTP(wrapped, r)
			// Log the request with appropriate level based on status
			s.requestLogger.Log(r.Method, r.URL.Path, wrapped.status, time.Since(start), getLogLevel(r.URL.Path, wrapped.status))
			return
		}
	}

	// First, check if any resource handler matches
	for _, rh := range s.resourceHandlers {
		if rh.Match(r.Method, r.URL.Path) {
			rh.Handle(wrapped, r)
			// Log the request
			s.requestLogger.Log(r.Method, r.URL.Path, wrapped.status, time.Since(start), getLogLevel(r.URL.Path, wrapped.status))
			return
		}
	}

	// Try to match a regular route
	route, ok := s.router.Match(r)
	if !ok {
		// No matching route - return 404
		wrapped.WriteHeader(http.StatusNotFound)
		wrapped.Header().Set("Content-Type", "application/json")
		wrapped.Write([]byte(`{"error":"not found"}`))
		// Log the 404
		s.requestLogger.Log(r.Method, r.URL.Path, wrapped.status, time.Since(start), getLogLevel(r.URL.Path, wrapped.status))
		return
	}

	// Handle the request with the matched route
	s.handleRequest(wrapped, r, route)

	// Log the request
	s.requestLogger.Log(r.Method, r.URL.Path, wrapped.status, time.Since(start), getLogLevel(r.URL.Path, wrapped.status))
}

// convertErrorConfigs converts config.ErrorConfig to service.ErrorConfig
func convertErrorConfigs(errorCfgs []*config.ErrorConfig) ([]*service.ErrorConfig, error) {
	result := make([]*service.ErrorConfig, 0, len(errorCfgs))
	for _, errCfg := range errorCfgs {
		// Evaluate error response body if present
		var bodyStr string
		if errCfg.Response != nil && errCfg.Response.BodyExpr != nil {
			evalCtx := &hcl.EvalContext{
				Functions: config.Functions(),
			}
			value, diags := errCfg.Response.BodyExpr.Value(evalCtx)
			if diags.HasErrors() {
				return nil, fmt.Errorf("failed to evaluate error %q body: %s", errCfg.Name, diags.Error())
			}
			bodyStr = value.AsString()
		}

		headers := make(map[string]string)
		if errCfg.Response != nil && errCfg.Response.HeadersExpr != nil {
			headersEvalCtx := &hcl.EvalContext{
				Functions: config.Functions(),
			}
			headersVal, diags := errCfg.Response.HeadersExpr.Value(headersEvalCtx)
			if diags.HasErrors() {
				return nil, fmt.Errorf("failed to evaluate error %q headers: %s", errCfg.Name, diags.Error())
			}
			if !headersVal.IsNull() {
				for key, val := range headersVal.AsValueMap() {
					headers[key] = val.AsString()
				}
			}
		}

		result = append(result, &service.ErrorConfig{
			Name:    errCfg.Name,
			Rate:    errCfg.Rate,
			Status:  errCfg.Status,
			Headers: headers,
			Body:    bodyStr,
		})
	}
	return result, nil
}

// handleRequest handles a matched request
func (s *HTTPService) handleRequest(w http.ResponseWriter, r *http.Request, route *Route) {
	handler := route.Handler
	if handler.Response == nil {
		// No response configured - return empty 200
		w.WriteHeader(http.StatusOK)
		return
	}

	// Apply latency injection (handler-level overrides service-level)
	if handler.Timing != nil {
		// Handler has its own timing config - parse and create injector for it
		p50, err := service.ParseDuration(handler.Timing.P50)
		if err != nil {
			log.Printf("Error parsing handler timing.p50: %v", err)
		} else {
			p90, err := service.ParseDuration(handler.Timing.P90)
			if err != nil {
				log.Printf("Error parsing handler timing.p90: %v", err)
			} else {
				p99, err := service.ParseDuration(handler.Timing.P99)
				if err != nil {
					log.Printf("Error parsing handler timing.p99: %v", err)
				} else {
					handlerLatency := service.NewLatencyInjector(service.TimingConfig{
						P50:      p50,
						P90:      p90,
						P99:      p99,
						Variance: handler.Timing.Variance,
					})
					handlerLatency.Inject(r.Context())
				}
			}
		}
	} else if s.latencyInjector != nil {
		// Use service-level timing
		s.latencyInjector.Inject(r.Context())
	}

	// Apply error injection (handler-level overrides service-level)
	if len(handler.Errors) > 0 {
		// Handler has its own error configs - convert and create injector for them
		errorConfigs, err := convertErrorConfigs(handler.Errors)
		if err != nil {
			log.Printf("Error converting handler error configs: %v", err)
		} else {
			handlerErrors := service.NewErrorInjector(errorConfigs)
			if errCfg := handlerErrors.ShouldInject(); errCfg != nil {
				handlerErrors.WriteError(w, errCfg)
				return
			}
		}
	} else if s.errorInjector != nil {
		// Use service-level errors
		if errCfg := s.errorInjector.ShouldInject(); errCfg != nil {
			s.errorInjector.WriteError(w, errCfg)
			return
		}
	}

	// Build evaluation context from request
	// For now, we don't extract path params (future enhancement)
	evalCtx := config.BuildEvalContext(r, nil, s.config.ServiceVars)

	// Execute steps if present
	if len(handler.Steps) > 0 {
		executor := step.NewExecutor(handler.Steps)
		if err := executor.Execute(r.Context(), evalCtx); err != nil {
			log.Printf("Error executing steps: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(fmt.Sprintf(`{"error":"step execution failed: %s"}`, err.Error())))
			return
		}
	}

	resp := handler.Response

	// Evaluate response body expression if present
	var bodyStr string
	if resp.BodyExpr != nil {
		value, diags := resp.BodyExpr.Value(evalCtx)
		if diags.HasErrors() {
			log.Printf("Error evaluating response body: %v", diags.Error())
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(fmt.Sprintf(`{"error":"response evaluation failed: %s"}`, diags.Error())))
			return
		}

		// Convert to string
		bodyStr = value.AsString()
	}

	// Set status code
	status := http.StatusOK
	if resp.Status != nil {
		status = *resp.Status
	}

	// Evaluate and set headers
	if resp.HeadersExpr != nil {
		headersVal, diags := resp.HeadersExpr.Value(evalCtx)
		if diags.HasErrors() {
			log.Printf("Error evaluating response headers: %v", diags.Error())
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(fmt.Sprintf(`{"error":"header evaluation failed: %s"}`, diags.Error())))
			return
		}
		// Convert to map and set headers (check for null first)
		if !headersVal.IsNull() {
			for key, val := range headersVal.AsValueMap() {
				w.Header().Set(key, val.AsString())
			}
		}
	}

	// Set content-type if not already set and body contains JSON
	if w.Header().Get("Content-Type") == "" && bodyStr != "" {
		w.Header().Set("Content-Type", "application/json")
	}

	// Write response
	w.WriteHeader(status)
	if bodyStr != "" {
		w.Write([]byte(bodyStr))
	}
}

// isMetaServicePath checks if a path is a meta service internal call
func isMetaServicePath(path string) bool {
	return len(path) >= 6 && path[:6] == "/meta."
}

// getLogLevel determines the log level based on HTTP status code and request path
func getLogLevel(path string, status int) string {
	// Meta service calls are debug level
	if isMetaServicePath(path) {
		return "debug"
	}

	// Classify by HTTP status code
	if status >= 500 {
		return "error" // 5xx server errors
	}
	if status >= 400 {
		return "warn" // 4xx client errors
	}
	return "info" // 2xx, 3xx success/redirect
}

// init registers the HTTP service factory
func init() {
	service.RegisterFactory("http", func(cfg *config.ServiceConfig) (service.Service, error) {
		return NewHTTPService(cfg)
	})
}
