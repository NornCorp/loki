package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/require"
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

func TestValidate_MissingType(t *testing.T) {
	cfg := &Config{
		Services: []*ServiceConfig{
			{
				Name:   "api",
				Listen: "0.0.0.0:8080",
			},
		},
	}

	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "type is required")
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
service "test" {
  type   = "http"
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
service "test" {
  type   = "http"
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

// TestMain ensures tests run from the correct directory
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
