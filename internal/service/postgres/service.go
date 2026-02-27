package postgres

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"sync"

	"github.com/norncorp/loki/internal/config"
	"github.com/norncorp/loki/internal/fake"
	"github.com/norncorp/loki/internal/resource"
	"github.com/norncorp/loki/internal/service"
)

// PostgresService implements a fake PostgreSQL database service.
type PostgresService struct {
	name      string
	config    *config.ServiceConfig
	logger    *slog.Logger
	auth      *Authenticator
	matcher   *QueryMatcher
	store     *resource.Store
	listener  net.Listener
	tlsConfig *tls.Config
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewPostgresService creates a new PostgreSQL service from config.
func NewPostgresService(cfg *config.ServiceConfig, logger *slog.Logger) (*PostgresService, error) {
	// Setup authentication
	var users map[string]string
	var database string
	if cfg.Auth != nil {
		users = cfg.Auth.Users
		database = cfg.Auth.Database
	}
	auth := NewAuthenticator(users, database)

	// Setup resource store
	store := resource.NewStore()
	matcher := NewQueryMatcher(store)

	// Create tables and populate with fake data
	for _, tbl := range cfg.Tables {
		schema := resource.Schema{
			Name:   tbl.Name,
			Fields: make([]resource.Field, len(tbl.Columns)),
		}
		for i, col := range tbl.Columns {
			schema.Fields[i] = resource.Field{
				Name:       col.Name,
				Type:       resource.FieldTypeAny,
				PrimaryKey: col.Name == "id",
				Index:      col.Name == "id",
			}
		}

		if err := store.CreateTable(tbl.Name, schema); err != nil {
			return nil, fmt.Errorf("create table %q: %w", tbl.Name, err)
		}

		// Generate fake rows
		if tbl.Rows > 0 {
			var gen *fake.Generator
			if tbl.Seed != nil {
				gen = fake.NewSeededGenerator(*tbl.Seed)
			} else {
				gen = fake.NewGenerator()
			}

			fakeFields := make([]fake.FieldConfig, len(tbl.Columns))
			for i, col := range tbl.Columns {
				fc := fake.FieldConfig{
					Name: col.Name,
					Type: fake.FakeType(col.Type),
				}
				cfg := make(map[string]any)
				if col.Min != nil {
					cfg["min"] = *col.Min
				}
				if col.Max != nil {
					cfg["max"] = *col.Max
				}
				if len(col.Values) > 0 {
					anyValues := make([]any, len(col.Values))
					for j, v := range col.Values {
						anyValues[j] = v
					}
					cfg["values"] = anyValues
				}
				if len(cfg) > 0 {
					fc.Config = cfg
				}
				fakeFields[i] = fc
			}

			rows, err := gen.GenerateRows(fakeFields, tbl.Rows)
			if err != nil {
				return nil, fmt.Errorf("generate data for table %q: %w", tbl.Name, err)
			}
			for _, row := range rows {
				if err := store.Insert(tbl.Name, row); err != nil {
					return nil, fmt.Errorf("insert row into %q: %w", tbl.Name, err)
				}
			}
		}

		// Register table columns with the query matcher
		colDefs := make([]TableColumn, len(tbl.Columns))
		for i, col := range tbl.Columns {
			colDefs[i] = TableColumn{
				Name:    col.Name,
				Type:    col.Type,
				TypeOID: typeOIDForFakeType(col.Type),
			}
		}
		matcher.RegisterTable(tbl.Name, colDefs)
	}

	// Add custom query patterns
	for _, q := range cfg.Queries {
		matcher.AddPattern(q.Pattern, q.FromTable, q.Where)
	}

	return &PostgresService{
		name:    cfg.Name,
		config:  cfg,
		logger:  logger,
		auth:    auth,
		matcher: matcher,
		store:   store,
	}, nil
}

func (s *PostgresService) Name() string      { return s.name }
func (s *PostgresService) Type() string      { return "postgres" }
func (s *PostgresService) Address() string   { return s.config.Listen }
func (s *PostgresService) Upstreams() []string { return s.config.InferredUpstreams }

// Start begins listening for PostgreSQL client connections.
func (s *PostgresService) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	listener, err := net.Listen("tcp", s.config.Listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// Build TLS config if present (used for PostgreSQL SSL negotiation)
	if s.config.TLS != nil {
		tlsCfg, err := service.BuildTLSConfig(s.config.TLS)
		if err != nil {
			listener.Close()
			return fmt.Errorf("failed to configure TLS: %w", err)
		}
		s.tlsConfig = tlsCfg
	}
	s.listener = listener

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop()
	}()

	proto := "PostgreSQL"
	if s.config.TLS != nil {
		proto = "PostgreSQL (TLS)"
	}
	s.logger.Info("service listening", "proto", proto, "addr", s.config.Listen)
	return nil
}

