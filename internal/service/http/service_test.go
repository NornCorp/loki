package http

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/norncorp/loki/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPService(t *testing.T) {
	cfg := &config.ServiceConfig{
		Name:   "test",
		Type:   "http",
		Listen: "127.0.0.1:0",
		Handlers: []*config.HandlerConfig{
			{
				Name:  "hello",
				Route: "GET /hello",
			},
		},
	}

	svc, err := NewHTTPService(cfg)
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.Equal(t, "test", svc.Name())
	require.Equal(t, "http", svc.Type())
}

func TestNewHTTPService_InvalidHandler(t *testing.T) {
	cfg := &config.ServiceConfig{
		Name:   "test",
		Type:   "http",
		Listen: "127.0.0.1:0",
		Handlers: []*config.HandlerConfig{
			{
				Name:  "invalid",
				Route: "invalid route",
			},
		},
	}

	svc, err := NewHTTPService(cfg)
	require.Error(t, err)
	require.Nil(t, svc)
}

func TestHTTPService_StartStop(t *testing.T) {
	cfg := &config.ServiceConfig{
		Name:   "test",
		Type:   "http",
		Listen: "127.0.0.1:0",
	}

	svc, err := NewHTTPService(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	// Start service
	err = svc.Start(ctx)
	require.NoError(t, err)
	require.NotNil(t, svc.server)
	require.NotNil(t, svc.listener)

	// Give server time to start
	time.Sleep(10 * time.Millisecond)

	// Stop service
	err = svc.Stop(ctx)
	require.NoError(t, err)
}

func TestHTTPService_ServeHTTP(t *testing.T) {
	// Helper to create expression from string
	makeExpr := func(s string) hcl.Expression {
		expr, diags := hclsyntax.ParseExpression([]byte(s), "test", hcl.Pos{})
		require.False(t, diags.HasErrors())
		return expr
	}

	status := http.StatusCreated
	cfg := &config.ServiceConfig{
		Name:   "test",
		Type:   "http",
		Listen: "127.0.0.1:0",
		Handlers: []*config.HandlerConfig{
			{
				Name:  "hello",
				Route: "GET /hello",
				Response: &config.ResponseConfig{
					Status:      &status,
					BodyExpr:    makeExpr(`jsonencode({ message = "Hello from Loki!" })`),
					HeadersExpr: makeExpr(`{ "X-Custom-Header" = "test-value" }`),
				},
			},
			{
				Name:  "health",
				Route: "GET /health",
				Response: &config.ResponseConfig{
					BodyExpr: makeExpr(`jsonencode({ status = "healthy" })`),
				},
			},
		},
	}

	svc, err := NewHTTPService(cfg)
	require.NoError(t, err)

	ctx := context.Background()
	err = svc.Start(ctx)
	require.NoError(t, err)
	defer svc.Stop(ctx)

	// Give server time to start
	time.Sleep(10 * time.Millisecond)

	// Get the actual listen address
	addr := svc.listener.Addr().String()
	baseURL := "http://" + addr

	// Test hello endpoint
	t.Run("GET /hello", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/hello")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusCreated, resp.StatusCode)
		require.Equal(t, "test-value", resp.Header.Get("X-Custom-Header"))
		require.Equal(t, "application/json", resp.Header.Get("Content-Type"))

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"message":"Hello from Loki!"}`, string(body))
	})

	// Test health endpoint
	t.Run("GET /health", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/health")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, "application/json", resp.Header.Get("Content-Type"))

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"status":"healthy"}`, string(body))
	})

	// Test 404
	t.Run("GET /notfound returns 404", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/notfound")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusNotFound, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"error":"not found"}`, string(body))
	})

	// Test wrong method
	t.Run("POST /hello returns 404", func(t *testing.T) {
		resp, err := http.Post(baseURL+"/hello", "application/json", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestHTTPService_EmptyResponse(t *testing.T) {
	cfg := &config.ServiceConfig{
		Name:   "test",
		Type:   "http",
		Listen: "127.0.0.1:0",
		Handlers: []*config.HandlerConfig{
			{
				Name:     "empty",
				Route:    "GET /empty",
				Response: nil,
			},
		},
	}

	svc, err := NewHTTPService(cfg)
	require.NoError(t, err)

	ctx := context.Background()
	err = svc.Start(ctx)
	require.NoError(t, err)
	defer svc.Stop(ctx)

	time.Sleep(10 * time.Millisecond)

	addr := svc.listener.Addr().String()
	resp, err := http.Get("http://" + addr + "/empty")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Empty(t, body)
}
