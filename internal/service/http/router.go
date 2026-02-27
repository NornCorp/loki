package http

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/norncorp/loki/internal/config"
)

// Route represents a parsed HTTP route
type Route struct {
	Method  string
	Path    string
	Handler *config.HandlerConfig
}

// Router matches HTTP requests to handlers
type Router struct {
	routes []*Route
}

// NewRouter creates a new router
func NewRouter() *Router {
	return &Router{
		routes: make([]*Route, 0),
	}
}

// AddHandler adds a handler to the router
func (r *Router) AddHandler(handler *config.HandlerConfig) error {
	route, err := parseRoute(handler.Route)
	if err != nil {
		return fmt.Errorf("failed to parse route for handler %q: %w", handler.Name, err)
	}

	route.Handler = handler
	r.routes = append(r.routes, route)
	return nil
}

// Match finds a matching route for a request
func (r *Router) Match(req *http.Request) (*Route, bool) {
	for _, route := range r.routes {
		if r.matchRoute(route, req) {
			return route, true
		}
	}
	return nil, false
}

// matchRoute checks if a route matches a request
func (r *Router) matchRoute(route *Route, req *http.Request) bool {
	if route.Method != "" && route.Method != req.Method {
		return false
	}

	// Fast path: no params, exact match
	if !strings.Contains(route.Path, ":") {
		return route.Path == req.URL.Path
	}

	// Segment-by-segment matching with :param support
	routeParts := strings.Split(route.Path, "/")
	reqParts := strings.Split(req.URL.Path, "/")
	if len(routeParts) != len(reqParts) {
		return false
	}
	for i, rp := range routeParts {
		if strings.HasPrefix(rp, ":") {
			continue
		}
		if rp != reqParts[i] {
			return false
		}
	}
	return true
}

// ExtractParams extracts path parameter values from a matched route.
func ExtractParams(route *Route, req *http.Request) map[string]string {
	params := make(map[string]string)
	routeParts := strings.Split(route.Path, "/")
	reqParts := strings.Split(req.URL.Path, "/")
	for i, rp := range routeParts {
		if strings.HasPrefix(rp, ":") && i < len(reqParts) {
			params[rp[1:]] = reqParts[i]
		}
	}
	return params
}

// parseRoute parses a route string like "GET /hello" into method and path
func parseRoute(routeStr string) (*Route, error) {
	if routeStr == "" {
		return nil, fmt.Errorf("route string is empty")
	}

	parts := strings.Fields(routeStr)
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid route: %q", routeStr)
	}

	route := &Route{}

	// If there are 2 parts, first is method, second is path
	if len(parts) == 2 {
		route.Method = strings.ToUpper(parts[0])
		route.Path = parts[1]
	} else if len(parts) == 1 {
		// If there's only 1 part, it could be just a path (any method)
		// or just a method (any path)
		// We'll treat it as a path for now
		route.Path = parts[0]
	} else {
		return nil, fmt.Errorf("invalid route format: %q (expected 'METHOD /path' or '/path')", routeStr)
	}

	// Validate path starts with /
	if !strings.HasPrefix(route.Path, "/") {
		return nil, fmt.Errorf("path must start with /: %q", route.Path)
	}

	return route, nil
}
