package parser

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/jumppad-labs/polymorph/internal/config"
	"github.com/jumppad-labs/polymorph/internal/config/connect"
	"github.com/jumppad-labs/polymorph/internal/config/http"
	"github.com/jumppad-labs/polymorph/internal/config/postgres"
	"github.com/jumppad-labs/polymorph/internal/config/proxy"
	"github.com/jumppad-labs/polymorph/internal/config/tcp"
)

// serviceDecoders maps service type labels to their per-type decoders.
var serviceDecoders = map[string]func(hcl.Body, *hcl.EvalContext) (config.Service, error){
	"http":     http.Decode,
	"proxy":    proxy.Decode,
	"tcp":      tcp.Decode,
	"connect":  connect.Decode,
	"postgres": postgres.Decode,
}

// ParseFile reads and parses an HCL config file or directory.
// If path is a directory, all *.hcl files in it (non-recursive) are loaded and merged.
func ParseFile(path string) (*config.Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if !info.IsDir() {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		return Parse(src, path)
	}

	// Directory: glob *.hcl, sort by name, parse each
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config directory: %w", err)
	}

	var files []*hcl.File
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".hcl") {
			continue
		}
		filePath := filepath.Join(path, entry.Name())
		src, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file %s: %w", filePath, err)
		}
		file, diags := hclsyntax.ParseConfig(src, filePath, hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			return nil, fmt.Errorf("failed to parse config %s: %s", filePath, diags.Error())
		}
		files = append(files, file)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no .hcl files found in directory %s", path)
	}

	return parseFiles(files)
}

// Parse parses HCL config from a byte slice using three-phase parsing.
// Phase A extracts service skeletons (name, type, listen) to build service.* variables.
// Phase B decodes the root config (non-service blocks) with an enriched eval context.
// Phase C decodes service blocks via per-type decoders.
func Parse(src []byte, filename string) (*config.Config, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse config: %s", diags.Error())
	}
	return parseFiles([]*hcl.File{file})
}

// parseFiles implements the three-phase parsing pipeline over one or more HCL files.
func parseFiles(files []*hcl.File) (*config.Config, error) {
	// Phase A: Extract service skeletons from each file's syntax body
	serviceVars := make(map[string]cty.Value)
	for _, file := range files {
		vars, err := extractServiceVars(file.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to extract service info: %w", err)
		}
		for k, v := range vars {
			serviceVars[k] = v
		}
	}

	// Phase B: Decode root config (non-service blocks) with enriched context
	ctx := &hcl.EvalContext{
		Functions: config.Functions(),
		Variables: make(map[string]cty.Value),
	}
	if len(serviceVars) > 0 {
		ctx.Variables["service"] = cty.ObjectVal(serviceVars)
	}

	var cfg config.Config
	diags := gohcl.DecodeBody(hcl.MergeFiles(files), ctx, &cfg)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse config: %s", diags.Error())
	}

	// Phase C: Decode service blocks via per-type decoders (iterate each file's syntax body)
	for _, file := range files {
		syntaxBody, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			return nil, fmt.Errorf("unexpected body type")
		}
		for _, block := range syntaxBody.Blocks {
			if block.Type != "service" || len(block.Labels) < 2 {
				continue
			}
			serviceType := block.Labels[0]
			name := block.Labels[1]

			decoder, exists := serviceDecoders[serviceType]
			if !exists {
				return nil, fmt.Errorf("service %q: unknown type %q", name, serviceType)
			}

			svc, err := decoder(block.Body, ctx)
			if err != nil {
				return nil, fmt.Errorf("service %q: %w", name, err)
			}

			svc.SetName(name)
			svc.SetServiceVars(serviceVars)
			cfg.Services = append(cfg.Services, svc)
		}
	}

	if err := inferUpstreams(&cfg, serviceVars); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// extractServiceVars reads service blocks from the raw HCL body and builds
// a map of service.* variables (address, host, port, type, url) for each service.
func extractServiceVars(body hcl.Body) (map[string]cty.Value, error) {
	syntaxBody, ok := body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("unexpected body type")
	}

	minCtx := &hcl.EvalContext{Functions: config.Functions()}
	serviceVars := make(map[string]cty.Value)

	for _, block := range syntaxBody.Blocks {
		if block.Type != "service" || len(block.Labels) < 2 {
			continue
		}

		serviceType := block.Labels[0]
		name := block.Labels[1]

		var listen string
		for attrName, attr := range block.Body.Attributes {
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

func splitHostPort(addr string) (host, port string) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, ""
	}
	return h, p
}

