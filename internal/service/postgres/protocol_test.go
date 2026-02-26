package postgres

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadStartupMessage(t *testing.T) {
	var buf bytes.Buffer

	// Build startup message: length(4) + version(4) + params + null terminator
	params := []byte("user\x00testuser\x00database\x00testdb\x00\x00")
	length := int32(4 + 4 + len(params)) // length field + version + params
	binary.Write(&buf, binary.BigEndian, length)
	binary.Write(&buf, binary.BigEndian, protocolVersion)
	buf.Write(params)

	msg, isSSL, err := readStartupMessage(&buf)
	require.NoError(t, err)
	require.False(t, isSSL)
	require.Equal(t, protocolVersion, msg.ProtocolVersion)
	require.Equal(t, "testuser", msg.Parameters["user"])
	require.Equal(t, "testdb", msg.Parameters["database"])
}

func TestReadStartupMessage_SSLRequest(t *testing.T) {
	var buf bytes.Buffer

	binary.Write(&buf, binary.BigEndian, int32(8)) // length
	binary.Write(&buf, binary.BigEndian, sslRequestCode)

	_, isSSL, err := readStartupMessage(&buf)
	require.NoError(t, err)
	require.True(t, isSSL)
}

func TestReadWriteMessage(t *testing.T) {
	var buf bytes.Buffer

	// Write a query message
	query := "SELECT 1\x00"
	err := writeMessage(&buf, msgQuery, []byte(query))
	require.NoError(t, err)

	// Read it back
	msgType, body, err := readMessage(&buf)
	require.NoError(t, err)
	require.Equal(t, msgQuery, msgType)
	require.Equal(t, query, string(body))
}

func TestWriteRowDescription(t *testing.T) {
	var buf bytes.Buffer

	cols := []ColumnDef{
		{Name: "id", TypeOID: oidUUID},
		{Name: "name", TypeOID: oidText},
	}
	err := writeRowDescription(&buf, cols)
	require.NoError(t, err)

	// Read it back as a raw message
	msgType, body, err := readMessage(&buf)
	require.NoError(t, err)
	require.Equal(t, msgRowDescription, msgType)

	// Parse number of fields
	numFields := binary.BigEndian.Uint16(body[:2])
	require.Equal(t, uint16(2), numFields)
}

func TestWriteDataRow(t *testing.T) {
	var buf bytes.Buffer

	err := writeDataRow(&buf, []string{"abc-123", "Alice"})
	require.NoError(t, err)

	msgType, body, err := readMessage(&buf)
	require.NoError(t, err)
	require.Equal(t, msgDataRow, msgType)

	// Parse number of columns
	numCols := binary.BigEndian.Uint16(body[:2])
	require.Equal(t, uint16(2), numCols)
}

func TestWriteCommandComplete(t *testing.T) {
	var buf bytes.Buffer

	err := writeCommandComplete(&buf, "SELECT 5")
	require.NoError(t, err)

	msgType, body, err := readMessage(&buf)
	require.NoError(t, err)
	require.Equal(t, msgCommandComplete, msgType)
	require.Equal(t, "SELECT 5\x00", string(body))
}

func TestWriteErrorResponse(t *testing.T) {
	var buf bytes.Buffer

	err := writeErrorResponse(&buf, "ERROR", "42601", "syntax error")
	require.NoError(t, err)

	msgType, body, err := readMessage(&buf)
	require.NoError(t, err)
	require.Equal(t, msgErrorResponse, msgType)
	require.Contains(t, string(body), "syntax error")
}

func TestTypeOIDForFakeType(t *testing.T) {
	tests := []struct {
		fakeType string
		expected int32
	}{
		{"uuid", oidUUID},
		{"int", oidInt4},
		{"bool", oidBool},
		{"decimal", oidFloat8},
		{"name", oidText},
		{"email", oidText},
		{"unknown", oidText},
	}
	for _, tt := range tests {
		t.Run(tt.fakeType, func(t *testing.T) {
			require.Equal(t, tt.expected, typeOIDForFakeType(tt.fakeType))
		})
	}
}
