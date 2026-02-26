package postgres

import (
	"bufio"
	"crypto/md5"
	"crypto/rand"
	"fmt"
)

// Authenticator handles PostgreSQL client authentication.
type Authenticator struct {
	users    map[string]string // username -> password
	database string
}

// NewAuthenticator creates a new authenticator.
func NewAuthenticator(users map[string]string, database string) *Authenticator {
	return &Authenticator{
		users:    users,
		database: database,
	}
}

// Authenticate handles the authentication handshake.
// Returns the authenticated username or an error.
func (a *Authenticator) Authenticate(rw *bufio.ReadWriter, startup *StartupMessage) (string, error) {
	user := startup.Parameters["user"]
	database := startup.Parameters["database"]

	// Check database name if configured
	if a.database != "" && database != a.database {
		writeErrorResponse(rw, "FATAL", "3D000",
			fmt.Sprintf("database %q does not exist", database))
		rw.Flush()
		return "", fmt.Errorf("unknown database: %s", database)
	}

	// If no users configured, accept all connections (trust auth)
	if len(a.users) == 0 {
		if err := writeAuthOk(rw); err != nil {
			return "", err
		}
		return user, nil
	}

	// Check if user exists
	password, ok := a.users[user]
	if !ok {
		writeErrorResponse(rw, "FATAL", "28P01",
			fmt.Sprintf("password authentication failed for user %q", user))
		rw.Flush()
		return "", fmt.Errorf("unknown user: %s", user)
	}

	// Generate salt and send MD5 challenge
	var salt [4]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	if err := writeAuthMD5(rw, salt); err != nil {
		return "", fmt.Errorf("send auth challenge: %w", err)
	}
	if err := rw.Flush(); err != nil {
		return "", fmt.Errorf("flush auth challenge: %w", err)
	}

	// Read password response
	msgType, body, err := readMessage(rw)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if msgType != msgPassword {
		return "", fmt.Errorf("expected password message, got %c", msgType)
	}

	// Parse password (null-terminated string)
	clientHash := string(body[:len(body)-1])

	// Compute expected MD5 hash
	expected := computeMD5Password(user, password, salt)

	if clientHash != expected {
		writeErrorResponse(rw, "FATAL", "28P01",
			fmt.Sprintf("password authentication failed for user %q", user))
		rw.Flush()
		return "", fmt.Errorf("authentication failed for user: %s", user)
	}

	if err := writeAuthOk(rw); err != nil {
		return "", err
	}

	return user, nil
}

// computeMD5Password computes the PostgreSQL MD5 password hash.
// Format: "md5" + md5(md5(password + username) + salt)
func computeMD5Password(user, password string, salt [4]byte) string {
	inner := md5.Sum([]byte(password + user))
	innerHex := fmt.Sprintf("%x", inner)
	outer := md5.Sum(append([]byte(innerHex), salt[:]...))
	return fmt.Sprintf("md5%x", outer)
}
