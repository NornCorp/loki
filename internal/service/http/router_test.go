package http

import (
	"net/http/httptest"
	"testing"

	confighttp "github.com/norncorp/loki/internal/config/http"
	"github.com/stretchr/testify/require"
)

func TestParseRoute(t *testing.T) {
	tests := []struct {
		name        string
		routeStr    string
		wantMethod  string
		wantPath    string
		expectError bool
	}{
		{
			name:       "method and path",
			routeStr:   "GET /hello",
			wantMethod: "GET",
			wantPath:   "/hello",
		},
		{
			name:       "lowercase method",
			routeStr:   "post /users",
			wantMethod: "POST",
			wantPath:   "/users",
		},
		{
			name:       "path only",
			routeStr:   "/health",
			wantMethod: "",
			wantPath:   "/health",
		},
		{
			name:        "empty route",
			routeStr:    "",
			expectError: true,
		},
		{
			name:        "path without slash",
			routeStr:    "GET hello",
			expectError: true,
		},
		{
			name:        "too many parts",
			routeStr:    "GET /hello world",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, err := parseRoute(tt.routeStr)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantMethod, route.Method)
			require.Equal(t, tt.wantPath, route.Path)
		})
	}
}

func TestRouter_AddHandler(t *testing.T) {
	router := NewRouter()

	handler := &confighttp.Handler{
		Name:  "test",
		Route: "GET /test",
	}

	err := router.AddHandler(handler)
	require.NoError(t, err)
	require.Len(t, router.routes, 1)
	require.Equal(t, "GET", router.routes[0].Method)
	require.Equal(t, "/test", router.routes[0].Path)
}

func TestRouter_AddHandler_InvalidRoute(t *testing.T) {
	router := NewRouter()

	handler := &confighttp.Handler{
		Name:  "test",
		Route: "invalid",
	}

	err := router.AddHandler(handler)
	require.Error(t, err)
}

func TestRouter_Match(t *testing.T) {
	router := NewRouter()

	handlers := []*confighttp.Handler{
		{
			Name:  "hello",
			Route: "GET /hello",
		},
		{
			Name:  "health",
			Route: "GET /health",
		},
		{
			Name:  "any_method",
			Route: "/any",
		},
	}

	for _, h := range handlers {
		require.NoError(t, router.AddHandler(h))
	}

	tests := []struct {
		name        string
		method      string
		path        string
		shouldMatch bool
		wantHandler string
	}{
		{
			name:        "exact match GET /hello",
			method:      "GET",
			path:        "/hello",
			shouldMatch: true,
			wantHandler: "hello",
		},
		{
			name:        "exact match GET /health",
			method:      "GET",
			path:        "/health",
			shouldMatch: true,
			wantHandler: "health",
		},
		{
			name:        "any method matches",
			method:      "POST",
			path:        "/any",
			shouldMatch: true,
			wantHandler: "any_method",
		},
		{
			name:        "wrong method",
			method:      "POST",
			path:        "/hello",
			shouldMatch: false,
		},
		{
			name:        "wrong path",
			method:      "GET",
			path:        "/notfound",
			shouldMatch: false,
		},
		{
			name:        "case sensitive path",
			method:      "GET",
			path:        "/HELLO",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			route, ok := router.Match(req)

			require.Equal(t, tt.shouldMatch, ok)
			if tt.shouldMatch {
				require.NotNil(t, route)
				require.Equal(t, tt.wantHandler, route.Handler.Name)
			}
		})
	}
}

func TestRouter_Match_PathParams(t *testing.T) {
	router := NewRouter()

	handlers := []*confighttp.Handler{
		{Name: "kv-get", Route: "GET /v1/secret/data/:path"},
		{Name: "kv-list", Route: "GET /v1/secret/metadata/:path"},
		{Name: "health", Route: "GET /v1/sys/health"},
	}
	for _, h := range handlers {
		require.NoError(t, router.AddHandler(h))
	}

	tests := []struct {
		name        string
		method      string
		path        string
		shouldMatch bool
		wantHandler string
	}{
		{
			name:        "exact match health",
			method:      "GET",
			path:        "/v1/sys/health",
			shouldMatch: true,
			wantHandler: "health",
		},
		{
			name:        "param match kv-get",
			method:      "GET",
			path:        "/v1/secret/data/mysecret",
			shouldMatch: true,
			wantHandler: "kv-get",
		},
		{
			name:        "param match kv-list",
			method:      "GET",
			path:        "/v1/secret/metadata/keys",
			shouldMatch: true,
			wantHandler: "kv-list",
		},
		{
			name:        "wrong method on param route",
			method:      "POST",
			path:        "/v1/secret/data/test",
			shouldMatch: false,
		},
		{
			name:        "too few segments",
			method:      "GET",
			path:        "/v1/secret/data",
			shouldMatch: false,
		},
		{
			name:        "too many segments",
			method:      "GET",
			path:        "/v1/secret/data/a/b",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			route, ok := router.Match(req)
			require.Equal(t, tt.shouldMatch, ok)
			if tt.shouldMatch {
				require.Equal(t, tt.wantHandler, route.Handler.Name)
			}
		})
	}
}

func TestExtractParams(t *testing.T) {
	route := &Route{Method: "GET", Path: "/v1/secret/data/:path"}
	req := httptest.NewRequest("GET", "/v1/secret/data/mysecret", nil)
	params := ExtractParams(route, req)
	require.Equal(t, "mysecret", params["path"])

	route2 := &Route{Method: "GET", Path: "/users/:id/posts/:postid"}
	req2 := httptest.NewRequest("GET", "/users/42/posts/99", nil)
	params2 := ExtractParams(route2, req2)
	require.Equal(t, "42", params2["id"])
	require.Equal(t, "99", params2["postid"])
}

func TestRouter_Match_EmptyRouter(t *testing.T) {
	router := NewRouter()
	req := httptest.NewRequest("GET", "/test", nil)

	route, ok := router.Match(req)
	require.False(t, ok)
	require.Nil(t, route)
}
