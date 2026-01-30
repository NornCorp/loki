package topology

import (
	"encoding/json"
	"log"
	"time"

	"github.com/hashicorp/serf/serf"
)

// TopologyEvent is broadcast via Serf user events
type TopologyEvent struct {
	Node      string   `json:"n"`  // Node name
	Neighbors []string `json:"nb"` // Direct neighbors (nodes we can reach)
}

// Broadcaster periodically broadcasts topology updates
type Broadcaster struct {
	mesh     *serf.Serf
	nodeName string
	interval time.Duration
	stopCh   chan struct{}
}

// NewBroadcaster creates a new topology broadcaster
func NewBroadcaster(mesh *serf.Serf, nodeName string, interval time.Duration) *Broadcaster {
	return &Broadcaster{
		mesh:     mesh,
		nodeName: nodeName,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins broadcasting topology updates
func (b *Broadcaster) Start() {
	// Broadcast immediately on start
	b.broadcastTopology()

	// Then periodically
	ticker := time.NewTicker(b.interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				b.broadcastTopology()
			case <-b.stopCh:
				ticker.Stop()
				return
			}
		}
	}()
}

// Stop stops broadcasting
func (b *Broadcaster) Stop() {
	close(b.stopCh)
}

// broadcastTopology broadcasts current topology to the mesh
func (b *Broadcaster) broadcastTopology() {
	neighbors := b.getNeighbors()

	event := TopologyEvent{
		Node:      b.nodeName,
		Neighbors: neighbors,
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("Failed to marshal topology event: %v", err)
		return
	}

	// Broadcast via Serf user event
	if err := b.mesh.UserEvent("topology", data, false); err != nil {
		log.Printf("Failed to broadcast topology event: %v", err)
	}
}

// getNeighbors returns list of reachable neighbors based on Serf member status
func (b *Broadcaster) getNeighbors() []string {
	neighbors := []string{}

	for _, member := range b.mesh.Members() {
		// Skip self
		if member.Name == b.nodeName {
			continue
		}

		// If Serf can gossip with them, we can likely reach them via HTTP
		if member.Status == serf.StatusAlive {
			neighbors = append(neighbors, member.Name)
		}
	}

	return neighbors
}

// OnMemberJoin should be called when a new member joins
func (b *Broadcaster) OnMemberJoin() {
	// Broadcast updated topology immediately when mesh changes
	b.broadcastTopology()
}

// OnMemberLeave should be called when a member leaves
func (b *Broadcaster) OnMemberLeave() {
	// Broadcast updated topology immediately when mesh changes
	b.broadcastTopology()
}
