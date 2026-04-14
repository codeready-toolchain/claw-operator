/*
Copyright 2026 Red Hat.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestCA(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	certBuf := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	keyBuf := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certBuf, keyBuf
}

func TestNewServer(t *testing.T) {
	certPEM, keyPEM := generateTestCA(t)
	logger := slog.Default()

	t.Run("should create server with valid config and CA", func(t *testing.T) {
		cfg := &Config{
			Routes: []Route{
				{Domain: "api.example.com", Injector: "bearer", EnvVar: "CRED_TEST"},
			},
		}
		srv, err := NewServer(cfg, certPEM, keyPEM, logger)
		require.NoError(t, err)
		require.NotNil(t, srv)
		assert.Len(t, srv.injectors, 1)
	})

	t.Run("should fail with invalid CA cert PEM", func(t *testing.T) {
		cfg := &Config{Routes: []Route{}}
		_, err := NewServer(cfg, []byte("not-pem"), keyPEM, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse CA")
	})

	t.Run("should fail with invalid CA key PEM", func(t *testing.T) {
		cfg := &Config{Routes: []Route{}}
		_, err := NewServer(cfg, certPEM, []byte("not-pem"), logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse CA")
	})

	t.Run("should fail with invalid injector config", func(t *testing.T) {
		cfg := &Config{
			Routes: []Route{
				{Domain: "x.com", Injector: "bearer"}, // missing envVar
			},
		}
		_, err := NewServer(cfg, certPEM, keyPEM, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create injector")
	})

	t.Run("should create server with zero routes", func(t *testing.T) {
		cfg := &Config{Routes: []Route{}}
		srv, err := NewServer(cfg, certPEM, keyPEM, logger)
		require.NoError(t, err)
		assert.Empty(t, srv.injectors)
	})
}

func TestServerHealthz(t *testing.T) {
	certPEM, keyPEM := generateTestCA(t)
	cfg := &Config{Routes: []Route{}}
	srv, err := NewServer(cfg, certPEM, keyPEM, slog.Default())
	require.NoError(t, err)

	handler := srv.Handler()

	t.Run("should return 200 on GET /healthz", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "ok\n", rr.Body.String())
	})

	t.Run("should return 405 on non-CONNECT non-healthz", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/other", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	})

	t.Run("should return 405 on POST /some-path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/some-path", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
		body, _ := io.ReadAll(rr.Body)
		assert.Contains(t, string(body), "proxy accepts CONNECT or /healthz only")
	})
}

func TestServerConnectBlocksUnknownDomain(t *testing.T) {
	certPEM, keyPEM := generateTestCA(t)
	cfg := &Config{
		Routes: []Route{
			{Domain: "api.allowed.com", Injector: "bearer", EnvVar: "CRED_TEST"},
		},
	}
	srv, err := NewServer(cfg, certPEM, keyPEM, slog.Default())
	require.NoError(t, err)

	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodConnect, "unknown.evil.com:443", nil)
	req.Host = "unknown.evil.com:443"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
	body, _ := io.ReadAll(rr.Body)
	assert.Contains(t, string(body), "domain not allowed")
}

func TestLeafCertGeneration(t *testing.T) {
	certPEM, keyPEM := generateTestCA(t)
	cfg := &Config{Routes: []Route{}}
	srv, err := NewServer(cfg, certPEM, keyPEM, slog.Default())
	require.NoError(t, err)

	cert, err := srv.getOrCreateLeafCert("api.example.com")
	require.NoError(t, err)
	require.NotNil(t, cert)
	require.Len(t, cert.Certificate, 1)

	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, "api.example.com", parsed.Subject.CommonName)
	assert.Contains(t, parsed.DNSNames, "api.example.com")

	// Verify caching: second call returns the same cert
	cert2, err := srv.getOrCreateLeafCert("api.example.com")
	require.NoError(t, err)
	assert.Equal(t, cert, cert2, "cached cert should be returned")

	// Different host gets a different cert
	cert3, err := srv.getOrCreateLeafCert("other.example.com")
	require.NoError(t, err)
	require.NotNil(t, cert3)
	assert.NotEqual(t, cert, cert3)
}

func TestTokenVendingResponse(t *testing.T) {
	data := TokenVendingResponse()
	assert.Contains(t, string(data), "openclaw-proxy-vended-token")
	assert.Contains(t, string(data), "Bearer")
}

func TestIsTokenVendingRequest(t *testing.T) {
	tests := []struct {
		name   string
		method string
		host   string
		path   string
		want   bool
	}{
		{
			name:   "valid token vending",
			method: http.MethodPost,
			host:   "oauth2.googleapis.com",
			path:   "/token",
			want:   true,
		},
		{
			name:   "wrong method",
			method: http.MethodGet,
			host:   "oauth2.googleapis.com",
			path:   "/token",
			want:   false,
		},
		{
			name:   "wrong host",
			method: http.MethodPost,
			host:   "api.googleapis.com",
			path:   "/token",
			want:   false,
		},
		{
			name:   "wrong path",
			method: http.MethodPost,
			host:   "oauth2.googleapis.com",
			path:   "/other",
			want:   false,
		},
		{
			name:   "valid with explicit port",
			method: http.MethodPost,
			host:   "oauth2.googleapis.com:443",
			path:   "/token",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(tt.method, "https://"+tt.host+tt.path, nil)
			req.Host = tt.host
			assert.Equal(t, tt.want, isTokenVendingRequest(req))
		})
	}
}
