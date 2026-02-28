package service

import (
	"context"
	"testing"

	"github.com/norncorp/loki/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRegistry_ConfigureHeimdall(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *config.HeimdallConfig
		wantErr   bool
		errString string
	}{
		{
			name:    "nil config runs in standalone mode",
			cfg:     nil,
			wantErr: false,
		},
		{
			name: "valid config",
			cfg: &config.HeimdallConfig{
				Address: "localhost:7946",
			},
			wantErr: false,
		},
		{
			name: "missing address",
			cfg: &config.HeimdallConfig{
				Address: "",
			},
			wantErr:   true,
			errString: "heimdall address is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry(nil)

			// Add a mock service
			mockSvc := &mockService{
				name: "test-service",
				typ:  "http",
			}
			registry.Register(mockSvc)

			err := registry.ConfigureHeimdall(tt.cfg, []config.Service{})
			if tt.wantErr {
				require.Error(t, err)
				if tt.errString != "" {
					require.Contains(t, err.Error(), tt.errString)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestRegistry_StartStopWithoutHeimdall(t *testing.T) {
	registry := NewRegistry(nil)

	mockSvc := &mockService{
		name: "test-service",
		typ:  "http",
	}
	registry.Register(mockSvc)

	ctx := context.Background()

	// Should work without Heimdall config
	err := registry.Start(ctx)
	require.NoError(t, err)
	require.True(t, mockSvc.started)

	err = registry.Stop(ctx)
	require.NoError(t, err)
	require.True(t, mockSvc.stopped)
}

func TestRegistry_Services(t *testing.T) {
	registry := NewRegistry(nil)

	svc1 := &mockService{name: "svc1", typ: "http"}
	svc2 := &mockService{name: "svc2", typ: "tcp"}

	registry.Register(svc1)
	registry.Register(svc2)

	services := registry.Services()
	require.Len(t, services, 2)
	require.Equal(t, "svc1", services[0].Name())
	require.Equal(t, "svc2", services[1].Name())
}

// mockService is a mock implementation of the Service interface
type mockService struct {
	name     string
	typ      string
	started  bool
	stopped  bool
	startErr error
	stopErr  error
}

func (m *mockService) Start(ctx context.Context) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.started = true
	return nil
}

func (m *mockService) Stop(ctx context.Context) error {
	if m.stopErr != nil {
		return m.stopErr
	}
	m.stopped = true
	return nil
}

func (m *mockService) Name() string {
	return m.name
}

func (m *mockService) Type() string {
	return m.typ
}

func (m *mockService) Address() string {
	return "localhost:8080"
}

func (m *mockService) Upstreams() []string {
	return []string{}
}