// inferUpstreams scans all HCL expressions in each service for service.<name>
// references, validates they point to known services, and populates InferredUpstreams.
func inferUpstreams(cfg *config.Config, knownServices map[string]cty.Value) error {
	for _, svc := range cfg.Services {
		upstreams := make(map[string]bool)

		for _, expr := range svc.Expressions() {
			if expr == nil {
				continue
			}
			for _, traversal := range expr.Variables() {
				if len(traversal) >= 2 && traversal.RootName() == "service" {
					if attr, ok := traversal[1].(hcl.TraverseAttr); ok {
						if _, exists := knownServices[attr.Name]; !exists {
							return fmt.Errorf("service %q references unknown service %q", svc.ServiceName(), attr.Name)
						}
						if attr.Name != svc.ServiceName() {
							upstreams[attr.Name] = true
						}
					}
				}
			}
		}

		if len(upstreams) > 0 {
			names := make([]string, 0, len(upstreams))
			for name := range upstreams {
				names = append(names, name)
			}
			sort.Strings(names)
			svc.SetInferredUpstreams(names)
		}
	}

	return nil
}

// Validate checks the configuration for errors.
func Validate(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	if err := validateLogging(cfg.Logging, "logging"); err != nil {
		return err
	}
	if err := validateTracing(cfg.Tracing); err != nil {
		return err
	}
	if err := validateMetrics(cfg.Metrics); err != nil {
		return err
	}

	for _, svc := range cfg.Services {
		if err := svc.Validate(); err != nil {
			return err
		}
		for i, h := range svc.GetHandlers() {
			if h.Name == "" {
				return fmt.Errorf("service %q handler %d: name is required", svc.ServiceName(), i)
			}
		}
		if err := validateLogging(svc.ServiceLogging(), fmt.Sprintf("service %q logging", svc.ServiceName())); err != nil {
			return err
		}
	}

	return nil
}

// ValidateCLI checks CLI configuration for errors.
func ValidateCLI(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.CLI == nil {
		return fmt.Errorf("no cli block found in config")
	}

	cli := cfg.CLI
	if cli.Name == "" {
		return fmt.Errorf("cli: name is required")
	}

	for i, cmd := range cli.Commands {
		if err := validateCLICommand(cmd, fmt.Sprintf("cli.command[%d]", i)); err != nil {
			return err
		}
	}

	return nil
}

func validateCLICommand(cmd *config.CLICommandConfig, path string) error {
	if cmd.Name == "" {
		return fmt.Errorf("%s: name is required", path)
	}
	if cmd.Action != nil && len(cmd.Commands) > 0 {
		return fmt.Errorf("%s %q: cannot have both action and subcommands", path, cmd.Name)
	}
	if cmd.Action != nil {
		for j, step := range cmd.Action.Steps {
			if step.Name == "" {
				return fmt.Errorf("%s %q action step[%d]: name is required", path, cmd.Name, j)
			}
			if step.HTTP == nil {
				return fmt.Errorf("%s %q action step %q: must have an http block", path, cmd.Name, step.Name)
			}
		}
	}
	for i, sub := range cmd.Commands {
		if err := validateCLICommand(sub, fmt.Sprintf("%s %q command[%d]", path, cmd.Name, i)); err != nil {
			return err
		}
	}
	return nil
}

var validLoggingLevels = map[string]bool{
	"debug": true, "info": true, "warn": true, "error": true,
}

var validLoggingFormats = map[string]bool{
	"text": true, "json": true,
}

var validTracingSamplers = map[string]bool{
	"always_on": true, "always_off": true, "parent_based": true, "ratio": true,
}

func validateLogging(cfg *config.LoggingConfig, prefix string) error {
	if cfg == nil {
		return nil
	}
	if cfg.Level != nil && !validLoggingLevels[*cfg.Level] {
		return fmt.Errorf("%s: invalid logging level %q (must be debug, info, warn, or error)", prefix, *cfg.Level)
	}
	if cfg.Format != nil && !validLoggingFormats[*cfg.Format] {
		return fmt.Errorf("%s: invalid logging format %q (must be text or json)", prefix, *cfg.Format)
	}
	if cfg.Output != nil {
		v := *cfg.Output
		if v != "stdout" && v != "stderr" && v == "" {
			return fmt.Errorf("%s: logging output must be stdout, stderr, or a non-empty file path", prefix)
		}
	}
	return nil
}

func validateTracing(cfg *config.TracingConfig) error {
	if cfg == nil {
		return nil
	}
	if cfg.Sampler != nil && !validTracingSamplers[*cfg.Sampler] {
		return fmt.Errorf("tracing: invalid sampler %q (must be always_on, always_off, parent_based, or ratio)", *cfg.Sampler)
	}
	if cfg.Ratio != nil {
		if *cfg.Ratio < 0.0 || *cfg.Ratio > 1.0 {
			return fmt.Errorf("tracing: ratio must be between 0.0 and 1.0, got %g", *cfg.Ratio)
		}
	}
	sampler := ""
	if cfg.Sampler != nil {
		sampler = *cfg.Sampler
	}
	if sampler == "ratio" && cfg.Ratio == nil {
		return fmt.Errorf("tracing: ratio is required when sampler is \"ratio\"")
	}
	return nil
}

func validateMetrics(cfg *config.MetricsConfig) error {
	if cfg == nil {
		return nil
	}
	if cfg.Path != nil && !strings.HasPrefix(*cfg.Path, "/") {
		return fmt.Errorf("metrics: path must start with /, got %q", *cfg.Path)
	}
	return nil
}
