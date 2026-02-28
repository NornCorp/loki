package http

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/norncorp/loki/internal/config"
	confighttp "github.com/norncorp/loki/internal/config/http"
	"github.com/norncorp/loki/internal/meta"
	"github.com/norncorp/loki/internal/metrics"
	"github.com/norncorp/loki/internal/resource"
	"github.com/norncorp/loki/internal/serf"
	"github.com/norncorp/loki/internal/service"
	"github.com/norncorp/loki/internal/step"
	"github.com/norncorp/loki/internal/tracing"
	"github.com/norncorp/loki/pkg/api/meta/v1/metaapiconnect"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// HTTPService implements an HTTP service
type HTTPService struct {
	name             string
	config           *confighttp.Service
	logger           *slog.Logger
	router           *Router
	resourceHandlers []*ResourceHandler
	resourceStore    *resource.Store
	server           *http.Server
	listener         net.Listener
	latencyInjector  *service.LatencyInjector
	errorInjector    *service.ErrorInjector
	mux              *http.ServeMux
	allConfigs       []config.Service                // All services for meta API
	requestLogger    *RequestLogger                  // Request log ring buffer
	staticHandler    http.Handler                    // Static file server (optional)
	staticPrefix     string                          // URL prefix for static files
	loadGenerator    *service.LoadGenerator          // CPU/memory load generator (optional)
	rateLimiter      *service.RateLimiter            // Service-level rate limiter (optional)
	handlerLimiters  map[string]*service.RateLimiter // Handler-level rate limiters
	metricsEnabled   bool                            // Whether to serve metrics endpoint
	metricsPath      string                          // Prometheus scrape path
	specHandler      *SpecHandler                    // OpenAPI spec handler (optional)
}

// NewHTTPService creates a new HTTP service
func NewHTTPService(cfg *confighttp.Service, logger *slog.Logger) (*HTTPService, error) {
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
		logger:           logger,
		router:           router,
		resourceStore:    resourceStore,
		resourceHandlers: resourceHandlers,
		latencyInjector:  latencyInjector,
		errorInjector:    errorInjector,
		requestLogger:    NewRequestLogger(1000), // Store last 1000 requests
		metricsEnabled:   metrics.IsEnabled(),
		metricsPath:      metrics.Path(),
	}

	// Set up static file server if configured
	if cfg.Static != nil {
		fs := http.FileServer(http.Dir(cfg.Static.Root))
		prefix := cfg.Static.Route
		if prefix == "" {
			prefix = "/"
		}
		svc.staticPrefix = prefix
		if prefix != "/" {
			svc.staticHandler = http.StripPrefix(prefix, fs)
		} else {
			svc.staticHandler = fs
		}
	}

	// Set up spec handler if configured
	if cfg.Spec != nil {
		sh, err := NewSpecHandler(cfg.Spec, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to load OpenAPI spec: %w", err)
		}
		svc.specHandler = sh
	}

	// Set up load generator if configured
	if cfg.Load != nil {
		var memBytes int64
		if cfg.Load.Memory != "" {
			var err error
			memBytes, err = service.ParseMemorySize(cfg.Load.Memory)
			if err != nil {
				return nil, fmt.Errorf("failed to parse load.memory: %w", err)
			}
		}
		cpuPercent := cfg.Load.CPUPercent
		if cpuPercent > 100 {
			cpuPercent = 100
		}
		svc.loadGenerator = service.NewLoadGenerator(service.LoadConfig{
			CPUCores:   cfg.Load.CPUCores,
			CPUPercent: cpuPercent / 100.0, // Convert percentage to 0.0-1.0
			Memory:     memBytes,
		})
	}

	// Set up rate limiter if configured
	if cfg.RateLimit != nil {
		rlCfg := service.RateLimitConfig{
			RPS:    cfg.RateLimit.RPS,
			Status: cfg.RateLimit.Status,
		}
		if cfg.RateLimit.Response != nil {
			if cfg.RateLimit.Response.BodyExpr != nil {
				evalCtx := &hcl.EvalContext{Functions: config.Functions()}
				value, diags := cfg.RateLimit.Response.BodyExpr.Value(evalCtx)
				if diags.HasErrors() {
					return nil, fmt.Errorf("failed to evaluate rate_limit response body: %s", diags.Error())
				}
				rlCfg.Body = value.AsString()
			}
			if cfg.RateLimit.Response.HeadersExpr != nil {
				evalCtx := &hcl.EvalContext{Functions: config.Functions()}
				headersVal, diags := cfg.RateLimit.Response.HeadersExpr.Value(evalCtx)
				if diags.HasErrors() {
					return nil, fmt.Errorf("failed to evaluate rate_limit response headers: %s", diags.Error())
				}
				rlCfg.Headers = make(map[string]string)
				if !headersVal.IsNull() {
					for key, val := range headersVal.AsValueMap() {
						rlCfg.Headers[key] = val.AsString()
					}
				}
			}
		}
		svc.rateLimiter = service.NewRateLimiter(rlCfg)
	}

	// Set up handler-level rate limiters
	for _, handler := range cfg.Handlers {
		if handler.RateLimit != nil {
			if svc.handlerLimiters == nil {
				svc.handlerLimiters = make(map[string]*service.RateLimiter)
			}
			hlCfg := service.RateLimitConfig{
				RPS:    handler.RateLimit.RPS,
				Status: handler.RateLimit.Status,
			}
			if handler.RateLimit.Response != nil {
				if handler.RateLimit.Response.BodyExpr != nil {
					evalCtx := &hcl.EvalContext{Functions: config.Functions()}
					value, diags := handler.RateLimit.Response.BodyExpr.Value(evalCtx)
					if diags.HasErrors() {
						return nil, fmt.Errorf("failed to evaluate handler %q rate_limit response body: %s", handler.Name, diags.Error())
					}
					hlCfg.Body = value.AsString()
				}
				if handler.RateLimit.Response.HeadersExpr != nil {
					evalCtx := &hcl.EvalContext{Functions: config.Functions()}
					headersVal, diags := handler.RateLimit.Response.HeadersExpr.Value(evalCtx)
					if diags.HasErrors() {
						return nil, fmt.Errorf("failed to evaluate handler %q rate_limit response headers: %s", handler.Name, diags.Error())
					}
					hlCfg.Headers = make(map[string]string)
					if !headersVal.IsNull() {
						for key, val := range headersVal.AsValueMap() {
							hlCfg.Headers[key] = val.AsString()
						}
					}
				}
			}
			svc.handlerLimiters[handler.Name] = service.NewRateLimiter(hlCfg)
		}
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
	return s.config.Upstreams
}

