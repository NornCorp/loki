package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/norncorp/loki/internal/config"
	confighttp "github.com/norncorp/loki/internal/config/http"
	"github.com/stretchr/testify/require"
)

func TestNewSpecHandler_ValidSpec(t *testing.T) {
	cfg := &config.SpecConfig{
		Path: "testdata/petstore.yaml",
	}

	sh, err := NewSpecHandler(cfg, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, sh)
	require.NotEmpty(t, sh.routes)

	// Should have routes for: GET /pets, POST /pets, GET /pets/:petId, PUT /pets/:petId, DELETE /pets/:petId
	require.GreaterOrEqual(t, len(sh.routes), 5)

	// Verify route methods and paths
	routeMap := make(map[string]string)
	for _, r := range sh.routes {
		routeMap[r.method+" "+r.path] = r.method
	}
	require.Contains(t, routeMap, "GET /pets")
	require.Contains(t, routeMap, "POST /pets")
	require.Contains(t, routeMap, "GET /pets/:petId")
	require.Contains(t, routeMap, "PUT /pets/:petId")
	require.Contains(t, routeMap, "DELETE /pets/:petId")
}

func TestNewSpecHandler_InvalidPath(t *testing.T) {
	cfg := &config.SpecConfig{
		Path: "testdata/nonexistent.yaml",
	}

	sh, err := NewSpecHandler(cfg, slog.Default())
	require.Error(t, err)
	require.Nil(t, sh)
	require.Contains(t, err.Error(), "failed to read spec file")
}

func TestNewSpecHandler_InvalidSpec(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte("not: valid: openapi: spec: [[["), 0644))

	cfg := &config.SpecConfig{
		Path: specPath,
	}

	sh, err := NewSpecHandler(cfg, slog.Default())
	require.Error(t, err)
	require.Nil(t, sh)
}

func TestNewSpecHandler_JSONSpec(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	specJSON := `{
		"openapi": "3.0.3",
		"info": {"title": "Test", "version": "1.0.0"},
		"paths": {
			"/health": {
				"get": {
					"responses": {
						"200": {
							"description": "OK",
							"content": {
								"application/json": {
									"schema": {
										"type": "object",
										"properties": {
											"status": {"type": "string"}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}`
	require.NoError(t, os.WriteFile(specPath, []byte(specJSON), 0644))

	cfg := &config.SpecConfig{
		Path: specPath,
	}

	sh, err := NewSpecHandler(cfg, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, sh)
	require.Len(t, sh.routes, 1)
	require.Equal(t, "GET", sh.routes[0].method)
	require.Equal(t, "/health", sh.routes[0].path)
}

func TestSpecHandler_Match(t *testing.T) {
	cfg := &config.SpecConfig{
		Path: "testdata/petstore.yaml",
	}

	sh, err := NewSpecHandler(cfg, slog.Default())
	require.NoError(t, err)

	tests := []struct {
		name        string
		method      string
		path        string
		shouldMatch bool
		wantStatus  int
	}{
		{
			name:        "exact match GET /pets",
			method:      "GET",
			path:        "/pets",
			shouldMatch: true,
			wantStatus:  200,
		},
		{
			name:        "exact match POST /pets",
			method:      "POST",
			path:        "/pets",
			shouldMatch: true,
			wantStatus:  201,
		},
		{
			name:        "param match GET /pets/:petId",
			method:      "GET",
			path:        "/pets/abc-123",
			shouldMatch: true,
			wantStatus:  200,
		},
		{
			name:        "param match DELETE /pets/:petId",
			method:      "DELETE",
			path:        "/pets/abc-123",
			shouldMatch: true,
			wantStatus:  204,
		},
		{
			name:        "wrong method",
			method:      "PATCH",
			path:        "/pets",
			shouldMatch: false,
		},
		{
			name:        "non-existent path",
			method:      "GET",
			path:        "/users",
			shouldMatch: false,
		},
		{
			name:        "too many segments",
			method:      "GET",
			path:        "/pets/123/extra",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, ok := sh.Match(tt.method, tt.path)
			require.Equal(t, tt.shouldMatch, ok)
			if tt.shouldMatch {
				require.NotNil(t, route)
				require.Equal(t, tt.wantStatus, route.status)
			}
		})
	}
}

