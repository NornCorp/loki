package config

import (
	"fmt"
	"net"
	"os"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// ParseFile reads and parses an HCL config file
func ParseFile(filename string) (*Config, error) {
	// Read the file
	src, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	return Parse(src, filename)
}

// Parse parses HCL config from a byte slice using two-phase parsing.
// Phase A extracts service skeletons (name, type, listen) to build service.* variables.
// Phase B decodes the full config with an enriched eval context containing service.* refs.
func Parse(src []byte, filename string) (*Config, error) {
	// Phase A: Parse file and extract service skeletons
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse config: %s", diags.Error())
	}

	serviceVars, err := extractServiceVars(file.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to extract service info: %w", err)
	}

	// Phase B: Full decode with enriched context
	ctx := &hcl.EvalContext{
		Functions: Functions(),
		Variables: make(map[string]cty.Value),
	}

	if len(serviceVars) > 0 {
		ctx.Variables["service"] = cty.ObjectVal(serviceVars)
	}

	var config Config
	diags = gohcl.DecodeBody(file.Body, ctx, &config)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse config: %s", diags.Error())
	}

	// Populate each service with the service vars map and infer upstreams
	for _, svc := range config.Services {
		svc.ServiceVars = serviceVars
	}
	if err := inferUpstreams(&config, serviceVars); err != nil {
		return nil, err
	}

	return &config, nil
}

// extractServiceVars reads service blocks from the raw HCL body and builds
// a map of service.* variables (address, host, port, type, url) for each service.
func extractServiceVars(body hcl.Body) (map[string]cty.Value, error) {
	syntaxBody, ok := body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("unexpected body type")
	}

	// Minimal context for evaluating literal attributes
	minCtx := &hcl.EvalContext{Functions: Functions()}

	serviceVars := make(map[string]cty.Value)

	for _, block := range syntaxBody.Blocks {
		if block.Type != "service" || len(block.Labels) < 1 {
			continue
		}

		name := block.Labels[0]

		var serviceType, listen string
		for attrName, attr := range block.Body.Attributes {
			if attrName == "type" {
				val, diags := attr.Expr.Value(minCtx)
				if !diags.HasErrors() && val.Type() == cty.String {
					serviceType = val.AsString()
				}
			}
			if attrName == "listen" {
				val, diags := attr.Expr.Value(minCtx)
				if !diags.HasErrors() && val.Type() == cty.String {
					listen = val.AsString()
				}
			}
		}

		host, port := splitHostPort(listen)
		url := fmt.Sprintf("http://%s", listen)

		serviceVars[name] = cty.ObjectVal(map[string]cty.Value{
			"address": cty.StringVal(listen),
			"host":    cty.StringVal(host),
			"port":    cty.StringVal(port),
			"type":    cty.StringVal(serviceType),
			"url":     cty.StringVal(url),
		})
	}

	return serviceVars, nil
}

// splitHostPort splits a listen address into host and port components.
func splitHostPort(addr string) (host, port string) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, ""
	}
	return h, p
}

// inferUpstreams scans all HCL expressions in each service for service.<name>
// references, validates they point to known services, and populates InferredUpstreams.
func inferUpstreams(cfg *Config, knownServices map[string]cty.Value) error {
	for _, svc := range cfg.Services {
		upstreams := make(map[string]bool)

		for _, expr := range collectExpressions(svc) {
			if expr == nil {
				continue
			}
			for _, traversal := range expr.Variables() {
				if len(traversal) >= 2 && traversal.RootName() == "service" {
					if attr, ok := traversal[1].(hcl.TraverseAttr); ok {
						if _, exists := knownServices[attr.Name]; !exists {
							return fmt.Errorf("service %q references unknown service %q", svc.Name, attr.Name)
						}
						if attr.Name != svc.Name {
							upstreams[attr.Name] = true
						}
					}
				}
			}
		}

		if len(upstreams) > 0 {
			svc.InferredUpstreams = make([]string, 0, len(upstreams))
			for name := range upstreams {
				svc.InferredUpstreams = append(svc.InferredUpstreams, name)
			}
			sort.Strings(svc.InferredUpstreams)
		}
	}

	return nil
}

