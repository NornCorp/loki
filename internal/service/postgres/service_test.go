package postgres

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/norncorp/loki/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNewPostgresService_Minimal(t *testing.T) {
	cfg := &config.ServiceConfig{
		Name:   "testdb",
		Type:   "postgres",
		Listen: "127.0.0.1:0",
	}

	svc, err := NewPostgresService(cfg, slog.Default())
	require.NoError(t, err)
	require.Equal(t, "testdb", svc.Name())
	require.Equal(t, "postgres", svc.Type())
}

func TestNewPostgresService_WithTables(t *testing.T) {
	seed := int64(42)
	cfg := &config.ServiceConfig{
		Name:   "testdb",
		Type:   "postgres",
		Listen: "127.0.0.1:0",
		Auth: &config.AuthConfig{
			Users:    map[string]string{"app": "secret"},
			Database: "myapp",
		},
		Tables: []*config.TableConfig{
			{
				Name: "user",
				Rows: 10,
				Seed: &seed,
				Columns: []*config.ColumnConfig{
					{Name: "id", Type: "uuid"},
					{Name: "name", Type: "name"},
					{Name: "email", Type: "email"},
				},
			},
		},
	}

	svc, err := NewPostgresService(cfg, slog.Default())
	require.NoError(t, err)

	// Verify table data was generated
	items, err := svc.store.List("user")
	require.NoError(t, err)
	require.Len(t, items, 10)
}

func startTestService(t *testing.T, cfg *config.ServiceConfig) (*PostgresService, string) {
	t.Helper()

	svc, err := NewPostgresService(cfg, slog.Default())
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))

	// Get actual listen address
	addr := svc.listener.Addr().String()

	t.Cleanup(func() {
		svc.Stop(ctx)
	})

	return svc, addr
}

func connectPG(t *testing.T, addr, user, database, password string) *bufio.ReadWriter {
	t.Helper()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)

	t.Cleanup(func() { conn.Close() })

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	// Send startup message
	params := fmt.Sprintf("user\x00%s\x00database\x00%s\x00\x00", user, database)
	length := int32(4 + 4 + len(params))
	binary.Write(rw, binary.BigEndian, length)
	binary.Write(rw, binary.BigEndian, protocolVersion)
	rw.WriteString(params)
	rw.Flush()

	// Read auth response
	msgType, body, err := readMessage(rw)
	require.NoError(t, err)

	if msgType == msgAuthentication {
		authType := int32(binary.BigEndian.Uint32(body[:4]))
		if authType == authMD5Password {
			// Send password
			var salt [4]byte
			copy(salt[:], body[4:8])
			hash := computeMD5Password(user, password, salt)
			hashBytes := append([]byte(hash), 0)
			writeMessage(rw, msgPassword, hashBytes)
			rw.Flush()

			// Read auth OK
			msgType, _, err = readMessage(rw)
			require.NoError(t, err)
			require.Equal(t, msgAuthentication, msgType)
		}
	}

	// Consume parameter status, backend key data, and ready for query
	for {
		msgType, _, err = readMessage(rw)
		require.NoError(t, err)
		if msgType == msgReadyForQuery {
			break
		}
	}

	return rw
}

func sendQuery(t *testing.T, rw *bufio.ReadWriter, sql string) ([][]string, string) {
	t.Helper()

	queryBytes := append([]byte(sql), 0)
	writeMessage(rw, msgQuery, queryBytes)
	rw.Flush()

	var rows [][]string
	var tag string

	for {
		msgType, body, err := readMessage(rw)
		require.NoError(t, err)

		switch msgType {
		case msgRowDescription:
			// Skip column metadata
		case msgDataRow:
			numCols := int(binary.BigEndian.Uint16(body[:2]))
			row := make([]string, numCols)
			offset := 2
			for i := 0; i < numCols; i++ {
				colLen := int32(binary.BigEndian.Uint32(body[offset : offset+4]))
				offset += 4
				if colLen >= 0 {
					row[i] = string(body[offset : offset+int(colLen)])
					offset += int(colLen)
				}
			}
			rows = append(rows, row)
		case msgCommandComplete:
			tag = string(body[:len(body)-1]) // strip null
		case msgReadyForQuery:
			return rows, tag
		case msgErrorResponse:
			// Parse error message
			errMsg := parseErrorMessage(body)
			t.Fatalf("SQL error: %s", errMsg)
		}
	}
}