// ConfigureMetaService sets up the meta service RPC handler
func (s *HTTPService) ConfigureMetaService(allConfigs []config.Service, serfClient *serf.Client, logProvider meta.RequestLogProvider) {
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
	s.logger.Info("meta service registered", "path", path)
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

	// Wrap with TLS if configured
	listener, err = service.WrapListenerTLS(listener, s.config.TLS)
	if err != nil {
		listener.Close()
		return fmt.Errorf("failed to configure TLS: %w", err)
	}
	s.listener = listener

	// Create HTTP server
	s.server = &http.Server{
		Handler: s,
	}

	// Start server in background
	proto := "HTTP"
	if s.config.TLS != nil {
		proto = "HTTPS"
	}
	go func() {
		s.logger.Info("service listening", "proto", proto, "addr", s.config.Listen)
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.Error("server error", "error", err)
		}
	}()

	return nil
}

// Stop gracefully stops the HTTP server
func (s *HTTPService) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	s.logger.Info("stopping service")

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
	// Serve Prometheus metrics endpoint
	if s.metricsEnabled && r.URL.Path == s.metricsPath {
		metrics.Handler().ServeHTTP(w, r)
		return
	}

	start := time.Now()

	// Wrap response writer to capture status code
	wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

	// Apply CORS headers
	if s.config.CORS != nil {
		origin := r.Header.Get("Origin")
		cors := s.config.CORS

		// Check if origin is allowed
		allowed := false
		for _, o := range cors.AllowedOrigins {
			if o == "*" || o == origin {
				allowed = true
				break
			}
		}

		if allowed {
			if len(cors.AllowedOrigins) == 1 && cors.AllowedOrigins[0] == "*" {
				wrapped.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				wrapped.Header().Set("Access-Control-Allow-Origin", origin)
				wrapped.Header().Set("Vary", "Origin")
			}

			methods := "GET, POST, PUT, DELETE, OPTIONS"
			if len(cors.AllowedMethods) > 0 {
				methods = strings.Join(cors.AllowedMethods, ", ")
			}
			wrapped.Header().Set("Access-Control-Allow-Methods", methods)

			headers := "Content-Type, Authorization"
			if len(cors.AllowedHeaders) > 0 {
				headers = strings.Join(cors.AllowedHeaders, ", ")
			}
			wrapped.Header().Set("Access-Control-Allow-Headers", headers)

			if cors.AllowCredentials != nil && *cors.AllowCredentials {
				wrapped.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			wrapped.WriteHeader(http.StatusNoContent)
			s.requestLogger.Log(r.Method, r.URL.Path, wrapped.status, time.Since(start), getLogLevel(r.URL.Path, wrapped.status))
			return
		}
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
		// Try spec handler (OpenAPI-derived routes)
		if s.specHandler != nil {
			if specRoute, matched := s.specHandler.Match(r.Method, r.URL.Path); matched {
				s.handleSpecRoute(wrapped, r, specRoute)
				duration := time.Since(start)
				s.requestLogger.Log(r.Method, r.URL.Path, wrapped.status, duration, getLogLevel(r.URL.Path, wrapped.status))
				metrics.RecordRequest(s.name, "spec", wrapped.status, duration)
				return
			}
		}

		// Try static file server if configured
		if s.staticHandler != nil && strings.HasPrefix(r.URL.Path, s.staticPrefix) {
			s.staticHandler.ServeHTTP(wrapped, r)
			duration := time.Since(start)
			s.requestLogger.Log(r.Method, r.URL.Path, wrapped.status, duration, getLogLevel(r.URL.Path, wrapped.status))
			metrics.RecordRequest(s.name, "static", wrapped.status, duration)
			return
		}

		// No matching route - return 404
		wrapped.WriteHeader(http.StatusNotFound)
		wrapped.Header().Set("Content-Type", "application/json")
		wrapped.Write([]byte(`{"error":"not found"}`))
		// Log the 404
		duration := time.Since(start)
		s.requestLogger.Log(r.Method, r.URL.Path, wrapped.status, duration, getLogLevel(r.URL.Path, wrapped.status))
		metrics.RecordRequest(s.name, "not_found", wrapped.status, duration)
		return
	}

	// Handle the request with the matched route
	s.handleRequest(wrapped, r, route)

	// Log and record metrics
	duration := time.Since(start)
	s.requestLogger.Log(r.Method, r.URL.Path, wrapped.status, duration, getLogLevel(r.URL.Path, wrapped.status))
	metrics.RecordRequest(s.name, route.Handler.Name, wrapped.status, duration)
}