// collectExpressions gathers all HCL expressions from a service config.
func collectExpressions(svc *ServiceConfig) []hcl.Expression {
	var exprs []hcl.Expression

	// Service-level expressions
	exprs = append(exprs, svc.TargetExpr, svc.RequestHeaders, svc.ResponseHeaders)

	// Handler expressions
	for _, handler := range svc.Handlers {
		if handler.Response != nil {
			exprs = append(exprs, handler.Response.BodyExpr, handler.Response.HeadersExpr)
		}
		for _, s := range handler.Steps {
			if s.HTTP != nil {
				exprs = append(exprs, s.HTTP.URLExpr, s.HTTP.BodyExpr, s.HTTP.HeadersExpr)
			}
		}
	}

	return exprs
}

// hasExpr reports whether an hcl.Expression was actually set in the HCL source.
// gohcl sets absent optional Expression fields to a synthetic null expression,
// so a simple nil check is insufficient.
func hasExpr(expr hcl.Expression) bool {
	if expr == nil {
		return false
	}
	if len(expr.Variables()) > 0 {
		return true
	}
	val, diags := expr.Value(nil)
	if diags.HasErrors() {
		return true // needs eval context → was explicitly set
	}
	return !val.IsNull()
}

// validServiceTypes is the set of allowed service types.
var validServiceTypes = map[string]bool{
	"http":     true,
	"proxy":    true,
	"tcp":      true,
	"connect":  true,
	"postgres": true,
}

