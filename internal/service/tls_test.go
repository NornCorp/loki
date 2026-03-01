package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jumppad-labs/polymorph/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBuildTLSConfig_Nil(t *testing.T) {
	tlsCfg, err := BuildTLSConfig(nil)
	require.NoError(t, err)
	require.Nil(t, tlsCfg)
}

func TestBuildTLSConfig_Auto(t *testing.T) {
	cfg := &config.TLSConfig{}

	tlsCfg, err := BuildTLSConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)
	require.Len(t, tlsCfg.Certificates, 1)

	// Verify the generated certificate is valid and has expected properties
	leaf, err := x509.ParseCertificate(tlsCfg.Certificates[0].Certificate[0])
	require.NoError(t, err)
	require.Equal(t, "Polymorph Auto-TLS", leaf.Subject.Organization[0])
	require.Contains(t, leaf.DNSNames, "localhost")
	require.Contains(t, leaf.IPAddresses, net.IPv4(127, 0, 0, 1).To4())
	require.True(t, leaf.NotAfter.After(time.Now()))
	require.True(t, leaf.NotBefore.Before(time.Now().Add(time.Minute)))
}

func TestBuildTLSConfig_CertKey(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	generateTestCertFiles(t, certPath, keyPath)

	cfg := &config.TLSConfig{
		Cert: certPath,
		Key:  keyPath,
	}

	tlsCfg, err := BuildTLSConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)
	require.Len(t, tlsCfg.Certificates, 1)
}

func TestBuildTLSConfig_InvalidCert(t *testing.T) {
	cfg := &config.TLSConfig{
		Cert: "/nonexistent/cert.pem",
		Key:  "/nonexistent/key.pem",
	}

	tlsCfg, err := BuildTLSConfig(cfg)
	require.Error(t, err)
	require.Nil(t, tlsCfg)
	require.Contains(t, err.Error(), "failed to load TLS certificate")
}

func TestBuildTLSConfig_MismatchedCertKey(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	otherKeyPath := filepath.Join(dir, "other_key.pem")

	generateTestCertFiles(t, certPath, keyPath)

	// Generate a different key
	key2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	keyDER2, err := x509.MarshalECPrivateKey(key2)
	require.NoError(t, err)
	keyPEM2 := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER2})
	require.NoError(t, os.WriteFile(otherKeyPath, keyPEM2, 0600))

	cfg := &config.TLSConfig{
		Cert: certPath,
		Key:  otherKeyPath,
	}

	tlsCfg, err := BuildTLSConfig(cfg)
	require.Error(t, err)
	require.Nil(t, tlsCfg)
	require.Contains(t, err.Error(), "failed to load TLS certificate")
}

func TestWrapListenerTLS_NilConfig(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	wrapped, err := WrapListenerTLS(ln, nil)
	require.NoError(t, err)
	require.Equal(t, ln, wrapped, "listener should be returned unchanged when config is nil")
}

func TestWrapListenerTLS_Auto(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	cfg := &config.TLSConfig{}

	wrapped, err := WrapListenerTLS(ln, cfg)
	require.NoError(t, err)
	require.NotNil(t, wrapped)
	require.NotEqual(t, ln, wrapped, "wrapped listener should differ from original")

	addr := wrapped.Addr().String()

	// Start a simple HTTPS server on the wrapped listener
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("tls-ok"))
		}),
	}
	go server.Serve(wrapped)
	defer server.Close()

	time.Sleep(10 * time.Millisecond)

	// Make an HTTPS request with InsecureSkipVerify (self-signed cert)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := client.Get("https://" + addr + "/test")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "tls-ok", string(body))
}

func TestWrapListenerTLS_InvalidConfig(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	cfg := &config.TLSConfig{
		Cert: "/nonexistent/cert.pem",
		Key:  "/nonexistent/key.pem",
	}

	wrapped, err := WrapListenerTLS(ln, cfg)
	require.Error(t, err)
	require.Nil(t, wrapped)
}

func TestHTTPService_TLS_Integration(t *testing.T) {
	// Integration test: mirrors what HTTPService.Start does internally.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	cfg := &config.TLSConfig{}

	tlsLn, err := WrapListenerTLS(ln, cfg)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	server := &http.Server{Handler: mux}
	go server.Serve(tlsLn)
	defer server.Close()

	time.Sleep(10 * time.Millisecond)

	addr := tlsLn.Addr().String()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	t.Run("HTTPS GET /health", func(t *testing.T) {
		resp, err := client.Get("https://" + addr + "/health")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, "application/json", resp.Header.Get("Content-Type"))

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"status":"healthy"}`, string(body))
	})

	t.Run("plain HTTP gets bad response from TLS listener", func(t *testing.T) {
		plainClient := &http.Client{Timeout: 2 * time.Second}
		resp, err := plainClient.Get("http://" + addr + "/health")
		if err != nil {
			return // connection error is acceptable
		}
		defer resp.Body.Close()
		// If we got a response at all, it should be the TLS error page (400 Bad Request)
		require.NotEqual(t, http.StatusOK, resp.StatusCode)
	})
}

// generateTestCertFiles creates a self-signed certificate and key pair
// and writes them as PEM files to the given paths.
func generateTestCertFiles(t *testing.T, certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	require.NoError(t, os.WriteFile(certPath, certPEM, 0644))

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0600))
}
