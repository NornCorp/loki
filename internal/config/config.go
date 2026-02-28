package config

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

// Service is the interface that all per-type service configs implement.
type Service interface {
	SetName(string)
	ServiceName() string
	ServiceType() string
	ServiceListen() string
	ServiceTLS() *TLSConfig
	ServiceLogging() *LoggingConfig
	Validate() error
	Expressions() []hcl.Expression
	SetServiceVars(map[string]cty.Value)
	SetInferredUpstreams([]string)
	GetServiceVars() map[string]cty.Value
	GetInferredUpstreams() []string
	GetHandlers() []HandlerConfig
	GetResources() []*ResourceConfig
}

// ValidateBase checks constraints shared across all service types.
// Each per-type Config calls this from its own Validate() method.
func ValidateBase(s Service) error {
	if s.ServiceName() == "" {
		return fmt.Errorf("service name is required")
	}
	if s.ServiceListen() == "" {
		return fmt.Errorf("service %q: listen address is required", s.ServiceName())
	}
	if s.ServiceTLS() != nil && (s.ServiceTLS().Cert == "") != (s.ServiceTLS().Key == "") {
		return fmt.Errorf("service %q: TLS cert and key must both be set or both empty", s.ServiceName())
	}
	return nil
}