// Validate checks the configuration for errors
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	for i, svc := range cfg.Services {
		if svc.Name == "" {
			return fmt.Errorf("service %d: name is required", i)
		}
		if svc.Type == "" {
			return fmt.Errorf("service %q: type is required", svc.Name)
		}
		if svc.Listen == "" {
			return fmt.Errorf("service %q: listen address is required", svc.Name)
		}
		if !validServiceTypes[svc.Type] {
			return fmt.Errorf("service %q: unknown type %q (must be http, proxy, tcp, connect, or postgres)", svc.Name, svc.Type)
		}

		if err := validateServiceFields(svc); err != nil {
			return err
		}

		for j, handler := range svc.Handlers {
			if handler.Name == "" {
				return fmt.Errorf("service %q handler %d: name is required", svc.Name, j)
			}
			if err := validateHandlerFields(svc, handler); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateServiceFields checks that type-specific service attributes are only
// used with their intended service type.
func validateServiceFields(svc *ServiceConfig) error {
	switch svc.Type {
	case "proxy":
		if svc.Package != "" {
			return fmt.Errorf("service %q: \"package\" is only valid for connect services", svc.Name)
		}
		if svc.Static != nil {
			return fmt.Errorf("service %q: \"static\" is only valid for http services", svc.Name)
		}
		if svc.Load != nil {
			return fmt.Errorf("service %q: \"load\" is only valid for http services", svc.Name)
		}
		if svc.RateLimit != nil {
			return fmt.Errorf("service %q: \"rate_limit\" is only valid for http services", svc.Name)
		}
	case "connect":
		if hasExpr(svc.TargetExpr) {
			return fmt.Errorf("service %q: \"target\" is only valid for proxy services", svc.Name)
		}
		if hasExpr(svc.RequestHeaders) {
			return fmt.Errorf("service %q: \"request_headers\" is only valid for proxy services", svc.Name)
		}
		if hasExpr(svc.ResponseHeaders) {
			return fmt.Errorf("service %q: \"response_headers\" is only valid for proxy services", svc.Name)
		}
		if svc.CORS != nil {
			return fmt.Errorf("service %q: \"cors\" is only valid for http and proxy services", svc.Name)
		}
		if svc.Static != nil {
			return fmt.Errorf("service %q: \"static\" is only valid for http services", svc.Name)
		}
		if svc.Load != nil {
			return fmt.Errorf("service %q: \"load\" is only valid for http services", svc.Name)
		}
		if svc.RateLimit != nil {
			return fmt.Errorf("service %q: \"rate_limit\" is only valid for http services", svc.Name)
		}
		if svc.Package == "" {
			return fmt.Errorf("service %q: \"package\" is required for connect services", svc.Name)
		}
	case "http":
		if svc.Package != "" {
			return fmt.Errorf("service %q: \"package\" is only valid for connect services", svc.Name)
		}
		if hasExpr(svc.TargetExpr) {
			return fmt.Errorf("service %q: \"target\" is only valid for proxy services", svc.Name)
		}
		if hasExpr(svc.RequestHeaders) {
			return fmt.Errorf("service %q: \"request_headers\" is only valid for proxy services", svc.Name)
		}
		if hasExpr(svc.ResponseHeaders) {
			return fmt.Errorf("service %q: \"response_headers\" is only valid for proxy services", svc.Name)
		}
	case "tcp":
		if svc.Package != "" {
			return fmt.Errorf("service %q: \"package\" is only valid for connect services", svc.Name)
		}
		if hasExpr(svc.TargetExpr) {
			return fmt.Errorf("service %q: \"target\" is only valid for proxy services", svc.Name)
		}
		if hasExpr(svc.RequestHeaders) {
			return fmt.Errorf("service %q: \"request_headers\" is only valid for proxy services", svc.Name)
		}
		if hasExpr(svc.ResponseHeaders) {
			return fmt.Errorf("service %q: \"response_headers\" is only valid for proxy services", svc.Name)
		}
		if svc.CORS != nil {
			return fmt.Errorf("service %q: \"cors\" is only valid for http and proxy services", svc.Name)
		}
		if svc.Static != nil {
			return fmt.Errorf("service %q: \"static\" is only valid for http services", svc.Name)
		}
		if svc.Load != nil {
			return fmt.Errorf("service %q: \"load\" is only valid for http services", svc.Name)
		}
		if svc.RateLimit != nil {
			return fmt.Errorf("service %q: \"rate_limit\" is only valid for http services", svc.Name)
		}
	case "postgres":
		if svc.Package != "" {
			return fmt.Errorf("service %q: \"package\" is only valid for connect services", svc.Name)
		}
		if hasExpr(svc.TargetExpr) {
			return fmt.Errorf("service %q: \"target\" is only valid for proxy services", svc.Name)
		}
		if hasExpr(svc.RequestHeaders) {
			return fmt.Errorf("service %q: \"request_headers\" is only valid for proxy services", svc.Name)
		}
		if hasExpr(svc.ResponseHeaders) {
			return fmt.Errorf("service %q: \"response_headers\" is only valid for proxy services", svc.Name)
		}
		if svc.CORS != nil {
			return fmt.Errorf("service %q: \"cors\" is only valid for http and proxy services", svc.Name)
		}
		if svc.Static != nil {
			return fmt.Errorf("service %q: \"static\" is only valid for http services", svc.Name)
		}
		if svc.Load != nil {
			return fmt.Errorf("service %q: \"load\" is only valid for http services", svc.Name)
		}
		if svc.RateLimit != nil {
			return fmt.Errorf("service %q: \"rate_limit\" is only valid for http services", svc.Name)
		}
		if len(svc.Resources) > 0 {
			return fmt.Errorf("service %q: use \"table\" blocks instead of \"resource\" blocks for postgres services", svc.Name)
		}
	}
	return nil
}

// validateHandlerFields checks that handler attributes match the parent service type.
func validateHandlerFields(svc *ServiceConfig, handler *HandlerConfig) error {
	switch svc.Type {
	case "http":
		if handler.Pattern != "" {
			return fmt.Errorf("service %q handler %q: \"pattern\" is only valid for tcp services", svc.Name, handler.Name)
		}
		if handler.Route == "" {
			return fmt.Errorf("service %q handler %q: \"route\" is required for http services", svc.Name, handler.Name)
		}
	case "proxy":
		if handler.Pattern != "" {
			return fmt.Errorf("service %q handler %q: \"pattern\" is only valid for tcp services", svc.Name, handler.Name)
		}
		if handler.Route == "" {
			return fmt.Errorf("service %q handler %q: \"route\" is required for proxy services", svc.Name, handler.Name)
		}
	case "tcp":
		if handler.Route != "" {
			return fmt.Errorf("service %q handler %q: \"route\" is not valid for tcp services", svc.Name, handler.Name)
		}
	case "connect":
		if handler.Pattern != "" {
			return fmt.Errorf("service %q handler %q: \"pattern\" is only valid for tcp services", svc.Name, handler.Name)
		}
		if handler.Route != "" {
			return fmt.Errorf("service %q handler %q: \"route\" is not valid for connect services (the handler label is the method name)", svc.Name, handler.Name)
		}
	case "postgres":
		if handler.Route != "" {
			return fmt.Errorf("service %q handler %q: \"route\" is not valid for postgres services", svc.Name, handler.Name)
		}
		if handler.Pattern != "" {
			return fmt.Errorf("service %q handler %q: \"pattern\" is not valid for postgres services", svc.Name, handler.Name)
		}
	}
	return nil
}

// FunctionsMap is a convenience wrapper for Functions that returns the correct type
func FunctionsMap() map[string]function.Function {
	return Functions()
}
