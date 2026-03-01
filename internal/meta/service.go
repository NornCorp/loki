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
	"github.com/jumppad-labs/polymorph/internal/config"
	metav1 "github.com/jumppad-labs/polymorph/pkg/api/meta/v1"
	"github.com/jumppad-labs/polymorph/pkg/api/meta/v1/metaapiconnect"
)

// SerfClient is an interface for looking up mesh members
type SerfClient interface {
	Members() []serf.Member
}

// RequestLog represents a captured HTTP request log
type RequestLog struct {
	Sequence   uint64
	Timestamp  int64 // Unix milliseconds
	Method     string
	Path       string
	Status     int32
	DurationMs int64
	Level      string // "info" or "debug"
}

// RequestLogProvider provides access to request logs for a service
type RequestLogProvider interface {
	GetLogs(serviceName string, afterSequence uint64, limit int32) ([]RequestLog, uint64)
}

// MetaService implements the PolymorphMetaService RPC
type MetaService struct {
	services           []config.Service
	nodeName           string
	serfClient         SerfClient
	requestLogProvider RequestLogProvider
}

// NewMetaService creates a new MetaService
func NewMetaService(services []config.Service, serfClient SerfClient, logProvider RequestLogProvider) *MetaService {
	return &MetaService{
		services:           services,
		serfClient:         serfClient,
		requestLogProvider: logProvider,
	}
}

// SetNodeName sets the node name for forwarding
func (s *MetaService) SetNodeName(nodeName string) {
	s.nodeName = nodeName
}

// Verify interface implementation
var _ metaapiconnect.PolymorphMetaServiceHandler = (*MetaService)(nil)

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
		if req.Msg.ServiceName != "" && svc.ServiceName() != req.Msg.ServiceName {
			continue
		}

		// Skip services with no resources
		svcResources := svc.GetResources()
		if len(svcResources) == 0 {
			continue
		}

		// Convert resources to proto format
		pluralizer := pluralize.NewClient()
		resources := make([]*metav1.Resource, 0, len(svcResources))
		for _, res := range svcResources {
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
			ServiceName: svc.ServiceName(),
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
	forwardURL := fmt.Sprintf("http://%s/meta.v1.PolymorphMetaService/GetResources", nextServiceAddr)
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

// GetRequestLogs returns recent HTTP request logs for a service
func (s *MetaService) GetRequestLogs(
	ctx context.Context,
	req *connect.Request[metav1.GetRequestLogsRequest],
) (*connect.Response[metav1.GetRequestLogsResponse], error) {
	// Check if we need to forward this request
	if len(req.Msg.Path) > 0 {
		return s.forwardRequestLogs(ctx, req)
	}

	// Get logs from the request log provider
	if s.requestLogProvider == nil {
		return connect.NewResponse(&metav1.GetRequestLogsResponse{
			Logs:           []*metav1.RequestLog{},
			LatestSequence: 0,
		}), nil
	}

	// Default limit
	limit := req.Msg.Limit
	if limit <= 0 {
		limit = 100
	}

	logs, latestSeq := s.requestLogProvider.GetLogs(
		req.Msg.ServiceName,
		req.Msg.AfterSequence,
		limit,
	)

	// Convert to proto format
	protoLogs := make([]*metav1.RequestLog, 0, len(logs))
	for _, log := range logs {
		protoLogs = append(protoLogs, &metav1.RequestLog{
			Sequence:   log.Sequence,
			Timestamp:  log.Timestamp,
			Method:     log.Method,
			Path:       log.Path,
			Status:     log.Status,
			DurationMs: log.DurationMs,
			Level:      log.Level,
		})
	}

	resp := &metav1.GetRequestLogsResponse{
		Logs:           protoLogs,
		LatestSequence: latestSeq,
	}

	return connect.NewResponse(resp), nil
}

// forwardRequestLogs forwards the GetRequestLogs request to the next hop in the path
func (s *MetaService) forwardRequestLogs(
	ctx context.Context,
	req *connect.Request[metav1.GetRequestLogsRequest],
) (*connect.Response[metav1.GetRequestLogsResponse], error) {
	nextHop := int(req.Msg.CurrentHop) + 1

	// Are we the destination?
	if nextHop >= len(req.Msg.Path) {
		// We're the final destination, handle locally
		localReq := connect.NewRequest(&metav1.GetRequestLogsRequest{
			ServiceName:   req.Msg.ServiceName,
			AfterSequence: req.Msg.AfterSequence,
			Limit:         req.Msg.Limit,
			// No path = handle locally
		})
		return s.GetRequestLogs(ctx, localReq)
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
	forwardURL := fmt.Sprintf("http://%s/meta.v1.PolymorphMetaService/GetRequestLogs", nextServiceAddr)
	forwardReq := map[string]any{
		"serviceName":   req.Msg.ServiceName,
		"afterSequence": req.Msg.AfterSequence,
		"limit":         req.Msg.Limit,
		"path":          req.Msg.Path,
		"currentHop":    nextHop,
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
	var response metav1.GetRequestLogsResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&response); err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("failed to parse response from next hop: %w", err))
	}

	return connect.NewResponse(&response), nil
}
