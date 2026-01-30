package config

import (
	"fmt"
	"os"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsimple"
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

// Parse parses HCL config from a byte slice
func Parse(src []byte, filename string) (*Config, error) {
	var config Config

	ctx := &hcl.EvalContext{
		Functions: Functions(),
	}

	err := hclsimple.Decode(filename, src, ctx, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

// Validate checks the configuration for errors
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	// Validate services
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

		// Validate handlers
		for j, handler := range svc.Handlers {
			if handler.Name == "" {
				return fmt.Errorf("service %q handler %d: name is required", svc.Name, j)
			}
		}
	}

	return nil
}

// FunctionsMap is a convenience wrapper for Functions that returns the correct type
func FunctionsMap() map[string]function.Function {
	return Functions()
}
