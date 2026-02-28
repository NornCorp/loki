package tcp

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/hashicorp/hcl/v2"
	"github.com/norncorp/loki/internal/config"
	configtcp "github.com/norncorp/loki/internal/config/tcp"
	"github.com/norncorp/loki/internal/service"
)

// TCPService implements a TCP service with pattern matching
type TCPService struct {
	name     string
	config   *configtcp.Service
	logger   *slog.Logger
	matcher  *Matcher
	listener net.Listener
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewTCPService creates a new TCP service
func NewTCPService(cfg *configtcp.Service, logger *slog.Logger) (*TCPService, error) {
	// Create matcher
	matcher := NewMatcher()

	// Add patterns from handle blocks
	evalCtx := &hcl.EvalContext{}
	for _, handler := range cfg.Handlers {
		if handler.Response == nil || handler.Response.BodyExpr == nil {
			continue
		}

		// Evaluate response body expression
		value, diags := handler.Response.BodyExpr.Value(evalCtx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("failed to evaluate handler %q response: %s", handler.Name, diags.Error())
		}
		responseStr := value.AsString()

		if handler.Name == "default" || handler.Pattern == "" {
			// Handler named "default" or with no pattern becomes the catch-all
			matcher.SetDefault(responseStr)
		} else {
			// Handler with a pattern becomes a pattern rule
			matcher.AddPattern(handler.Pattern, responseStr)
		}
	}

	svc := &TCPService{
		name:    cfg.Name,
		config:  cfg,
		logger:  logger,
		matcher: matcher,
	}

	return svc, nil
}

// Name returns the service name
func (s *TCPService) Name() string {
	return s.name
}

// Type returns the service type
func (s *TCPService) Type() string {
	return "tcp"
}

// Address returns the service listen address
func (s *TCPService) Address() string {
	return s.config.Listen
}

// Upstreams returns the list of upstream service dependencies
func (s *TCPService) Upstreams() []string {
	return s.config.Upstreams
}

// Start starts the TCP server
func (s *TCPService) Start(ctx context.Context) error {
	// Create context for managing connections
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Create listener
	listener, err := net.Listen("tcp", s.config.Listen)
	if err != nil {
		return fmt.Errorf("failed to create TCP listener: %w", err)
	}

	// Wrap with TLS if configured
	listener, err = service.WrapListenerTLS(listener, s.config.TLS)
	if err != nil {
		listener.Close()
		return fmt.Errorf("failed to configure TLS: %w", err)
	}
	s.listener = listener

	// Start accepting connections in background
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop()
	}()

	proto := "TCP"
	if s.config.TLS != nil {
		proto = "TCP (TLS)"
	}
	s.logger.Info("service listening", "proto", proto, "addr", s.config.Listen)
	return nil
}

// Stop gracefully stops the TCP server
func (s *TCPService) Stop(ctx context.Context) error {
	if s.listener == nil {
		return nil
	}

	s.logger.Info("stopping service")

	// Close listener to stop accepting new connections
	if err := s.listener.Close(); err != nil {
		return fmt.Errorf("failed to close listener: %w", err)
	}

	// Cancel context to signal all connections to close
	if s.cancel != nil {
		s.cancel()
	}

	// Wait for all connections to finish
	s.wg.Wait()

	return nil
}

// acceptLoop accepts incoming connections
func (s *TCPService) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Check if we're shutting down
			select {
			case <-s.ctx.Done():
				return
			default:
				s.logger.Error("accept error", "error", err)
				continue
			}
		}

		// Handle connection in background
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConnection(conn)
		}()
	}
}

// handleConnection handles a single TCP connection
func (s *TCPService) handleConnection(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		// Check if context is cancelled
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		// Read incoming line
		line := scanner.Text()

		// Match against patterns
		response := s.matcher.Match(line)

		// Send response
		if response != "" {
			if _, err := conn.Write([]byte(response)); err != nil {
				s.logger.Error("write error", "error", err)
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		// Only log if not due to connection close
		select {
		case <-s.ctx.Done():
			return
		default:
			s.logger.Error("scan error", "error", err)
		}
	}
}

// init registers the TCP service factory
func init() {
	service.RegisterFactory("tcp", func(cfg config.Service, logger *slog.Logger) (service.Service, error) {
		c, ok := cfg.(*configtcp.Service)
		if !ok {
			return nil, fmt.Errorf("tcp: unexpected config type %T", cfg)
		}
		return NewTCPService(c, logger)
	})
}