// handleSpecRoute applies service-level injection and writes a spec-derived response.
func (s *HTTPService) handleSpecRoute(w http.ResponseWriter, r *http.Request, route *specRoute) {
	// Apply service-level latency injection
	if s.latencyInjector != nil {
		s.latencyInjector.Inject(r.Context())
	}

	// Apply service-level error injection
	if s.errorInjector != nil {
		if errCfg := s.errorInjector.ShouldInject(); errCfg != nil {
			s.errorInjector.WriteError(w, errCfg)
			return
		}
	}

	// Apply service-level rate limiting
	if s.rateLimiter != nil {
		if !s.rateLimiter.Allow() {
			s.rateLimiter.WriteError(w)
			return
		}
	}

	// Apply load generation
	if s.loadGenerator != nil {
		loadCtx, loadCancel := context.WithCancel(r.Context())
		defer loadCancel()
		go s.loadGenerator.Generate(loadCtx)
	}

	// Write pre-generated response
	s.specHandler.Handle(w, r, route)
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

	// Start tracing span
	tracer := tracing.Tracer("loki.http")
	ctx, span := tracer.Start(r.Context(), fmt.Sprintf("%s %s", r.Method, r.URL.Path),
		trace.WithAttributes(
			attribute.String("service", s.name),
			attribute.String("handler", handler.Name),
		),
	)
	defer span.End()
	r = r.WithContext(ctx)

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
			s.logger.Error("failed to parse handler timing.p50", "handler", handler.Name, "error", err)
		} else {
			p90, err := service.ParseDuration(handler.Timing.P90)
			if err != nil {
				s.logger.Error("failed to parse handler timing.p90", "handler", handler.Name, "error", err)
			} else {
				p99, err := service.ParseDuration(handler.Timing.P99)
				if err != nil {
					s.logger.Error("failed to parse handler timing.p99", "handler", handler.Name, "error", err)
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
			s.logger.Error("failed to convert handler error configs", "handler", handler.Name, "error", err)
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
			metrics.RecordError(s.name, handler.Name, "injected")
			s.errorInjector.WriteError(w, errCfg)
			return
		}
	}

	// Apply rate limiting (handler-level overrides service-level)
	if rl, ok := s.handlerLimiters[handler.Name]; ok {
		if !rl.Allow() {
			rl.WriteError(w)
			return
		}
	} else if s.rateLimiter != nil {
		if !s.rateLimiter.Allow() {
			s.rateLimiter.WriteError(w)
			return
		}
	}

	// Apply load generation (runs in background for duration of request)
	if s.loadGenerator != nil {
		loadCtx, loadCancel := context.WithCancel(r.Context())
		defer loadCancel()
		go s.loadGenerator.Generate(loadCtx)
	}

	// Build evaluation context from request
	pathParams := ExtractParams(route, r)
	evalCtx := config.BuildEvalContext(r, pathParams, s.config.Vars)

	// Execute steps if present
	if len(handler.Steps) > 0 {
		executor := step.NewExecutor(handler.Steps)
		if err := executor.Execute(r.Context(), evalCtx); err != nil {
			s.logger.Error("step execution failed", "handler", handler.Name, "error", err)
			metrics.RecordError(s.name, handler.Name, "step_failed")
			span.RecordError(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"error":"step execution failed: %s"}`, err.Error())
			return
		}
	}

	resp := handler.Response

	// Evaluate response body expression if present
	var bodyStr string
	if resp.BodyExpr != nil {
		value, diags := resp.BodyExpr.Value(evalCtx)
		if diags.HasErrors() {
			s.logger.Error("failed to evaluate response body", "handler", handler.Name, "error", diags.Error())
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
			s.logger.Error("failed to evaluate response headers", "handler", handler.Name, "error", diags.Error())
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
	service.RegisterFactory("http", func(cfg config.Service, logger *slog.Logger) (service.Service, error) {
		c, ok := cfg.(*confighttp.Service)
		if !ok {
			return nil, fmt.Errorf("http: unexpected config type %T", cfg)
		}
		return NewHTTPService(c, logger)
	})
}