func parseErrorMessage(body []byte) string {
	for i := 0; i < len(body)-1; {
		code := body[i]
		i++
		// Find null terminator
		end := i
		for end < len(body) && body[end] != 0 {
			end++
		}
		if code == errFieldMessage {
			return string(body[i:end])
		}
		i = end + 1
	}
	return "unknown error"
}

func TestPostgresService_Connect_TrustAuth(t *testing.T) {
	cfg := &config.ServiceConfig{
		Name:   "testdb",
		Type:   "postgres",
		Listen: "127.0.0.1:0",
	}

	_, addr := startTestService(t, cfg)
	rw := connectPG(t, addr, "anyuser", "anydb", "")
	require.NotNil(t, rw)
}

func TestPostgresService_Connect_MD5Auth(t *testing.T) {
	cfg := &config.ServiceConfig{
		Name:   "testdb",
		Type:   "postgres",
		Listen: "127.0.0.1:0",
		Auth: &config.AuthConfig{
			Users:    map[string]string{"app": "secret"},
			Database: "myapp",
		},
	}

	_, addr := startTestService(t, cfg)
	rw := connectPG(t, addr, "app", "myapp", "secret")
	require.NotNil(t, rw)
}

func TestPostgresService_Query_Select(t *testing.T) {
	seed := int64(42)
	cfg := &config.ServiceConfig{
		Name:   "testdb",
		Type:   "postgres",
		Listen: "127.0.0.1:0",
		Tables: []*config.TableConfig{
			{
				Name: "user",
				Rows: 5,
				Seed: &seed,
				Columns: []*config.ColumnConfig{
					{Name: "id", Type: "uuid"},
					{Name: "name", Type: "name"},
				},
			},
		},
	}

	_, addr := startTestService(t, cfg)
	rw := connectPG(t, addr, "test", "testdb", "")

	rows, tag := sendQuery(t, rw, "SELECT * FROM users")
	require.Equal(t, "SELECT 5", tag)
	require.Len(t, rows, 5)
	require.Len(t, rows[0], 2) // id, name columns
}

func TestPostgresService_Query_InsertAndSelect(t *testing.T) {
	cfg := &config.ServiceConfig{
		Name:   "testdb",
		Type:   "postgres",
		Listen: "127.0.0.1:0",
		Tables: []*config.TableConfig{
			{
				Name: "user",
				Rows: 0,
				Columns: []*config.ColumnConfig{
					{Name: "id", Type: "uuid"},
					{Name: "name", Type: "name"},
				},
			},
		},
	}

	_, addr := startTestService(t, cfg)
	rw := connectPG(t, addr, "test", "testdb", "")

	// Insert
	_, tag := sendQuery(t, rw, "INSERT INTO users (id, name) VALUES ('abc-123', 'Alice')")
	require.Equal(t, "INSERT 0 1", tag)

	// Select
	rows, tag := sendQuery(t, rw, "SELECT * FROM users")
	require.Equal(t, "SELECT 1", tag)
	require.Len(t, rows, 1)
	require.Equal(t, "abc-123", rows[0][0])
	require.Equal(t, "Alice", rows[0][1])
}

func TestPostgresService_Query_MultipleQueries(t *testing.T) {
	seed := int64(42)
	cfg := &config.ServiceConfig{
		Name:   "testdb",
		Type:   "postgres",
		Listen: "127.0.0.1:0",
		Tables: []*config.TableConfig{
			{
				Name: "user",
				Rows: 3,
				Seed: &seed,
				Columns: []*config.ColumnConfig{
					{Name: "id", Type: "uuid"},
					{Name: "name", Type: "name"},
				},
			},
		},
	}

	_, addr := startTestService(t, cfg)
	rw := connectPG(t, addr, "test", "testdb", "")

	// First query
	rows1, _ := sendQuery(t, rw, "SELECT * FROM users")
	require.Len(t, rows1, 3)

	// Second query on same connection
	rows2, _ := sendQuery(t, rw, "SELECT * FROM users")
	require.Len(t, rows2, 3)
}
