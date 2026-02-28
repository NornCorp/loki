package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/norncorp/loki/internal/config"
	configconnect "github.com/norncorp/loki/internal/config/connect"
	"github.com/stretchr/testify/require"
)

func TestNewConnectService(t *testing.T) {
	cfg := &configconnect.Service{
		Name:    "test-api",
		Listen:  "0.0.0.0:9090",
		Package: "api.v1",
		Resources: []*config.ResourceConfig{
			{
				Name: "user",
				Fields: []*config.FieldConfig{
					{Name: "id", Type: "uuid"},
					{Name: "name", Type: "name"},
				},
			},
		},
	}

	svc, err := NewConnectService(cfg, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.Equal(t, "test-api", svc.Name())
	require.Equal(t, "connect", svc.Type())
	require.Equal(t, "0.0.0.0:9090", svc.Address())
}

func TestNewConnectServiceNoPack(t *testing.T) {
	cfg := &configconnect.Service{
		Name:   "test-api",
		Listen: "0.0.0.0:9090",
	}

	_, err := NewConnectService(cfg, slog.Default())
	require.Error(t, err)
	require.Contains(t, err.Error(), "package is required")
}

func TestConnectServiceStartStop(t *testing.T) {
	cfg := &configconnect.Service{
		Name:    "test-api",
		Listen:  "127.0.0.1:0",
		Package: "api.v1",
		Resources: []*config.ResourceConfig{
			{
				Name: "user",
				Rows: 5,
				Fields: []*config.FieldConfig{
					{Name: "id", Type: "uuid"},
					{Name: "name", Type: "name"},
				},
			},
		},
	}

	svc, err := NewConnectService(cfg, slog.Default())
	require.NoError(t, err)

	ctx := context.Background()

	// Start service
	err = svc.Start(ctx)
	require.NoError(t, err)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Stop service
	err = svc.Stop(ctx)
	require.NoError(t, err)
}

func TestConnectServiceCRUD(t *testing.T) {
	cfg := &configconnect.Service{
		Name:    "test-api",
		Listen:  "127.0.0.1:18080",
		Package: "api.v1",
		Resources: []*config.ResourceConfig{
			{
				Name: "user",
				Rows: 10,
				Fields: []*config.FieldConfig{
					{Name: "id", Type: "uuid"},
					{Name: "name", Type: "name"},
					{Name: "email", Type: "email"},
				},
			},
		},
	}

	svc, err := NewConnectService(cfg, slog.Default())
	require.NoError(t, err)

	ctx := context.Background()
	err = svc.Start(ctx)
	require.NoError(t, err)
	defer svc.Stop(ctx)

	time.Sleep(200 * time.Millisecond)

	baseURL := "http://127.0.0.1:18080/api.v1.UserService"

	// Test List
	listResp := makeRequest(t, baseURL+"/ListUsers", map[string]any{})
	users, ok := listResp["users"].([]any)
	require.True(t, ok)
	require.Len(t, users, 10)

	// Get first user ID
	firstUser := users[0].(map[string]any)
	userID := firstUser["id"].(string)

	// Test Get
	getResp := makeRequest(t, baseURL+"/GetUser", map[string]any{"id": userID})
	require.Equal(t, userID, getResp["id"])

	// Test Create
	newUser := map[string]any{
		"id":    "test-123",
		"name":  "Test User",
		"email": "test@example.com",
	}
	createResp := makeRequest(t, baseURL+"/CreateUser", map[string]any{"user": newUser})
	require.Equal(t, "test-123", createResp["id"])
	require.Equal(t, "Test User", createResp["name"])

	// Test Update
	updatedUser := map[string]any{
		"id":    "test-123",
		"name":  "Updated User",
		"email": "updated@example.com",
	}
	updateResp := makeRequest(t, baseURL+"/UpdateUser", map[string]any{"user": updatedUser})
	require.Equal(t, "Updated User", updateResp["name"])

	// Test Delete
	deleteResp := makeRequest(t, baseURL+"/DeleteUser", map[string]any{"id": "test-123"})
	require.Empty(t, deleteResp)

	// Verify deletion
	resp, err := http.Post(baseURL+"/GetUser", "application/json",
		bytes.NewBuffer(mustMarshal(map[string]any{"id": "test-123"})))
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func makeRequest(t *testing.T, url string, body map[string]any) map[string]any {
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(mustMarshal(body)))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	return result
}

func mustMarshal(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
