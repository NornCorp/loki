package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestParseFile_MinimalHTTPService(t *testing.T) {
	cfg, err := ParseFile("testdata/minimal.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.Len(t, cfg.Services, 1)
	svc := cfg.Services[0]
	require.Equal(t, "api", svc.Name)
	require.Equal(t, "http", svc.Type)
	require.Equal(t, "0.0.0.0:8080", svc.Listen)
	require.Empty(t, svc.Handlers)
}

func TestParseFile_WithHandlers(t *testing.T) {
	cfg, err := ParseFile("testdata/with_handlers.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.Len(t, cfg.Services, 1)
	svc := cfg.Services[0]
	require.Equal(t, "api", svc.Name)
	require.Len(t, svc.Handlers, 2)

	// Check hello handler
	hello := svc.Handlers[0]
	require.Equal(t, "hello", hello.Name)
	require.Equal(t, "GET /hello", hello.Route)
	require.NotNil(t, hello.Response)
	require.NotNil(t, hello.Response.BodyExpr)

	// Evaluate the body expression
	evalCtx := &hcl.EvalContext{Functions: Functions()}
	value, diags := hello.Response.BodyExpr.Value(evalCtx)
	require.False(t, diags.HasErrors())
	bodyStr := value.AsString()
	require.Contains(t, bodyStr, "Hello from Loki!")

	// Verify JSON is valid
	var parsed map[string]interface{}
	err = json.Unmarshal([]byte(bodyStr), &parsed)
	require.NoError(t, err)
	require.Equal(t, "Hello from Loki!", parsed["message"])

	// Check health handler
	health := svc.Handlers[1]
	require.Equal(t, "health", health.Name)
	require.Equal(t, "GET /health", health.Route)
	require.NotNil(t, health.Response)
	require.NotNil(t, health.Response.BodyExpr)

	value, diags = health.Response.BodyExpr.Value(evalCtx)
	require.False(t, diags.HasErrors())
	require.Contains(t, value.AsString(), "healthy")
}

func TestParseFile_CustomFunctions(t *testing.T) {
	cfg, err := ParseFile("testdata/functions.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.Len(t, cfg.Services, 1)
	svc := cfg.Services[0]
	require.Len(t, svc.Handlers, 1)

	handler := svc.Handlers[0]
	require.NotNil(t, handler.Response)
	require.NotNil(t, handler.Response.BodyExpr)

	// Evaluate the body expression
	evalCtx := &hcl.EvalContext{Functions: Functions()}
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
	cfg, err := ParseFile("testdata/invalid_syntax.hcl")
	require.Error(t, err)
	require.Nil(t, cfg)
	require.Contains(t, err.Error(), "failed to parse config")
}

func TestParseFile_FileNotFound(t *testing.T) {
	cfg, err := ParseFile("testdata/nonexistent.hcl")
	require.Error(t, err)
	require.Nil(t, cfg)
	require.Contains(t, err.Error(), "failed to read config file")
}

func TestParseFile_WithHeimdall(t *testing.T) {
	cfg, err := ParseFile("testdata/with_heimdall.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.NotNil(t, cfg.Heimdall)
	require.Equal(t, "heimdall:7946", cfg.Heimdall.Address)

	require.Len(t, cfg.Services, 1)
	svc := cfg.Services[0]
	require.Equal(t, "api", svc.Name)
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg, err := ParseFile("testdata/minimal.hcl")
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
	cfg := &Config{
		Services: []*ServiceConfig{
			{
				Name: "api",
				Type: "http",
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
	require.Equal(t, "test", cfg.Services[0].Name)
	require.Equal(t, "0.0.0.0:9000", cfg.Services[0].Listen)
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
			require.Len(t, cfg.Services[0].Handlers, 1)

			// Evaluate the body expression
			evalCtx := &hcl.EvalContext{Functions: Functions()}
			value, diags := cfg.Services[0].Handlers[0].Response.BodyExpr.Value(evalCtx)
			require.False(t, diags.HasErrors())
			require.Equal(t, tt.expected, value.AsString())
		})
	}
}

func TestParse_ServiceReferences(t *testing.T) {
	cfg, err := ParseFile("testdata/service_refs.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Services, 2)

	// Verify service vars are populated on all services
	backend := cfg.Services[0]
	require.Equal(t, "backend", backend.Name)
	require.NotNil(t, backend.ServiceVars)
	require.Contains(t, backend.ServiceVars, "backend")
	require.Contains(t, backend.ServiceVars, "proxy")

	proxy := cfg.Services[1]
	require.Equal(t, "proxy", proxy.Name)

	// Verify the service.backend.* vars resolve correctly
	backendVars := backend.ServiceVars["backend"]
	backendMap := backendVars.AsValueMap()
	require.Equal(t, "http://127.0.0.1:8081", backendMap["url"].AsString())
	require.Equal(t, "127.0.0.1:8081", backendMap["address"].AsString())
	require.Equal(t, "127.0.0.1", backendMap["host"].AsString())
	require.Equal(t, "8081", backendMap["port"].AsString())
	require.Equal(t, "http", backendMap["type"].AsString())

	// Verify proxy's target expression can be evaluated with service vars
	require.NotNil(t, proxy.TargetExpr)
	evalCtx := &hcl.EvalContext{
		Functions: Functions(),
		Variables: map[string]cty.Value{
			"service": cty.ObjectVal(proxy.ServiceVars),
		},
	}
	targetVal, diags := proxy.TargetExpr.Value(evalCtx)
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
	cfg, err := ParseFile("testdata/gateway_refs.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Services, 3)

	// The gateway references user-service and order-service via step URLs
	gateway := cfg.Services[2]
	require.Equal(t, "api-gateway", gateway.Name)
	require.Len(t, gateway.InferredUpstreams, 2)
	require.Contains(t, gateway.InferredUpstreams, "user-service")
	require.Contains(t, gateway.InferredUpstreams, "order-service")

	// The upstream services should have no inferred upstreams
	require.Empty(t, cfg.Services[0].InferredUpstreams)
	require.Empty(t, cfg.Services[1].InferredUpstreams)
}

func TestParse_InferUpstreams_Proxy(t *testing.T) {
	cfg, err := ParseFile("testdata/service_refs.hcl")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// The proxy service references backend via target = service.backend.url
	proxy := cfg.Services[1]
	require.Equal(t, "proxy", proxy.Name)
	require.Len(t, proxy.InferredUpstreams, 1)
	require.Equal(t, "backend", proxy.InferredUpstreams[0])

	// The backend has no upstream references
	require.Empty(t, cfg.Services[0].InferredUpstreams)
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
	require.NotNil(t, svc.ServiceVars)

	vars := svc.ServiceVars["my-api"].AsValueMap()
	require.Equal(t, "10.0.0.1:9090", vars["address"].AsString())
	require.Equal(t, "10.0.0.1", vars["host"].AsString())
	require.Equal(t, "9090", vars["port"].AsString())
	require.Equal(t, "http", vars["type"].AsString())
	require.Equal(t, "http://10.0.0.1:9090", vars["url"].AsString())
}

func TestValidate_UnknownServiceType(t *testing.T) {
	cfg := &Config{
		Services: []*ServiceConfig{
			{Name: "api", Type: "grpc", Listen: "0.0.0.0:8080"},
		},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown type \"grpc\"")
}

func TestValidate_PackageOnlyForConnect(t *testing.T) {
	for _, svcType := range []string{"http", "proxy", "tcp"} {
		t.Run(svcType, func(t *testing.T) {
			cfg := &Config{
				Services: []*ServiceConfig{
					{Name: "api", Type: svcType, Listen: "0.0.0.0:8080", Package: "api.v1"},
				},
			}
			err := Validate(cfg)
			require.Error(t, err)
			require.Contains(t, err.Error(), "\"package\" is only valid for connect services")
		})
	}
}

func TestValidate_ConnectRequiresPackage(t *testing.T) {
	cfg := &Config{
		Services: []*ServiceConfig{
			{Name: "api", Type: "connect", Listen: "0.0.0.0:9090"},
		},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "\"package\" is required for connect services")
}

func TestValidate_TargetOnlyForProxy(t *testing.T) {
	// Parse a real HCL config to get a non-nil TargetExpr on an http service
	src := []byte(`
service "http" "api" {
  listen = "0.0.0.0:8080"
  target = "http://example.com"
}
`)
	cfg, err := Parse(src, "test.hcl")
	require.NoError(t, err)

	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "\"target\" is only valid for proxy services")
}

func TestValidate_RequestHeadersOnlyForProxy(t *testing.T) {
	src := []byte(`
service "http" "api" {
  listen           = "0.0.0.0:8080"
  request_headers  = { "X-Test" = "val" }
}
`)
	cfg, err := Parse(src, "test.hcl")
	require.NoError(t, err)

	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "\"request_headers\" is only valid for proxy services")
}

func TestValidate_ResponseHeadersOnlyForProxy(t *testing.T) {
	src := []byte(`
service "http" "api" {
  listen            = "0.0.0.0:8080"
  response_headers  = { "X-Test" = "val" }
}
`)
	cfg, err := Parse(src, "test.hcl")
	require.NoError(t, err)

	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "\"response_headers\" is only valid for proxy services")
}

func TestValidate_PatternOnlyForTCP(t *testing.T) {
	for _, svcType := range []string{"http", "proxy"} {
		t.Run(svcType, func(t *testing.T) {
			cfg := &Config{
				Services: []*ServiceConfig{
					{
						Name: "api", Type: svcType, Listen: "0.0.0.0:8080",
						Handlers: []*HandlerConfig{
							{Name: "test", Route: "GET /test", Pattern: "PING*"},
						},
					},
				},
			}
			err := Validate(cfg)
			require.Error(t, err)
			require.Contains(t, err.Error(), "\"pattern\" is only valid for tcp services")
		})
	}
}

func TestValidate_PatternNotValidForConnect(t *testing.T) {
	cfg := &Config{
		Services: []*ServiceConfig{
			{
				Name: "api", Type: "connect", Listen: "0.0.0.0:9090", Package: "api.v1",
				Handlers: []*HandlerConfig{
					{Name: "Search", Pattern: "PING*"},
				},
			},
		},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "\"pattern\" is only valid for tcp services")
}

func TestValidate_RouteRequiredForHTTP(t *testing.T) {
	cfg := &Config{
		Services: []*ServiceConfig{
			{
				Name: "api", Type: "http", Listen: "0.0.0.0:8080",
				Handlers: []*HandlerConfig{
					{Name: "test"},
				},
			},
		},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "\"route\" is required for http services")
}

func TestValidate_RouteNotValidForTCP(t *testing.T) {
	cfg := &Config{
		Services: []*ServiceConfig{
			{
				Name: "cache", Type: "tcp", Listen: "0.0.0.0:6379",
				Handlers: []*HandlerConfig{
					{Name: "ping", Pattern: "PING*", Route: "GET /ping"},
				},
			},
		},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "\"route\" is not valid for tcp services")
}

func TestValidate_RouteNotValidForConnect(t *testing.T) {
	cfg := &Config{
		Services: []*ServiceConfig{
			{
				Name: "api", Type: "connect", Listen: "0.0.0.0:9090", Package: "api.v1",
				Handlers: []*HandlerConfig{
					{Name: "Search", Route: "GET /search"},
				},
			},
		},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "\"route\" is not valid for connect services")
}

func TestValidate_ValidProxyService(t *testing.T) {
	src := []byte(`
service "http" "backend" {
  listen = "127.0.0.1:8081"
}

service "proxy" "proxy" {
  listen           = "0.0.0.0:8080"
  target           = service.backend.url
  request_headers  = { "X-Proxy" = "loki" }
  response_headers = { "X-Served-By" = "loki-proxy" }

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
	cfg := &Config{
		Services: []*ServiceConfig{
			{
				Name: "cache", Type: "tcp", Listen: "0.0.0.0:6379",
				Handlers: []*HandlerConfig{
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
	cfg := &Config{
		Services: []*ServiceConfig{
			{
				Name: "api", Type: "connect", Listen: "0.0.0.0:9090", Package: "api.v1",
				Handlers: []*HandlerConfig{
					{Name: "SearchUsers"},
				},
			},
		},
	}
	err := Validate(cfg)
	require.NoError(t, err)
}

// --- Observability config tests (Phase 1) ---

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
	require.NotNil(t, svc.Logging)
	require.Equal(t, "warn", *svc.Logging.Level)
	require.Equal(t, "/var/log/noisy.log", *svc.Logging.Output)
	// Format not overridden, should be nil
	require.Nil(t, svc.Logging.Format)
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
	require.Nil(t, cfg.Services[0].Logging)
}

func TestValidate_LoggingLevel_Invalid(t *testing.T) {
	level := "trace"
	cfg := &Config{
		Logging: &LoggingConfig{Level: &level},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid logging level")
}

func TestValidate_LoggingFormat_Invalid(t *testing.T) {
	format := "yaml"
	cfg := &Config{
		Logging: &LoggingConfig{Format: &format},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid logging format")
}

func TestValidate_LoggingOutput_Empty(t *testing.T) {
	output := ""
	cfg := &Config{
		Logging: &LoggingConfig{Output: &output},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "logging output must be stdout, stderr, or a non-empty file path")
}

func TestValidate_TracingSampler_Invalid(t *testing.T) {
	sampler := "random"
	cfg := &Config{
		Tracing: &TracingConfig{Sampler: &sampler},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid sampler")
}

func TestValidate_TracingRatio_OutOfRange(t *testing.T) {
	ratio := 1.5
	sampler := "ratio"
	cfg := &Config{
		Tracing: &TracingConfig{Sampler: &sampler, Ratio: &ratio},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ratio must be between 0.0 and 1.0")
}

func TestValidate_TracingRatio_RequiredForRatioSampler(t *testing.T) {
	sampler := "ratio"
	cfg := &Config{
		Tracing: &TracingConfig{Sampler: &sampler},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ratio is required when sampler is \"ratio\"")
}

func TestValidate_MetricsPath_NoSlash(t *testing.T) {
	path := "metrics"
	cfg := &Config{
		Metrics: &MetricsConfig{Path: &path},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "path must start with /")
}

func TestValidate_ServiceLogging_InvalidLevel(t *testing.T) {
	level := "verbose"
	cfg := &Config{
		Services: []*ServiceConfig{
			{
				Name:    "api",
				Type:    "http",
				Listen:  "0.0.0.0:8080",
				Logging: &LoggingConfig{Level: &level},
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
	cfg := &Config{
		Logging: &LoggingConfig{Level: &level, Format: &format, Output: &output},
		Tracing: &TracingConfig{Enabled: &enabled, Sampler: &sampler, Ratio: &ratio},
		Metrics: &MetricsConfig{Enabled: &enabled, Path: &path},
		Services: []*ServiceConfig{
			{Name: "api", Type: "http", Listen: "0.0.0.0:8080"},
		},
	}
	err := Validate(cfg)
	require.NoError(t, err)
}

// TestMain ensures tests run from the correct directory
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
