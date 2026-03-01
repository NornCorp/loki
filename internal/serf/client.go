package serf

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/hashicorp/serf/serf"
	"github.com/jumppad-labs/polymorph/internal/topology"
)

// ClientConfig contains configuration for creating a Serf client
type ClientConfig struct {
	// NodeName is the name of this node in the mesh
	NodeName string

	// Tags are metadata tags for this node
	Tags map[string]string

	// JoinAddr is the address of the Heimdall mesh to join
	JoinAddr string
}

// Client manages a Serf client connection to Heimdall mesh
type Client struct {
	serf        *serf.Serf
	config      ClientConfig
	broadcaster *topology.Broadcaster
}

// NewClient creates a new Serf client with the given configuration
func NewClient(config ClientConfig) (*Client, error) {
	if config.NodeName == "" {
		// Default to hostname if not specified
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("failed to get hostname: %w", err)
		}
		config.NodeName = hostname
	}

	if config.JoinAddr == "" {
		return nil, fmt.Errorf("join address is required")
	}

	return &Client{
		config: config,
	}, nil
}

// Start initializes and joins the Serf mesh
func (c *Client) Start(ctx context.Context) error {
	// Create Serf configuration
	conf := serf.DefaultConfig()
	conf.NodeName = c.config.NodeName
	conf.Tags = c.config.Tags

	// Initialize memberlist config with defaults
	// This is critical - without it, Serf creates a default config with port 7946
	mlConfig := memberlist.DefaultLANConfig()
	mlConfig.Name = c.config.NodeName

	// Use random port for client (don't conflict with server)
	// Setting port to 0 allows the OS to assign a random available port (ephemeral range)
	mlConfig.BindAddr = "0.0.0.0"
	mlConfig.BindPort = 0
	mlConfig.AdvertiseAddr = ""
	mlConfig.AdvertisePort = 0

	conf.MemberlistConfig = mlConfig

	// Disable event broadcasting since we're a client
	conf.EventCh = nil

	// Create Serf instance
	s, err := serf.Create(conf)
	if err != nil {
		return fmt.Errorf("failed to create serf instance: %w", err)
	}

	c.serf = s

	// Join the Heimdall mesh
	_, err = c.serf.Join([]string{c.config.JoinAddr}, false)
	if err != nil {
		// Clean up on failure
		c.serf.Shutdown()
		return fmt.Errorf("failed to join heimdall mesh at %s: %w", c.config.JoinAddr, err)
	}

	// Start broadcasting topology
	c.broadcaster = topology.NewBroadcaster(s, c.config.NodeName, 30*time.Second)
	c.broadcaster.Start()

	return nil
}

// Stop leaves the mesh and shuts down the client
func (c *Client) Stop() error {
	if c.serf == nil {
		return nil
	}

	// Stop broadcaster
	if c.broadcaster != nil {
		c.broadcaster.Stop()
	}

	// Leave gracefully
	err := c.serf.Leave()
	if err != nil {
		return fmt.Errorf("failed to leave mesh: %w", err)
	}

	// Shutdown
	err = c.serf.Shutdown()
	if err != nil {
		return fmt.Errorf("failed to shutdown serf: %w", err)
	}

	return nil
}

// UpdateTags updates the tags for this node
func (c *Client) UpdateTags(tags map[string]string) error {
	if c.serf == nil {
		return fmt.Errorf("serf client not started")
	}

	return c.serf.SetTags(tags)
}

// Members returns all members in the mesh
func (c *Client) Members() []serf.Member {
	if c.serf == nil {
		return nil
	}
	return c.serf.Members()
}
