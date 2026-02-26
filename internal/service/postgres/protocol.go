package postgres

import (
	"encoding/binary"
	"fmt"
	"io"
)

// PostgreSQL protocol version 3.0
const protocolVersion int32 = 196608 // (3 << 16) | 0

// SSL request code
const sslRequestCode int32 = 80877103

// Frontend message types (client -> server)
const (
	msgPassword  byte = 'p'
	msgQuery     byte = 'Q'
	msgTerminate byte = 'X'
)

// Backend message types (server -> client)
const (
	msgAuthentication  byte = 'R'
	msgParameterStatus byte = 'S'
	msgBackendKeyData  byte = 'K'
	msgReadyForQuery   byte = 'Z'
	msgRowDescription  byte = 'T'
	msgDataRow         byte = 'D'
	msgCommandComplete byte = 'C'
	msgErrorResponse   byte = 'E'
)

// Authentication types
const (
	authOk          int32 = 0
	authMD5Password int32 = 5
)

// Transaction status
const (
	txIdle byte = 'I'
)

// Error field codes
const (
	errFieldSeverity byte = 'S'
	errFieldCode     byte = 'C'
	errFieldMessage  byte = 'M'
)

// Common PostgreSQL type OIDs
const (
	oidText      int32 = 25
	oidInt4      int32 = 23
	oidBool      int32 = 16
	oidFloat8    int32 = 701
	oidUUID      int32 = 2950
	oidTimestamp int32 = 1114
)

// StartupMessage represents the initial client message.
type StartupMessage struct {
	ProtocolVersion int32
	Parameters      map[string]string
}

// readStartupMessage reads the initial startup message from the client.
// Returns the startup message and whether it was an SSL request.
func readStartupMessage(r io.Reader) (*StartupMessage, bool, error) {
	var length int32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, false, fmt.Errorf("read startup length: %w", err)
	}
	if length < 8 || length > 10000 {
		return nil, false, fmt.Errorf("invalid startup message length: %d", length)
	}

	buf := make([]byte, length-4)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, false, fmt.Errorf("read startup body: %w", err)
	}

	version := int32(binary.BigEndian.Uint32(buf[:4]))

	if version == sslRequestCode {
		return nil, true, nil
	}
	if version != protocolVersion {
		return nil, false, fmt.Errorf("unsupported protocol version: %d", version)
	}

	params := make(map[string]string)
	data := buf[4:]
	for len(data) > 1 {
		key, rest, err := readCString(data)
		if err != nil || key == "" {
			break
		}
		data = rest

		value, rest, err := readCString(data)
		if err != nil {
			break
		}
		data = rest
		params[key] = value
	}

	return &StartupMessage{
		ProtocolVersion: version,
		Parameters:      params,
	}, false, nil
}

func readCString(data []byte) (string, []byte, error) {
	for i, b := range data {
		if b == 0 {
			return string(data[:i]), data[i+1:], nil
		}
	}
	return "", nil, fmt.Errorf("no null terminator found")
}

// readMessage reads a typed message from the client.
func readMessage(r io.Reader) (byte, []byte, error) {
	var msgType [1]byte
	if _, err := io.ReadFull(r, msgType[:]); err != nil {
		return 0, nil, err
	}

	var length int32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return 0, nil, fmt.Errorf("read message length: %w", err)
	}
	if length < 4 {
		return 0, nil, fmt.Errorf("invalid message length: %d", length)
	}

	body := make([]byte, length-4)
	if length > 4 {
		if _, err := io.ReadFull(r, body); err != nil {
			return 0, nil, fmt.Errorf("read message body: %w", err)
		}
	}

	return msgType[0], body, nil
}

// writeMessage writes a typed message to the client.
func writeMessage(w io.Writer, msgType byte, data []byte) error {
	if _, err := w.Write([]byte{msgType}); err != nil {
		return err
	}
	length := int32(4 + len(data))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return err
	}
	if len(data) > 0 {
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

func writeAuthMD5(w io.Writer, salt [4]byte) error {
	var buf [8]byte
	binary.BigEndian.PutUint32(buf[:4], uint32(authMD5Password))
	copy(buf[4:], salt[:])
	return writeMessage(w, msgAuthentication, buf[:])
}

func writeAuthOk(w io.Writer) error {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:4], uint32(authOk))
	return writeMessage(w, msgAuthentication, buf[:])
}

func writeParameterStatus(w io.Writer, key, value string) error {
	data := append([]byte(key), 0)
	data = append(data, []byte(value)...)
	data = append(data, 0)
	return writeMessage(w, msgParameterStatus, data)
}

func writeBackendKeyData(w io.Writer, pid, secret int32) error {
	var buf [8]byte
	binary.BigEndian.PutUint32(buf[:4], uint32(pid))
	binary.BigEndian.PutUint32(buf[4:8], uint32(secret))
	return writeMessage(w, msgBackendKeyData, buf[:])
}

func writeReadyForQuery(w io.Writer, txStatus byte) error {
	return writeMessage(w, msgReadyForQuery, []byte{txStatus})
}

// ColumnDef describes a column in a query result set.
type ColumnDef struct {
	Name    string
	TypeOID int32
}

func writeRowDescription(w io.Writer, columns []ColumnDef) error {
	var data []byte
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, uint16(len(columns)))
	data = append(data, buf...)

	for _, col := range columns {
		data = append(data, []byte(col.Name)...)
		data = append(data, 0)
		// Table OID (0 = computed)
		data = append(data, 0, 0, 0, 0)
		// Column attribute number (0)
		data = append(data, 0, 0)
		// Data type OID
		oidBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(oidBuf, uint32(col.TypeOID))
		data = append(data, oidBuf...)
		// Data type size (-1 = variable)
		data = append(data, 0xFF, 0xFF)
		// Type modifier (-1 = default)
		data = append(data, 0xFF, 0xFF, 0xFF, 0xFF)
		// Format code (0 = text)
		data = append(data, 0, 0)
	}

	return writeMessage(w, msgRowDescription, data)
}

func writeDataRow(w io.Writer, values []string) error {
	var data []byte
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, uint16(len(values)))
	data = append(data, buf...)

	for _, val := range values {
		valBytes := []byte(val)
		lenBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(lenBuf, uint32(len(valBytes)))
		data = append(data, lenBuf...)
		data = append(data, valBytes...)
	}

	return writeMessage(w, msgDataRow, data)
}

func writeCommandComplete(w io.Writer, tag string) error {
	data := append([]byte(tag), 0)
	return writeMessage(w, msgCommandComplete, data)
}

func writeErrorResponse(w io.Writer, severity, code, message string) error {
	var data []byte
	data = append(data, errFieldSeverity)
	data = append(data, []byte(severity)...)
	data = append(data, 0)
	data = append(data, errFieldCode)
	data = append(data, []byte(code)...)
	data = append(data, 0)
	data = append(data, errFieldMessage)
	data = append(data, []byte(message)...)
	data = append(data, 0)
	data = append(data, 0) // terminator
	return writeMessage(w, msgErrorResponse, data)
}

// typeOIDForFakeType maps fake data types to PostgreSQL type OIDs.
func typeOIDForFakeType(fakeType string) int32 {
	switch fakeType {
	case "uuid":
		return oidUUID
	case "int":
		return oidInt4
	case "bool":
		return oidBool
	case "decimal", "float":
		return oidFloat8
	case "date", "datetime":
		return oidTimestamp
	default:
		return oidText
	}
}