func TestSpecHandler_ArrayResponse(t *testing.T) {
	rows := 5
	seed := int64(42)
	cfg := &config.SpecConfig{
		Path: "testdata/petstore.yaml",
		Rows: &rows,
		Seed: &seed,
	}

	sh, err := NewSpecHandler(cfg, slog.Default())
	require.NoError(t, err)

	route, ok := sh.Match("GET", "/pets")
	require.True(t, ok)
	require.NotNil(t, route.response)

	// Response should be a JSON array
	var items []json.RawMessage
	err = json.Unmarshal(route.response, &items)
	require.NoError(t, err)
	require.Len(t, items, rows)

	// Each item should be valid JSON with expected fields
	for _, item := range items {
		var obj map[string]any
		err = json.Unmarshal(item, &obj)
		require.NoError(t, err)
		require.Contains(t, obj, "id")
		require.Contains(t, obj, "name")
	}
}

func TestSpecHandler_NoSchema(t *testing.T) {
	cfg := &config.SpecConfig{
		Path: "testdata/petstore.yaml",
	}

	sh, err := NewSpecHandler(cfg, slog.Default())
	require.NoError(t, err)

	// DELETE /pets/:petId has a 204 No Content response with no schema
	route, ok := sh.Match("DELETE", "/pets/123")
	require.True(t, ok)
	require.Equal(t, 204, route.status)
	require.Nil(t, route.response)
}

func TestSpecHandler_PathParamConversion(t *testing.T) {
	cfg := &config.SpecConfig{
		Path: "testdata/petstore.yaml",
	}

	sh, err := NewSpecHandler(cfg, slog.Default())
	require.NoError(t, err)

	// Verify {petId} was converted to :petId
	found := false
	for _, r := range sh.routes {
		if r.path == "/pets/:petId" {
			found = true
			break
		}
	}
	require.True(t, found, "expected path /pets/:petId with converted params")
}

func TestSpecHandler_SeededDeterminism(t *testing.T) {
	seed := int64(99)
	cfg := &config.SpecConfig{
		Path: "testdata/petstore.yaml",
		Seed: &seed,
	}

	sh1, err := NewSpecHandler(cfg, slog.Default())
	require.NoError(t, err)

	sh2, err := NewSpecHandler(cfg, slog.Default())
	require.NoError(t, err)

	// Same seed should produce same route structure
	require.Equal(t, len(sh1.routes), len(sh2.routes))
	for i, r1 := range sh1.routes {
		r2 := sh2.routes[i]
		require.Equal(t, r1.method, r2.method)
		require.Equal(t, r1.path, r2.path)
		require.Equal(t, r1.status, r2.status)
		// Response sizes should be consistent (same schema produces same structure)
		require.Equal(t, len(r1.response) > 0, len(r2.response) > 0)
	}
}

func TestSpecHandler_Integration(t *testing.T) {
	rows := 3
	seed := int64(42)
	cfg := &confighttp.Service{
		Name:   "spec-test",
		Listen: "127.0.0.1:0",
		Spec: &config.SpecConfig{
			Path: "testdata/petstore.yaml",
			Rows: &rows,
			Seed: &seed,
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

	t.Run("GET /pets returns array", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/pets")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, "application/json", resp.Header.Get("Content-Type"))

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var items []json.RawMessage
		err = json.Unmarshal(body, &items)
		require.NoError(t, err)
		require.Len(t, items, rows)
	})

	t.Run("GET /pets/:id returns object", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/pets/some-uuid")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, "application/json", resp.Header.Get("Content-Type"))

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var obj map[string]any
		err = json.Unmarshal(body, &obj)
		require.NoError(t, err)
		require.Contains(t, obj, "id")
		require.Contains(t, obj, "name")
	})

	t.Run("DELETE /pets/:id returns 204", func(t *testing.T) {
		req, err := http.NewRequest("DELETE", baseURL+"/pets/some-uuid", nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusNoContent, resp.StatusCode)
	})

	t.Run("handle blocks override spec routes", func(t *testing.T) {
		// No handle blocks configured, so spec routes should handle everything
		resp, err := http.Get(baseURL + "/nonexistent")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}
