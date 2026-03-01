package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"

	"github.com/jumppad-labs/polymorph/internal/config"
	"github.com/jumppad-labs/polymorph/internal/config/connect"
	"github.com/jumppad-labs/polymorph/internal/config/http"
	"github.com/jumppad-labs/polymorph/internal/config/proxy"
	"github.com/jumppad-labs/polymorph/internal/config/tcp"
)

func TestParseFile_MinimalHTTPService(t *testing.T) {
	cfg, err := ParseFile("../testdata/minimal.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.Len(t, cfg.Services, 1)
	svc := cfg.Services[0]
	require.Equal(t, "api", svc.ServiceName())
	require.Equal(t, "http", svc.ServiceType())
	require.Equal(t, "0.0.0.0:8080", svc.ServiceListen())

	httpCfg := svc.(*http.Service)
	require.Empty(t, httpCfg.Handlers)
}

func TestParseFile_WithHandlers(t *testing.T) {
	cfg, err := ParseFile("../testdata/with_handlers.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.Len(t, cfg.Services, 1)
	svc := cfg.Services[0]
	require.Equal(t, "api", svc.ServiceName())

	httpCfg := svc.(*http.Service)
	require.Len(t, httpCfg.Handlers, 2)

	// Check hello handler
	hello := httpCfg.Handlers[0]
	require.Equal(t, "hello", hello.Name)
	require.Equal(t, "GET /hello", hello.Route)
	require.NotNil(t, hello.Response)
	require.NotNil(t, hello.Response.BodyExpr)

	// Evaluate the body expression
	evalCtx := &hcl.EvalContext{Functions: config.Functions()}
	value, diags := hello.Response.BodyExpr.Value(evalCtx)
	require.False(t, diags.HasErrors())
	bodyStr := value.AsString()
	require.Contains(t, bodyStr, "Hello from Polymorph!")

	// Verify JSON is valid
	var parsed map[string]interface{}
	err = json.Unmarshal([]byte(bodyStr), &parsed)
	require.NoError(t, err)
	require.Equal(t, "Hello from Polymorph!", parsed["message"])

	// Check health handler
	health := httpCfg.Handlers[1]
	require.Equal(t, "health", health.Name)
	require.Equal(t, "GET /health", health.Route)
	require.NotNil(t, health.Response)
	require.NotNil(t, health.Response.BodyExpr)

	value, diags = health.Response.BodyExpr.Value(evalCtx)
	require.False(t, diags.HasErrors())
	require.Contains(t, value.AsString(), "healthy")
}

func TestParseFile_CustomFunctions(t *testing.T) {
	cfg, err := ParseFile("../testdata/functions.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.Len(t, cfg.Services, 1)
	httpCfg := cfg.Services[0].(*http.Service)
	require.Len(t, httpCfg.Handlers, 1)

	handler := httpCfg.Handlers[0]
	require.NotNil(t, handler.Response)
	require.NotNil(t, handler.Response.BodyExpr)

	// Evaluate the body expression
	evalCtx := &hcl.EvalContext{Functions: config.Functions()}
	value, diags := handler.Response.BodyExpr.Value(evalCtx)
	require.False(t, diags.HasErrors())

	// Parse the JSON body
	var parsed map[string]interface{}
	err = json.Unmarshal([]byte(value.AsString()), &parsed)
	require.NoError(t, err)

	// Verify uuid() function generated a valid UUID
	id, ok := parsed["id"].(string)
	require.True(t, ok, "id should be a string")
	require.Len(t, id, 36, "UUID should be 36 characters")
	require.Contains(t, id, "-", "UUID should contain hyphens")

	// Verify timestamp() function generated a valid timestamp
	ts, ok := parsed["timestamp"].(string)
	require.True(t, ok, "timestamp should be a string")
	require.True(t, strings.HasSuffix(ts, "Z"), "timestamp should be in UTC (end with Z)")
	require.Contains(t, ts, "T", "timestamp should contain T separator")

	// Verify jsonencode() function properly encoded nested object
	data, ok := parsed["data"].(map[string]interface{})
	require.True(t, ok, "data should be an object")
	require.Equal(t, "bar", data["foo"])
	require.Equal(t, float64(42), data["num"])
}

func TestParseFile_InvalidSyntax(t *testing.T) {
	cfg, err := ParseFile("../testdata/invalid_syntax.hcl")
	require.Error(t, err)
	require.Nil(t, cfg)
	require.Contains(t, err.Error(), "failed to parse config")
}

func TestParseFile_FileNotFound(t *testing.T) {
	cfg, err := ParseFile("../testdata/nonexistent.hcl")
	require.Error(t, err)
	require.Nil(t, cfg)
	require.Contains(t, err.Error(), "failed to read config file")
}

func TestParseFile_WithHeimdall(t *testing.T) {
	cfg, err := ParseFile("../testdata/with_heimdall.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.NotNil(t, cfg.Heimdall)
	require.Equal(t, "heimdall:7946", cfg.Heimdall.Address)

	require.Len(t, cfg.Services, 1)
	require.Equal(t, "api", cfg.Services[0].ServiceName())
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg, err := ParseFile("../testdata/minimal.hcl")
	require.NoError(t, err)

	err = Validate(cfg)
	require.NoError(t, err)
}

func TestValidate_NilConfig(t *testing.T) {
	err := Validate(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "config is nil")
}

func TestValidate_MissingListen(t *testing.T) {
	cfg := &config.Config{
		Services: []config.Service{
			&http.Service{
				Name: "api",
			},
		},
	}

	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "listen address is required")
}

func TestParse_FromBytes(t *testing.T) {
	src := []byte(`
service "http" "test" {
  listen = "0.0.0.0:9000"
}
`)

	cfg, err := Parse(src, "test.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Services, 1)
	require.Equal(t, "test", cfg.Services[0].ServiceName())
	require.Equal(t, "0.0.0.0:9000", cfg.Services[0].ServiceListen())
}

func TestFunctions_Jsonencode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple object",
			input:    `{ foo = "bar" }`,
			expected: `{"foo":"bar"}`,
		},
		{
			name:     "nested object",
			input:    `{ outer = { inner = "value" } }`,
			expected: `{"outer":{"inner":"value"}}`,
		},
		{
			name:     "array",
			input:    `["a", "b", "c"]`,
			expected: `["a","b","c"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := []byte(`
service "http" "test" {
  listen = "0.0.0.0:8080"

  handle "test" {
    route = "GET /test"
    response {
      body = jsonencode(` + tt.input + `)
    }
  }
}
`)

			cfg, err := Parse(src, "test.hcl")
			require.NoError(t, err)
			require.NotNil(t, cfg)
			require.Len(t, cfg.Services, 1)

			httpCfg := cfg.Services[0].(*http.Service)
			require.Len(t, httpCfg.Handlers, 1)

			// Evaluate the body expression
			evalCtx := &hcl.EvalContext{Functions: config.Functions()}
			value, diags := httpCfg.Handlers[0].Response.BodyExpr.Value(evalCtx)
			require.False(t, diags.HasErrors())
			require.Equal(t, tt.expected, value.AsString())
		})
	}
}

func TestParse_ServiceReferences(t *testing.T) {
	cfg, err := ParseFile("../testdata/service_refs.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Services, 2)

	// Verify service vars are populated on all services
	backend := cfg.Services[0]
	require.Equal(t, "backend", backend.ServiceName())
	require.NotNil(t, backend.GetServiceVars())
	require.Contains(t, backend.GetServiceVars(), "backend")
	require.Contains(t, backend.GetServiceVars(), "proxy")

	proxySvc := cfg.Services[1]
	require.Equal(t, "proxy", proxySvc.ServiceName())

	// Verify the service.backend.* vars resolve correctly
	backendVars := backend.GetServiceVars()["backend"]
	backendMap := backendVars.AsValueMap()
	require.Equal(t, "http://127.0.0.1:8081", backendMap["url"].AsString())
	require.Equal(t, "127.0.0.1:8081", backendMap["address"].AsString())
	require.Equal(t, "127.0.0.1", backendMap["host"].AsString())
	require.Equal(t, "8081", backendMap["port"].AsString())
	require.Equal(t, "http", backendMap["type"].AsString())

	// Verify proxy's target expression can be evaluated with service vars
	proxyCfg := proxySvc.(*proxy.Service)
	require.NotNil(t, proxyCfg.TargetExpr)
	evalCtx := &hcl.EvalContext{
		Functions: config.Functions(),
		Variables: map[string]cty.Value{
			"service": cty.ObjectVal(proxySvc.GetServiceVars()),
		},
	}
	targetVal, diags := proxyCfg.TargetExpr.Value(evalCtx)
	require.False(t, diags.HasErrors())
	require.Equal(t, "http://127.0.0.1:8081", targetVal.AsString())
}

func TestParse_ServiceReferences_UnknownService(t *testing.T) {
	src := []byte(`
service "proxy" "proxy" {
  listen = "0.0.0.0:8080"
  target = service.nonexistent.url
}
`)

	cfg, err := Parse(src, "test.hcl")
	require.Error(t, err)
	require.Nil(t, cfg)
	require.Contains(t, err.Error(), "service")
}

func TestParse_InferUpstreams(t *testing.T) {
	cfg, err := ParseFile("../testdata/gateway_refs.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Services, 3)

	// The gateway references user-service and order-service via step URLs
	gateway := cfg.Services[2]
	require.Equal(t, "api-gateway", gateway.ServiceName())
	require.Len(t, gateway.GetInferredUpstreams(), 2)
	require.Contains(t, gateway.GetInferredUpstreams(), "user-service")
	require.Contains(t, gateway.GetInferredUpstreams(), "order-service")

	// The upstream services should have no inferred upstreams
	require.Empty(t, cfg.Services[0].GetInferredUpstreams())
	require.Empty(t, cfg.Services[1].GetInferredUpstreams())
}

func TestParse_InferUpstreams_Proxy(t *testing.T) {
	cfg, err := ParseFile("../testdata/service_refs.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// The proxy service references backend via target = service.backend.url
	proxy := cfg.Services[1]
	require.Equal(t, "proxy", proxy.ServiceName())
	require.Len(t, proxy.GetInferredUpstreams(), 1)
	require.Equal(t, "backend", proxy.GetInferredUpstreams()[0])

	// The backend has no upstream references
	require.Empty(t, cfg.Services[0].GetInferredUpstreams())
}

func TestParse_ServiceVars_AllAttributes(t *testing.T) {
	src := []byte(`
service "http" "my-api" {
  listen = "10.0.0.1:9090"
}
`)

	cfg, err := Parse(src, "test.hcl")
	require.NoError(t, err)

	svc := cfg.Services[0]
	require.NotNil(t, svc.GetServiceVars())

	vars := svc.GetServiceVars()["my-api"].AsValueMap()
	require.Equal(t, "10.0.0.1:9090", vars["address"].AsString())
	require.Equal(t, "10.0.0.1", vars["host"].AsString())
	require.Equal(t, "9090", vars["port"].AsString())
	require.Equal(t, "http", vars["type"].AsString())
	require.Equal(t, "http://10.0.0.1:9090", vars["url"].AsString())
}

// --- Type-specific field rejection tests ---
// With per-type config structs, gohcl rejects fields that don't belong to
// a service type at parse time (instead of validate time).

func TestParse_UnknownServiceType(t *testing.T) {
	src := []byte(`
service "grpc" "api" {
  listen = "0.0.0.0:8080"
}
`)
	cfg, err := Parse(src, "test.hcl")
	require.Error(t, err)
	require.Nil(t, cfg)
	require.Contains(t, err.Error(), "unknown type \"grpc\"")
}

func TestParse_PackageOnlyForConnect(t *testing.T) {
	for _, svcType := range []string{"http", "proxy", "tcp"} {
		t.Run(svcType, func(t *testing.T) {
			target := ""
			if svcType == "proxy" {
				target = "\n  target = \"http://localhost:8081\""
			}
			src := []byte(fmt.Sprintf(`
service "%s" "api" {
  listen  = "0.0.0.0:8080"%s
  package = "api.v1"
}
`, svcType, target))
			_, err := Parse(src, "test.hcl")
			require.Error(t, err)
			require.Contains(t, err.Error(), "package")
		})
	}
}

func TestValidate_ConnectRequiresPackage(t *testing.T) {
	cfg := &config.Config{
		Services: []config.Service{
			&connect.Service{
				Name:   "api",
				Listen: "0.0.0.0:9090",
			},
		},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "package is required for connect services")
}

func TestParse_TargetOnlyForProxy(t *testing.T) {
	src := []byte(`
service "http" "api" {
  listen = "0.0.0.0:8080"
  target = "http://example.com"
}
`)
	_, err := Parse(src, "test.hcl")
	require.Error(t, err)
	require.Contains(t, err.Error(), "target")
}

func TestParse_RequestHeadersOnlyForProxy(t *testing.T) {
	src := []byte(`
service "http" "api" {
  listen           = "0.0.0.0:8080"
  request_headers  = { "X-Test" = "val" }
}
`)
	_, err := Parse(src, "test.hcl")
	require.Error(t, err)
	require.Contains(t, err.Error(), "request_headers")
}

func TestParse_ResponseHeadersOnlyForProxy(t *testing.T) {
	src := []byte(`
service "http" "api" {
  listen            = "0.0.0.0:8080"
  response_headers  = { "X-Test" = "val" }
}
`)
	_, err := Parse(src, "test.hcl")
	require.Error(t, err)
	require.Contains(t, err.Error(), "response_headers")
}

func TestParse_PatternOnlyForTCP(t *testing.T) {
	for _, svcType := range []string{"http", "proxy"} {
		t.Run(svcType, func(t *testing.T) {
			target := ""
			if svcType == "proxy" {
				target = "\n  target = \"http://localhost:8081\""
			}
			src := []byte(fmt.Sprintf(`
service "%s" "api" {
  listen = "0.0.0.0:8080"%s
  handle "test" {
    route   = "GET /test"
    pattern = "PING*"
  }
}
`, svcType, target))
			_, err := Parse(src, "test.hcl")
			require.Error(t, err)
			require.Contains(t, err.Error(), "pattern")
		})
	}
}

func TestParse_PatternNotValidForConnect(t *testing.T) {
	src := []byte(`
service "connect" "api" {
  listen  = "0.0.0.0:9090"
  package = "api.v1"
  handle "Search" {
    pattern = "PING*"
  }
}
`)
	_, err := Parse(src, "test.hcl")
	require.Error(t, err)
	require.Contains(t, err.Error(), "pattern")
}

func TestValidate_RouteRequiredForHTTP(t *testing.T) {
	cfg := &config.Config{
		Services: []config.Service{
			&http.Service{
				Name:   "api",
				Listen: "0.0.0.0:8080",
				Handlers: []*http.Handler{
					{Name: "test"},
				},
			},
		},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires a route")
}

func TestParse_RouteNotValidForTCP(t *testing.T) {
	src := []byte(`
service "tcp" "cache" {
  listen = "0.0.0.0:6379"
  handle "ping" {
    pattern = "PING*"
    route   = "GET /ping"
  }
}
`)
	_, err := Parse(src, "test.hcl")
	require.Error(t, err)
	require.Contains(t, err.Error(), "route")
}

func TestParse_RouteNotValidForConnect(t *testing.T) {
	src := []byte(`
service "connect" "api" {
  listen  = "0.0.0.0:9090"
  package = "api.v1"
  handle "Search" {
    route = "GET /search"
  }
}
`)
	_, err := Parse(src, "test.hcl")
	require.Error(t, err)
	require.Contains(t, err.Error(), "route")
}

func TestValidate_ValidProxyService(t *testing.T) {
	src := []byte(`
service "http" "backend" {
  listen = "127.0.0.1:8081"
}

service "proxy" "proxy" {
  listen           = "0.0.0.0:8080"
  target           = service.backend.url
  request_headers  = { "X-Proxy" = "polymorph" }
  response_headers = { "X-Served-By" = "polymorph-proxy" }

  handle "health" {
    route = "GET /health"
    response {
      body = jsonencode({ status = "ok" })
    }
  }
}
`)
	cfg, err := Parse(src, "test.hcl")
	require.NoError(t, err)

	err = Validate(cfg)
	require.NoError(t, err)
}

func TestValidate_ValidTCPService(t *testing.T) {
	cfg := &config.Config{
		Services: []config.Service{
			&tcp.Service{
				Name:   "cache",
				Listen: "0.0.0.0:6379",
				Handlers: []*tcp.Handler{
					{Name: "ping", Pattern: "PING*"},
					{Name: "default"},
				},
			},
		},
	}
	err := Validate(cfg)
	require.NoError(t, err)
}

func TestValidate_ValidConnectService(t *testing.T) {
	cfg := &config.Config{
		Services: []config.Service{
			&connect.Service{
				Name:    "api",
				Listen:  "0.0.0.0:9090",
				Package: "api.v1",
				Handlers: []*connect.Handler{
					{Name: "SearchUsers"},
				},
			},
		},
	}
	err := Validate(cfg)
	require.NoError(t, err)
}

// --- Observability config tests ---

func TestParse_LoggingConfig(t *testing.T) {
	src := []byte(`
logging {
  level  = "debug"
  format = "json"
  output = "/var/log/app.log"
}

service "http" "api" {
  listen = "0.0.0.0:8080"
}
`)
	cfg, err := Parse(src, "test.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg.Logging)
	require.Equal(t, "debug", *cfg.Logging.Level)
	require.Equal(t, "json", *cfg.Logging.Format)
	require.Equal(t, "/var/log/app.log", *cfg.Logging.Output)
}

func TestParse_TracingConfig(t *testing.T) {
	src := []byte(`
tracing {
  enabled  = true
  endpoint = "otel:4318"
  sampler  = "ratio"
  ratio    = 0.5
}

service "http" "api" {
  listen = "0.0.0.0:8080"
}
`)
	cfg, err := Parse(src, "test.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg.Tracing)
	require.True(t, *cfg.Tracing.Enabled)
	require.Equal(t, "otel:4318", *cfg.Tracing.Endpoint)
	require.Equal(t, "ratio", *cfg.Tracing.Sampler)
	require.InDelta(t, 0.5, *cfg.Tracing.Ratio, 0.001)
}

func TestParse_MetricsConfig(t *testing.T) {
	src := []byte(`
metrics {
  enabled = false
  path    = "/-/metrics"
}

service "http" "api" {
  listen = "0.0.0.0:8080"
}
`)
	cfg, err := Parse(src, "test.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg.Metrics)
	require.False(t, *cfg.Metrics.Enabled)
	require.Equal(t, "/-/metrics", *cfg.Metrics.Path)
}

func TestParse_ServiceLoggingOverride(t *testing.T) {
	src := []byte(`
logging {
  level  = "info"
  format = "text"
}

service "http" "noisy" {
  listen = "0.0.0.0:8080"

  logging {
    level  = "warn"
    output = "/var/log/noisy.log"
  }
}
`)
	cfg, err := Parse(src, "test.hcl")
	require.NoError(t, err)

	// Global logging
	require.NotNil(t, cfg.Logging)
	require.Equal(t, "info", *cfg.Logging.Level)

	// Per-service logging override
	svc := cfg.Services[0]
	require.NotNil(t, svc.ServiceLogging())
	require.Equal(t, "warn", *svc.ServiceLogging().Level)
	require.Equal(t, "/var/log/noisy.log", *svc.ServiceLogging().Output)
	// Format not overridden, should be nil
	require.Nil(t, svc.ServiceLogging().Format)
}

func TestParse_ObservabilityDefaults(t *testing.T) {
	src := []byte(`
service "http" "api" {
  listen = "0.0.0.0:8080"
}
`)
	cfg, err := Parse(src, "test.hcl")
	require.NoError(t, err)
	require.Nil(t, cfg.Logging)
	require.Nil(t, cfg.Tracing)
	require.Nil(t, cfg.Metrics)
	require.Nil(t, cfg.Services[0].ServiceLogging())
}

func TestValidate_LoggingLevel_Invalid(t *testing.T) {
	level := "trace"
	cfg := &config.Config{
		Logging: &config.LoggingConfig{Level: &level},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid logging level")
}

func TestValidate_LoggingFormat_Invalid(t *testing.T) {
	format := "yaml"
	cfg := &config.Config{
		Logging: &config.LoggingConfig{Format: &format},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid logging format")
}

func TestValidate_LoggingOutput_Empty(t *testing.T) {
	output := ""
	cfg := &config.Config{
		Logging: &config.LoggingConfig{Output: &output},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "logging output must be stdout, stderr, or a non-empty file path")
}

func TestValidate_TracingSampler_Invalid(t *testing.T) {
	sampler := "random"
	cfg := &config.Config{
		Tracing: &config.TracingConfig{Sampler: &sampler},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid sampler")
}

func TestValidate_TracingRatio_OutOfRange(t *testing.T) {
	ratio := 1.5
	sampler := "ratio"
	cfg := &config.Config{
		Tracing: &config.TracingConfig{Sampler: &sampler, Ratio: &ratio},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ratio must be between 0.0 and 1.0")
}

func TestValidate_TracingRatio_RequiredForRatioSampler(t *testing.T) {
	sampler := "ratio"
	cfg := &config.Config{
		Tracing: &config.TracingConfig{Sampler: &sampler},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ratio is required when sampler is \"ratio\"")
}

func TestValidate_MetricsPath_NoSlash(t *testing.T) {
	path := "metrics"
	cfg := &config.Config{
		Metrics: &config.MetricsConfig{Path: &path},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "path must start with /")
}

func TestValidate_ServiceLogging_InvalidLevel(t *testing.T) {
	level := "verbose"
	cfg := &config.Config{
		Services: []config.Service{
			&http.Service{
				Name:    "api",
				Listen:  "0.0.0.0:8080",
				Logging: &config.LoggingConfig{Level: &level},
			},
		},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "service \"api\" logging")
	require.Contains(t, err.Error(), "invalid logging level")
}

func TestValidate_ObservabilityValid(t *testing.T) {
	level := "info"
	format := "json"
	output := "stderr"
	enabled := true
	sampler := "ratio"
	ratio := 0.1
	path := "/metrics"
	cfg := &config.Config{
		Logging: &config.LoggingConfig{Level: &level, Format: &format, Output: &output},
		Tracing: &config.TracingConfig{Enabled: &enabled, Sampler: &sampler, Ratio: &ratio},
		Metrics: &config.MetricsConfig{Enabled: &enabled, Path: &path},
		Services: []config.Service{
			&http.Service{Name: "api", Listen: "0.0.0.0:8080"},
		},
	}
	err := Validate(cfg)
	require.NoError(t, err)
}

func TestParseFile_Directory(t *testing.T) {
	dir := t.TempDir()

	// File 1: logging block
	err := os.WriteFile(filepath.Join(dir, "01-logging.hcl"), []byte(`
logging {
  level  = "debug"
  format = "json"
}
`), 0644)
	require.NoError(t, err)

	// File 2: service block
	err = os.WriteFile(filepath.Join(dir, "02-service.hcl"), []byte(`
service "http" "api" {
  listen = "0.0.0.0:8080"
}
`), 0644)
	require.NoError(t, err)

	cfg, err := ParseFile(dir)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Logging from file 1
	require.NotNil(t, cfg.Logging)
	require.Equal(t, "debug", *cfg.Logging.Level)
	require.Equal(t, "json", *cfg.Logging.Format)

	// Service from file 2
	require.Len(t, cfg.Services, 1)
	require.Equal(t, "api", cfg.Services[0].ServiceName())
	require.Equal(t, "0.0.0.0:8080", cfg.Services[0].ServiceListen())
}

func TestParseFile_DirectoryEmpty(t *testing.T) {
	dir := t.TempDir()

	cfg, err := ParseFile(dir)
	require.Error(t, err)
	require.Nil(t, cfg)
	require.Contains(t, err.Error(), "no .hcl files found in directory")
}

func TestParseFile_DirectoryCrossFileRefs(t *testing.T) {
	dir := t.TempDir()

	// File 1: backend service
	err := os.WriteFile(filepath.Join(dir, "01-backend.hcl"), []byte(`
service "http" "backend" {
  listen = "127.0.0.1:8081"
}
`), 0644)
	require.NoError(t, err)

	// File 2: proxy referencing backend via service.backend.url
	err = os.WriteFile(filepath.Join(dir, "02-proxy.hcl"), []byte(`
service "proxy" "gateway" {
  listen = "0.0.0.0:8080"
  target = service.backend.url
}
`), 0644)
	require.NoError(t, err)

	cfg, err := ParseFile(dir)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Services, 2)

	// Verify backend
	require.Equal(t, "backend", cfg.Services[0].ServiceName())

	// Verify proxy and its target resolves cross-file
	gw := cfg.Services[1]
	require.Equal(t, "gateway", gw.ServiceName())

	proxyCfg := gw.(*proxy.Service)
	require.NotNil(t, proxyCfg.TargetExpr)
	evalCtx := &hcl.EvalContext{
		Functions: config.Functions(),
		Variables: map[string]cty.Value{
			"service": cty.ObjectVal(gw.GetServiceVars()),
		},
	}
	targetVal, diags := proxyCfg.TargetExpr.Value(evalCtx)
	require.False(t, diags.HasErrors())
	require.Equal(t, "http://127.0.0.1:8081", targetVal.AsString())

	// Verify inferred upstreams
	require.Len(t, gw.GetInferredUpstreams(), 1)
	require.Equal(t, "backend", gw.GetInferredUpstreams()[0])
}

// TestMain ensures tests run from the correct directory
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
