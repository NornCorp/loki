package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"connectrpc.com/connect"
	"github.com/gertd/go-pluralize"
	"github.com/hashicorp/serf/serf"
	"github.com/norncorp/loki/internal/config"
	metav1 "github.com/norncorp/loki/pkg/api/meta/v1"
	"github.com/norncorp/loki/pkg/api/meta/v1/metaapiconnect"
)

// SerfClient is an interface for looking up mesh members
type SerfClient interface {
	Members() []serf.Member
}

// MetaService implements the LokiMetaService RPC
type MetaService struct {
	services   []*config.ServiceConfig
	nodeName   string
	serfClient SerfClient
}

// NewMetaService creates a new MetaService
func NewMetaService(services []*config.ServiceConfig, serfClient SerfClient) *MetaService {
	return &MetaService{
		services:   services,
		serfClient: serfClient,
	}
}

// SetNodeName sets the node name for forwarding
func (s *MetaService) SetNodeName(nodeName string) {
	s.nodeName = nodeName
}

// Verify interface implementation
var _ metaapiconnect.LokiMetaServiceHandler = (*MetaService)(nil)

// GetResources returns resource schemas for services on this node
func (s *MetaService) GetResources(
	ctx context.Context,
	req *connect.Request[metav1.GetResourcesRequest],
) (*connect.Response[metav1.GetResourcesResponse], error) {
	// Check if we need to forward this request
	if len(req.Msg.Path) > 0 {
		return s.forwardRequest(ctx, req)
	}

	// Handle locally
	var serviceResources []*metav1.ServiceResources

	for _, svc := range s.services {
		// Filter by service name if requested
		if req.Msg.ServiceName != "" && svc.Name != req.Msg.ServiceName {
			continue
		}

		// Skip services with no resources
		if len(svc.Resources) == 0 {
			continue
		}

		// Convert resources to proto format
		pluralizer := pluralize.NewClient()
		resources := make([]*metav1.Resource, 0, len(svc.Resources))
		for _, res := range svc.Resources {
			fields := make([]*metav1.Field, 0, len(res.Fields))
			for _, field := range res.Fields {
				fields = append(fields, &metav1.Field{
					Name:   field.Name,
					Type:   field.Type,
					Values: field.Values,
					Min:    field.Min,
					Max:    field.Max,
				})
			}

			resources = append(resources, &metav1.Resource{
				Name:       res.Name,
				RowCount:   int32(res.Rows),
				Fields:     fields,
				PluralName: pluralizer.Plural(res.Name),
			})
		}

		serviceResources = append(serviceResources, &metav1.ServiceResources{
			ServiceName: svc.Name,
			Resources:   resources,
		})
	}

	resp := &metav1.GetResourcesResponse{
		Services: serviceResources,
	}

	return connect.NewResponse(resp), nil
}

// forwardRequest forwards the request to the next hop in the path
func (s *MetaService) forwardRequest(
	ctx context.Context,
	req *connect.Request[metav1.GetResourcesRequest],
) (*connect.Response[metav1.GetResourcesResponse], error) {
	nextHop := int(req.Msg.CurrentHop) + 1

	// Are we the destination?
	if nextHop >= len(req.Msg.Path) {
		// We're the final destination, handle locally
		localReq := connect.NewRequest(&metav1.GetResourcesRequest{
			ServiceName: req.Msg.ServiceName,
			// No path = handle locally
		})
		return s.GetResources(ctx, localReq)
	}

	// Forward to next node in path
	nextNodeName := req.Msg.Path[nextHop]

	// Look up the next hop's address from Serf
	if s.serfClient == nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("cannot forward requests in standalone mode"))
	}

	var nextServiceAddr string
	members := s.serfClient.Members()
	for _, member := range members {
		if member.Name == nextNodeName {
			// Find an HTTP service on this node from the "services" tag
			if servicesJSON, ok := member.Tags["services"]; ok {
				var serviceInfos []struct {
					Name    string `json:"name"`
					Type    string `json:"type"`
					Address string `json:"address"`
				}
				if err := json.Unmarshal([]byte(servicesJSON), &serviceInfos); err != nil {
					continue
				}
				// Find first HTTP service
				for _, info := range serviceInfos {
					if info.Type == "http" {
						nextServiceAddr = info.Address
						break
					}
				}
			}
			break
		}
	}

	if nextServiceAddr == "" {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("cannot find service address for node %q", nextNodeName))
	}

	// Build forwarding request
	forwardURL := fmt.Sprintf("http://%s/meta.v1.LokiMetaService/GetResources", nextServiceAddr)
	forwardReq := map[string]any{
		"serviceName": req.Msg.ServiceName,
		"path":        req.Msg.Path,
		"currentHop":  nextHop,
	}

	reqJSON, err := json.Marshal(forwardReq)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Make HTTP request to next hop
	httpReq, err := http.NewRequestWithContext(ctx, "POST", forwardURL, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable,
			fmt.Errorf("failed to forward to next hop: %w", err))
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("next hop returned status %d: %s", httpResp.StatusCode, string(body)))
	}

	// Parse and return response
	var response metav1.GetResourcesResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&response); err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("failed to parse response from next hop: %w", err))
	}

	return connect.NewResponse(&response), nil
}
