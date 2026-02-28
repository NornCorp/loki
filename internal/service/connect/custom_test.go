package connect

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/norncorp/loki/internal/config"
	configconnect "github.com/norncorp/loki/internal/config/connect"
	"github.com/norncorp/loki/internal/config/parser"
	"github.com/stretchr/testify/require"
)

func TestCustomMethodsFromHCL(t *testing.T) {
	// Inline HCL with handle blocks instead of method blocks
	hcl := `
service "connect" "user-api" {
  listen  = "0.0.0.0:8080"
  package = "api.v1"

  resource "user" {
    rows = 20
    field "id"    { type = "uuid" }
    field "name"  { type = "name" }
    field "email" { type = "email" }
    field "age" {
      type = "int"
      min  = 18
      max  = 80
    }
  }

  handle "SearchUsers" {
    response {
      body = jsonencode({
        users = [
          { id = uuid(), name = "Custom User 1", email = "custom1@example.com", age = 25 },
          { id = uuid(), name = "Custom User 2", email = "custom2@example.com", age = 30 }
        ]
        query = request.query
      })
    }
  }

  handle "GetUserStats" {
    response {
      body = jsonencode({
        total_users  = 20
        requested_id = request.id
        timestamp    = timestamp()
      })
    }
  }

  handle "Echo" {
    response {
      body = jsonencode({
        message  = "Echo response"
        received = request
      })
    }
  }
}
`

	cfg, err := parser.Parse([]byte(hcl), "test-custom-methods.hcl")
	require.NoError(t, err)
	require.Len(t, cfg.Services, 1)

	connectCfg := cfg.Services[0].(*configconnect.Service)
	require.Len(t, connectCfg.Handlers, 3) // SearchUsers, GetUserStats, Echo

	svc, err := NewConnectService(connectCfg, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.Len(t, svc.customHandlers, 3)
}

func TestMethodOverridesFromHCL(t *testing.T) {
	// Inline HCL with handle blocks instead of method blocks
	hcl := `
service "connect" "user-api" {
  listen  = "0.0.0.0:8080"
  package = "api.v1"

  resource "user" {
    rows = 5
    field "id"   { type = "uuid" }
    field "name" { type = "name" }
  }

  handle "GetUser" {
    response {
      body = jsonencode({
        id   = "override-id"
        name = "Overridden User"
        note = "This is a custom override of the auto-generated GetUser method"
      })
    }
  }

  handle "ListUsers" {
    response {
      body = jsonencode({
        users = [
          { id = "custom-1", name = "Custom User 1" },
          { id = "custom-2", name = "Custom User 2" }
        ]
        note = "Custom list override - not using the resource store"
      })
    }
  }
}
`

	cfg, err := parser.Parse([]byte(hcl), "test-method-overrides.hcl")
	require.NoError(t, err)
	require.Len(t, cfg.Services, 1)

	connectCfg := cfg.Services[0].(*configconnect.Service)
	require.Len(t, connectCfg.Handlers, 2) // GetUser, ListUsers overrides

	svc, err := NewConnectService(connectCfg, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, svc)

	// Verify custom handlers were created
	require.Len(t, svc.customHandlers, 2)

	// Verify resource handlers still exist (for non-overridden methods)
	require.Len(t, svc.resourceHandlers, 1)
}

func TestCustomMethodHandler(t *testing.T) {
	method := &configconnect.Handler{
		Name: "TestMethod",
	}

	handler, err := NewCustomMethodHandler(method, "api.v1", "UserService", nil)
	require.NoError(t, err)
	require.NotNil(t, handler)
	require.Equal(t, "TestMethod", handler.method.Name)
	require.Equal(t, "api.v1", handler.packageName)
	require.Equal(t, "UserService", handler.serviceName)

	// Test path generation
	path, _ := handler.RegisterHandler()
	require.Equal(t, "/api.v1.UserService/TestMethod", path)
}

func TestConnectServiceWithCustomMethods(t *testing.T) {
	cfg := &configconnect.Service{
		Name:    "test-api",
		Listen:  "127.0.0.1:0",
		Package: "api.v1",
		Resources: []*config.ResourceConfig{
			{
				Name: "user",
				Rows: 3,
				Fields: []*config.FieldConfig{
					{Name: "id", Type: "uuid"},
				},
			},
		},
		Handlers: []*configconnect.Handler{
			{
				Name: "CustomMethod",
			},
		},
	}

	svc, err := NewConnectService(cfg, slog.Default())
	require.NoError(t, err)
	require.Len(t, svc.customHandlers, 1)
	require.Len(t, svc.resourceHandlers, 1)

	// Test start/stop
	ctx := context.Background()
	err = svc.Start(ctx)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	err = svc.Stop(ctx)
	require.NoError(t, err)
}
