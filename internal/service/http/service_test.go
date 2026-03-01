package http

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/jumppad-labs/polymorph/internal/config"
	confighttp "github.com/jumppad-labs/polymorph/internal/config/http"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPService(t *testing.T) {
	cfg := &confighttp.Service{
		Name:   "test",
		Listen: "127.0.0.1:0",
		Handlers: []*confighttp.Handler{
			{
				Name:  "hello",
				Route: "GET /hello",
			},
		},
	}

	svc, err := NewHTTPService(cfg, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.Equal(t, "test", svc.Name())
	require.Equal(t, "http", svc.Type())
}

func TestNewHTTPService_InvalidHandler(t *testing.T) {
	cfg := &confighttp.Service{
		Name:   "test",
		Listen: "127.0.0.1:0",
		Handlers: []*confighttp.Handler{
			{
				Name:  "invalid",
				Route: "invalid route",
			},
		},
	}

	svc, err := NewHTTPService(cfg, slog.Default())
	require.Error(t, err)
	require.Nil(t, svc)
}

func TestHTTPService_StartStop(t *testing.T) {
	cfg := &confighttp.Service{
		Name:   "test",
		Listen: "127.0.0.1:0",
	}

	svc, err := NewHTTPService(cfg, slog.Default())
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
	cfg := &confighttp.Service{
		Name:   "test",
		Listen: "127.0.0.1:0",
		Handlers: []*confighttp.Handler{
			{
				Name:  "hello",
				Route: "GET /hello",
				Response: &config.ResponseConfig{
					Status:      &status,
					BodyExpr:    makeExpr(`jsonencode({ message = "Hello from Polymorph!" })`),
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

	svc, err := NewHTTPService(cfg, slog.Default())
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
		require.JSONEq(t, `{"message":"Hello from Polymorph!"}`, string(body))
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
	cfg := &confighttp.Service{
		Name:   "test",
		Listen: "127.0.0.1:0",
		Handlers: []*confighttp.Handler{
			{
				Name:     "empty",
				Route:    "GET /empty",
				Response: nil,
			},
		},
	}

	svc, err := NewHTTPService(cfg, slog.Default())
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

func TestHTTPService_StaticFiles(t *testing.T) {
	// Create a temp directory with test files
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "css"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "css", "style.css"), []byte("body{}"), 0644))

	cfg := &confighttp.Service{
		Name:   "static-test",
		Listen: "127.0.0.1:0",
		Static: &config.StaticConfig{
			Root: dir,
		},
	}

	svc, err := NewHTTPService(cfg, slog.Default())
	require.NoError(t, err)

	ctx := context.Background()
	err = svc.Start(ctx)
	require.NoError(t, err)
	defer svc.Stop(ctx)

	time.Sleep(10 * time.Millisecond)

	baseURL := "http://" + svc.listener.Addr().String()

	t.Run("serves file at root", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/index.html")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "<h1>hello</h1>", string(body))
	})

	t.Run("serves nested file", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/css/style.css")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "body{}", string(body))
	})

	t.Run("404 for missing file", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/nope.txt")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestHTTPService_StaticFilesWithPrefix(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('hi')"), 0644))

	cfg := &confighttp.Service{
		Name:   "static-prefix-test",
		Listen: "127.0.0.1:0",
		Static: &config.StaticConfig{
			Route: "/assets",
			Root:  dir,
		},
	}

	svc, err := NewHTTPService(cfg, slog.Default())
	require.NoError(t, err)

	ctx := context.Background()
	err = svc.Start(ctx)
	require.NoError(t, err)
	defer svc.Stop(ctx)

	time.Sleep(10 * time.Millisecond)

	baseURL := "http://" + svc.listener.Addr().String()

	t.Run("serves file under prefix", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/assets/app.js")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "console.log('hi')", string(body))
	})

	t.Run("404 outside prefix", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/app.js")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}
