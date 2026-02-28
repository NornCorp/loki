package connect

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/zclconf/go-cty/cty"

	"github.com/norncorp/loki/internal/config"
)

var _ config.Service = (*Service)(nil)

// Service is the per-type configuration for Connect-RPC services.
type Service struct {
	// Shared fields
	Name    string
	Listen  string                `hcl:"listen"`
	TLS     *config.TLSConfig     `hcl:"tls,block"`
	Timing  *config.TimingConfig  `hcl:"timing,block"`
	Errors  []*config.ErrorConfig `hcl:"error,block"`
	Logging *config.LoggingConfig `hcl:"logging,block"`

	// Connect-specific fields
	Package   string                   `hcl:"package"`
	Resources []*config.ResourceConfig `hcl:"resource,block"`
	Handlers  []*Handler               `hcl:"handle,block"`

	// State set by parser (not from HCL)
	Vars      map[string]cty.Value
	Upstreams []string
}

// Handler is a Connect-RPC method handler.
type Handler struct {
	Name     string                 `hcl:"name,label"`
	Steps    []*config.StepConfig   `hcl:"step,block"`
	Response *config.ResponseConfig `hcl:"response,block"`
}

func (c *Service) SetName(n string)                       { c.Name = n }
func (c *Service) ServiceName() string                    { return c.Name }
func (c *Service) ServiceType() string                    { return "connect" }
func (c *Service) ServiceListen() string                  { return c.Listen }
func (c *Service) ServiceTLS() *config.TLSConfig          { return c.TLS }
func (c *Service) ServiceLogging() *config.LoggingConfig  { return c.Logging }
func (c *Service) SetServiceVars(v map[string]cty.Value)  { c.Vars = v }
func (c *Service) SetInferredUpstreams(u []string)        { c.Upstreams = u }
func (c *Service) GetServiceVars() map[string]cty.Value   { return c.Vars }
func (c *Service) GetInferredUpstreams() []string         { return c.Upstreams }
func (c *Service) GetResources() []*config.ResourceConfig { return c.Resources }

func (c *Service) Validate() error {
	if err := config.ValidateBase(c); err != nil {
		return err
	}
	if c.Package == "" {
		return fmt.Errorf("service %q: package is required for connect services", c.Name)
	}
	return nil
}

func (c *Service) Expressions() []hcl.Expression {
	var exprs []hcl.Expression
	for _, h := range c.Handlers {
		if h.Response != nil {
			exprs = append(exprs, h.Response.BodyExpr, h.Response.HeadersExpr)
		}
		for _, s := range h.Steps {
			if s.HTTP != nil {
				exprs = append(exprs, s.HTTP.URLExpr, s.HTTP.BodyExpr, s.HTTP.HeadersExpr)
			}
		}
	}
	return exprs
}

func (c *Service) GetHandlers() []config.HandlerConfig {
	handlers := make([]config.HandlerConfig, len(c.Handlers))
	for i, h := range c.Handlers {
		handlers[i] = config.HandlerConfig{
			Name:     h.Name,
			Steps:    h.Steps,
			Response: h.Response,
		}
	}
	return handlers
}

// Decode decodes an HCL block body into a Connect-RPC Config.
func Decode(body hcl.Body, ctx *hcl.EvalContext) (config.Service, error) {
	var cfg Service
	diags := gohcl.DecodeBody(body, ctx, &cfg)
	if diags.HasErrors() {
		return nil, diags
	}
	return &cfg, nil
}
