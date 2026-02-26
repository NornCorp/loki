package proxy

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/hashicorp/hcl/v2"
	"github.com/norncorp/loki/internal/config"
	"github.com/norncorp/loki/internal/service"
	"github.com/zclconf/go-cty/cty"
)

// proxyRoute represents a route override
type proxyRoute struct {
	method  string
	path    string
	handler http.HandlerFunc
}

// proxyRouter is a simple router for handle overrides
type proxyRouter struct {
	routes []*proxyRoute
}

// newProxyRouter creates a new router
func newProxyRouter() *proxyRouter {
	return &proxyRouter{
		routes: make([]*proxyRoute, 0),
	}
}

// add adds a route to the router
func (r *proxyRouter) add(method, path string, handler http.HandlerFunc) {
	r.routes = append(r.routes, &proxyRoute{
		method:  method,
		path:    path,
		handler: handler,
	})
}

// match finds a matching route for a request
func (r *proxyRouter) match(method, path string) http.HandlerFunc {
	for _, route := range r.routes {
		if route.method == method && route.path == path {
			return route.handler
		}
	}
	return nil
}

// ProxyService implements a reverse proxy service with transforms
type ProxyService struct {
	name         string
	config       *config.ServiceConfig
	server       *http.Server
	proxy        *httputil.ReverseProxy
	upstreamURL  *url.URL
	requestXfm   *Transform
	responseXfm  *Transform
	router       *proxyRouter
}

// NewProxyService creates a new proxy service
func NewProxyService(cfg *config.ServiceConfig) (*ProxyService, error) {
	if cfg.TargetExpr == nil {
		return nil, fmt.Errorf("target is required for proxy service")
	}

	// Evaluate target expression eagerly as a string (with service vars for service.* refs)
	evalCtx := &hcl.EvalContext{
		Functions: config.Functions(),
		Variables: make(map[string]cty.Value),
	}
	if len(cfg.ServiceVars) > 0 {
		evalCtx.Variables["service"] = cty.ObjectVal(cfg.ServiceVars)
	}
	targetVal, diags := cfg.TargetExpr.Value(evalCtx)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to evaluate target: %s", diags.Error())
	}
	targetStr := targetVal.AsString()

	// Parse upstream URL
	upstreamURL, err := url.Parse(targetStr)
	if err != nil {
		return nil, fmt.Errorf("invalid target URL: %w", err)
	}

	// Parse request header transforms
	var requestXfm *Transform
	if cfg.RequestHeaders != nil {
		requestXfm, err = parseHeadersExpr(cfg.RequestHeaders, evalCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to parse request_headers: %w", err)
		}
	}

	// Parse response header transforms
	var responseXfm *Transform
	if cfg.ResponseHeaders != nil {
		responseXfm, err = parseHeadersExpr(cfg.ResponseHeaders, evalCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to parse response_headers: %w", err)
		}
	}

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(upstreamURL)

	// Create router for handle overrides
	r := newProxyRouter()

	svc := &ProxyService{
		name:        cfg.Name,
		config:      cfg,
		proxy:       proxy,
		upstreamURL: upstreamURL,
		requestXfm:  requestXfm,
		responseXfm: responseXfm,
		router:      r,
	}

	// Add handle overrides to router
	for _, handler := range cfg.Handlers {
		if handler.Route == "" {
			return nil, fmt.Errorf("handler %q missing route", handler.Name)
		}

		// Parse route to extract method and path
		method, path, ok := parseRoute(handler.Route)
		if !ok {
			return nil, fmt.Errorf("invalid route format %q (expected \"METHOD /path\")", handler.Route)
		}

		// Create handler function
		handlerFn := svc.createHandlerOverride(handler)
		r.add(method, path, handlerFn)
	}

	// Customize proxy director to apply request transforms
	defaultDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		// Apply default director (sets Host, URL, etc.)
		defaultDirector(req)

		// Apply request transforms
		if requestXfm != nil {
			requestXfm.ApplyRequest(req)
		}
	}

	// Customize proxy response modifier to apply response transforms
	if responseXfm != nil {
		proxy.ModifyResponse = func(resp *http.Response) error {
			responseXfm.ApplyResponse(resp)
			return nil
		}
	}

	return svc, nil
}

// createHandlerOverride creates a handler function for a handle override
func (s *ProxyService) createHandlerOverride(handler *config.HandlerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// For now, just return the configured response
		// TODO: Add step execution support if needed
		if handler.Response != nil {
			// Build evaluation context with functions
			evalCtx := config.BuildEvalContext(r, nil, s.config.ServiceVars)

			// Set status code
			status := 200
			if handler.Response.Status != nil {
				status = *handler.Response.Status
			}

			// Evaluate headers
			if handler.Response.HeadersExpr != nil {
				headersVal, diags := handler.Response.HeadersExpr.Value(evalCtx)
				if !diags.HasErrors() && !headersVal.IsNull() {
					headersMap := headersVal.AsValueMap()
					for k, v := range headersMap {
						w.Header().Set(k, v.AsString())
					}
				}
			}

			// Evaluate body
			var body []byte
			if handler.Response.BodyExpr != nil {
				bodyVal, diags := handler.Response.BodyExpr.Value(evalCtx)
				if !diags.HasErrors() {
					body = []byte(bodyVal.AsString())
				}
			}

			w.WriteHeader(status)
			if len(body) > 0 {
				w.Write(body)
			}
		}
	}
}

// Name returns the service name
func (s *ProxyService) Name() string {
	return s.name
}

// Type returns the service type
func (s *ProxyService) Type() string {
	return "proxy"
}

// Address returns the service listen address
func (s *ProxyService) Address() string {
	return s.config.Listen
}

// Upstreams returns the list of upstream service dependencies
func (s *ProxyService) Upstreams() []string {
	return s.config.InferredUpstreams
}

// Start starts the proxy server
func (s *ProxyService) Start(ctx context.Context) error {
	// Create HTTP handler that checks router first, then proxies
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if there's a handle override for this route
		if handlerFn := s.router.match(r.Method, r.URL.Path); handlerFn != nil {
			handlerFn(w, r)
			return
		}

		// Otherwise, proxy to upstream
		s.proxy.ServeHTTP(w, r)
	})

	// Create HTTP server
	s.server = &http.Server{
		Addr:    s.config.Listen,
		Handler: handler,
	}

	// Start server in background
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Proxy service %q error: %v", s.name, err)
		}
	}()

	log.Printf("Proxy service %q listening on %s (target: %s)", s.name, s.config.Listen, s.upstreamURL)
	return nil
}

// Stop gracefully stops the proxy server
func (s *ProxyService) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	log.Printf("Stopping proxy service %q", s.name)
	return s.server.Shutdown(ctx)
}

// parseRoute parses a route string like "GET /path" into method and path
func parseRoute(route string) (method, path string, ok bool) {
	// Simple split on first space
	for i, c := range route {
		if c == ' ' {
			return route[:i], route[i+1:], true
		}
	}
	return "", "", false
}

// init registers the proxy service factory
func init() {
	service.RegisterFactory("proxy", func(cfg *config.ServiceConfig) (service.Service, error) {
		return NewProxyService(cfg)
	})
}
