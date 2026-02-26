package config

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

// Config is the root configuration structure
type Config struct {
	Heimdall *HeimdallConfig `hcl:"heimdall,block"`
	Services []*ServiceConfig `hcl:"service,block"`
	Body     hcl.Body         `hcl:",remain"`
}

// HeimdallConfig configures the connection to Heimdall gossip mesh
type HeimdallConfig struct {
	Address  string   `hcl:"address"`
	NodeName string   `hcl:"node_name,optional"` // Optional custom node name (defaults to hostname)
	Body     hcl.Body `hcl:",remain"`
}

// ServiceConfig defines a service instance
type ServiceConfig struct {
	Name            string             `hcl:"name,label"`
	Type            string             `hcl:"type"`
	Listen          string             `hcl:"listen"`
	Package         string             `hcl:"package,optional"`          // For Connect-RPC service
	TargetExpr      hcl.Expression     `hcl:"target,optional"`           // For proxy service (expression for service refs)
	RequestHeaders  hcl.Expression     `hcl:"request_headers,optional"`  // For proxy service request header additions
	ResponseHeaders hcl.Expression     `hcl:"response_headers,optional"` // For proxy service response header additions
	CORS            *CORSConfig        `hcl:"cors,block"`                // For HTTP services
	Static          *StaticConfig      `hcl:"static,block"`              // For HTTP services
	Auth            *AuthConfig        `hcl:"auth,block"`                // For postgres service
	Timing          *TimingConfig      `hcl:"timing,block"`
	Load            *LoadConfig        `hcl:"load,block"`                // For HTTP services
	Errors          []*ErrorConfig     `hcl:"error,block"`
	RateLimit       *RateLimitConfig   `hcl:"rate_limit,block"`
	Handlers        []*HandlerConfig   `hcl:"handle,block"`
	Resources       []*ResourceConfig  `hcl:"resource,block"`
	Tables          []*TableConfig     `hcl:"table,block"`               // For postgres service
	Queries         []*QueryConfig     `hcl:"query,block"`               // For postgres service
	Body            hcl.Body           `hcl:",remain"`

	// Populated by parser (not from HCL)
	ServiceVars       map[string]cty.Value // service.* variables for expression evaluation
	InferredUpstreams []string             // auto-inferred upstream dependencies
}

// HandlerConfig defines a request handler
type HandlerConfig struct {
	Name     string          `hcl:"name,label"`
	Route    string          `hcl:"route,optional"`
	Pattern  string          `hcl:"pattern,optional"` // For TCP pattern matching
	Timing    *TimingConfig    `hcl:"timing,block"`
	Errors    []*ErrorConfig   `hcl:"error,block"`
	RateLimit *RateLimitConfig `hcl:"rate_limit,block"`
	Steps     []*StepConfig    `hcl:"step,block"`
	Response *ResponseConfig `hcl:"response,block"`
	Body     hcl.Body        `hcl:",remain"`
}

// StepConfig defines a step to execute before returning response
type StepConfig struct {
	Name string          `hcl:"name,label"`
	HTTP *HTTPStepConfig `hcl:"http,block"`
	Body hcl.Body        `hcl:",remain"`
}

// HTTPStepConfig defines an HTTP step
type HTTPStepConfig struct {
	URLExpr     hcl.Expression `hcl:"url"`
	Method      string         `hcl:"method,optional"`
	HeadersExpr hcl.Expression `hcl:"headers,optional"`
	BodyExpr    hcl.Expression `hcl:"body,optional"`
	Remain      hcl.Body       `hcl:",remain"`
}

// ResponseConfig defines a response
type ResponseConfig struct {
	Status      *int           `hcl:"status,optional"`
	HeadersExpr hcl.Expression `hcl:"headers,optional"`
	BodyExpr    hcl.Expression `hcl:"body,optional"`
	Remain      hcl.Body       `hcl:",remain"`
}

// TimingConfig defines latency injection parameters
type TimingConfig struct {
	P50      string  `hcl:"p50"`
	P90      string  `hcl:"p90"`
	P99      string  `hcl:"p99"`
	Variance float64 `hcl:"variance,optional"`
	Body     hcl.Body `hcl:",remain"`
}

// ErrorConfig defines an error injection rule
type ErrorConfig struct {
	Name     string          `hcl:"name,label"`
	Rate     float64         `hcl:"rate"`
	Status   int             `hcl:"status"`
	Response *ResponseConfig `hcl:"response,block"`
	Body     hcl.Body        `hcl:",remain"`
}

// RateLimitConfig defines rate limiting parameters
type RateLimitConfig struct {
	RPS      float64         `hcl:"rps"`
	Status   int             `hcl:"status,optional"`
	Response *ResponseConfig `hcl:"response,block"`
	Body     hcl.Body        `hcl:",remain"`
}

// CORSConfig defines CORS settings for HTTP services
type CORSConfig struct {
	AllowedOrigins   []string `hcl:"allowed_origins"`
	AllowedMethods   []string `hcl:"allowed_methods,optional"`
	AllowedHeaders   []string `hcl:"allowed_headers,optional"`
	AllowCredentials *bool    `hcl:"allow_credentials,optional"`
	Body             hcl.Body `hcl:",remain"`
}

// LoadConfig defines load generation parameters
type LoadConfig struct {
	CPUCores   int     `hcl:"cpu_cores,optional"`
	CPUPercent float64 `hcl:"cpu_percent,optional"`
	Memory     string  `hcl:"memory,optional"`
	Body       hcl.Body `hcl:",remain"`
}

// StaticConfig defines a static file server
type StaticConfig struct {
	Route string   `hcl:"route,optional"`
	Root  string   `hcl:"root"`
	Body  hcl.Body `hcl:",remain"`
}

// AuthConfig defines authentication for postgres services
type AuthConfig struct {
	Users    map[string]string `hcl:"users"`
	Database string            `hcl:"database"`
	Body     hcl.Body          `hcl:",remain"`
}

// TableConfig defines a table for postgres services (similar to ResourceConfig)
type TableConfig struct {
	Name    string          `hcl:"name,label"`
	Rows    int             `hcl:"rows,optional"`
	Seed    *int64          `hcl:"seed,optional"`
	Columns []*ColumnConfig `hcl:"column,block"`
	Body    hcl.Body        `hcl:",remain"`
}

// ColumnConfig defines a column in a postgres table
type ColumnConfig struct {
	Name   string         `hcl:"name,label"`
	Type   string         `hcl:"type"`
	Config map[string]any `hcl:"config,optional"`
	Min    *float64       `hcl:"min,optional"`
	Max    *float64       `hcl:"max,optional"`
	Values []string       `hcl:"values,optional"`
	Body   hcl.Body       `hcl:",remain"`
}

// QueryConfig defines a custom query pattern for postgres services
type QueryConfig struct {
	Pattern   string   `hcl:"pattern,label"`
	FromTable string   `hcl:"from_table,optional"`
	Where     string   `hcl:"where,optional"`
	Body      hcl.Body `hcl:",remain"`
}

