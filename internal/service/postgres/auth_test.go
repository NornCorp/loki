package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComputeMD5Password(t *testing.T) {
	// Known test vector:
	// user="app", password="secret", salt=[0x01, 0x02, 0x03, 0x04]
	// inner = md5("secretapp") = hex
	// outer = md5(innerHex + salt) = hex
	// result = "md5" + outerHex
	salt := [4]byte{0x01, 0x02, 0x03, 0x04}
	result := computeMD5Password("app", "secret", salt)

	// Should start with "md5" prefix
	require.True(t, len(result) > 3)
	require.Equal(t, "md5", result[:3])

	// Should be md5 prefix + 32 hex chars
	require.Len(t, result, 35) // "md5" + 32 hex chars

	// Same inputs should produce same output
	result2 := computeMD5Password("app", "secret", salt)
	require.Equal(t, result, result2)

	// Different salt should produce different output
	salt2 := [4]byte{0x05, 0x06, 0x07, 0x08}
	result3 := computeMD5Password("app", "secret", salt2)
	require.NotEqual(t, result, result3)
}

func TestNewAuthenticator(t *testing.T) {
	auth := NewAuthenticator(map[string]string{"app": "secret"}, "mydb")
	require.NotNil(t, auth)
	require.Equal(t, "mydb", auth.database)
	require.Equal(t, "secret", auth.users["app"])
}

func TestNewAuthenticator_NoUsers(t *testing.T) {
	auth := NewAuthenticator(nil, "")
	require.NotNil(t, auth)
	require.Empty(t, auth.users)
}