// Stop gracefully shuts down the service.
func (s *PostgresService) Stop(ctx context.Context) error {
	if s.listener == nil {
		return nil
	}

	s.logger.Info("stopping service")

	// Cancel context first so accept loop sees shutdown before listener close error
	if s.cancel != nil {
		s.cancel()
	}
	if err := s.listener.Close(); err != nil {
		return fmt.Errorf("close listener: %w", err)
	}
	s.wg.Wait()
	return nil
}

func (s *PostgresService) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				s.logger.Error("accept error", "error", err)
				continue
			}
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConnection(conn)
		}()
	}
}

func (s *PostgresService) handleConnection(conn net.Conn) {
	defer conn.Close()

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	// Read startup message
	startup, isSSL, err := readStartupMessage(rw)
	if err != nil {
		s.logger.Error("startup error", "error", err)
		return
	}

	// Handle SSL request
	if isSSL {
		if s.tlsConfig != nil {
			// Accept SSL: upgrade the connection to TLS
			if _, err := conn.Write([]byte("S")); err != nil {
				return
			}
			tlsConn := tls.Server(conn, s.tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				s.logger.Error("TLS handshake error", "error", err)
				return
			}
			conn = tlsConn
			rw = bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
		} else {
			// Reject SSL
			if _, err := conn.Write([]byte("N")); err != nil {
				return
			}
		}
		startup, _, err = readStartupMessage(rw)
		if err != nil {
			s.logger.Error("startup error", "error", err)
			return
		}
	}

	// Authenticate
	if _, err := s.auth.Authenticate(rw, startup); err != nil {
		s.logger.Error("auth failed", "error", err)
		rw.Flush()
		return
	}

	// Send server parameters
	writeParameterStatus(rw, "server_version", "16.0 (Loki)")
	writeParameterStatus(rw, "server_encoding", "UTF8")
	writeParameterStatus(rw, "client_encoding", "UTF8")
	writeParameterStatus(rw, "DateStyle", "ISO, MDY")
	writeParameterStatus(rw, "integer_datetimes", "on")
	writeParameterStatus(rw, "standard_conforming_strings", "on")

	// Send backend key data
	writeBackendKeyData(rw, rand.Int31(), rand.Int31())

	// Ready for queries
	writeReadyForQuery(rw, txIdle)
	rw.Flush()

	// Query loop
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		msgType, body, err := readMessage(rw)
		if err != nil {
			if err == io.EOF {
				return
			}
			s.logger.Error("read error", "error", err)
			return
		}

		switch msgType {
		case msgTerminate:
			return
		case msgQuery:
			query := string(body[:len(body)-1]) // strip null terminator
			s.handleQuery(rw, query)
			rw.Flush()
		default:
			writeErrorResponse(rw, "ERROR", "0A000",
				fmt.Sprintf("unsupported message type: %c", msgType))
			writeReadyForQuery(rw, txIdle)
			rw.Flush()
		}
	}
}

func (s *PostgresService) handleQuery(w io.Writer, query string) {
	result, err := s.matcher.Execute(query)
	if err != nil {
		writeErrorResponse(w, "ERROR", "42601", err.Error())
		writeReadyForQuery(w, txIdle)
		return
	}

	if result.Columns != nil {
		writeRowDescription(w, result.Columns)
		for _, row := range result.Rows {
			writeDataRow(w, row)
		}
	}

	writeCommandComplete(w, result.Tag)
	writeReadyForQuery(w, txIdle)
}

func init() {
	service.RegisterFactory("postgres", func(cfg *config.ServiceConfig, logger *slog.Logger) (service.Service, error) {
		return NewPostgresService(cfg, logger)
	})
}
