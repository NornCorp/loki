package serf

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name      string
		config    ClientConfig
		wantErr   bool
		errString string
	}{
		{
			name: "valid config",
			config: ClientConfig{
				NodeName: "test-node",
				JoinAddr: "localhost:7946",
				Tags: map[string]string{
					"service": "test",
				},
			},
			wantErr: false,
		},
		{
			name: "uses hostname when node name not specified",
			config: ClientConfig{
				JoinAddr: "localhost:7946",
			},
			wantErr: false,
		},
		{
			name: "missing join address",
			config: ClientConfig{
				NodeName: "test-node",
			},
			wantErr:   true,
			errString: "join address is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.config)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errString != "" {
					require.Contains(t, err.Error(), tt.errString)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, client)
			require.NotEmpty(t, client.config.NodeName)
		})
	}
}

func TestClient_Stop_BeforeStart(t *testing.T) {
	client, err := NewClient(ClientConfig{
		NodeName: "test-node",
		JoinAddr: "localhost:7946",
	})
	require.NoError(t, err)

	// Should not error when stopping before starting
	err = client.Stop()
	require.NoError(t, err)
}

func TestClient_UpdateTags_BeforeStart(t *testing.T) {
	client, err := NewClient(ClientConfig{
		NodeName: "test-node",
		JoinAddr: "localhost:7946",
	})
	require.NoError(t, err)

	// Should error when updating tags before starting
	err = client.UpdateTags(map[string]string{"key": "value"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "serf client not started")
}
