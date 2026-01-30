package config

import "github.com/hashicorp/hcl/v2"

// Config is the root configuration structure
type Config struct {
	Heimdall *HeimdallConfig `hcl:"heimdall,block"`
	Services []*ServiceConfig `hcl:"service,block"`
	Body     hcl.Body         `hcl:",remain"`
}

// HeimdallConfig configures the connection to Heimdall gossip mesh
type HeimdallConfig struct {
	Address string   `hcl:"address"`
	Body    hcl.Body `hcl:",remain"`
}

// ServiceConfig defines a service instance
type ServiceConfig struct {
	Name      string             `hcl:"name,label"`
	Type      string             `hcl:"type"`
	Listen    string             `hcl:"listen"`
	Upstreams []string           `hcl:"upstreams,optional"`
	Timing    *TimingConfig      `hcl:"timing,block"`
	Errors    []*ErrorConfig     `hcl:"error,block"`
	Handlers  []*HandlerConfig   `hcl:"handle,block"`
	Resources []*ResourceConfig  `hcl:"resource,block"`
	Body      hcl.Body           `hcl:",remain"`
}

// HandlerConfig defines a request handler
type HandlerConfig struct {
	Name     string          `hcl:"name,label"`
	Route    string          `hcl:"route,optional"`
	Steps    []*StepConfig   `hcl:"step,block"`
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
	URL     string            `hcl:"url"`
	Method  string            `hcl:"method,optional"`
	Headers map[string]string `hcl:"headers,optional"`
	Body    string            `hcl:"body,optional"`
	Remain  hcl.Body          `hcl:",remain"`
}

// ResponseConfig defines a response
type ResponseConfig struct {
	Status     *int              `hcl:"status,optional"`
	Headers    map[string]string `hcl:"headers,optional"`
	BodyExpr   hcl.Expression    `hcl:"body,optional"`
	Remain     hcl.Body          `hcl:",remain"`
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
