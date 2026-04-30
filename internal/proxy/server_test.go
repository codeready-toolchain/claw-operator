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
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
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
		assert.NotNil(t, srv.proxy)
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

	t.Run("should fail when cert is not a CA", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		tmpl := &x509.Certificate{
			SerialNumber: serial,
			Subject:      pkix.Name{CommonName: "Not A CA"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
		}
		certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		require.NoError(t, err)
		nonCACert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
		nonCAKeyDER, err := x509.MarshalECPrivateKey(key)
		require.NoError(t, err)
		nonCAKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: nonCAKeyDER})

		cfg := &Config{Routes: []Route{}}
		_, err = NewServer(cfg, nonCACert, nonCAKeyPEM, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "IsCA=false")
	})

	t.Run("should fail when key does not match cert", func(t *testing.T) {
		otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		otherKeyDER, err := x509.MarshalECPrivateKey(otherKey)
		require.NoError(t, err)
		mismatchedKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: otherKeyDER})

		cfg := &Config{Routes: []Route{}}
		_, err = NewServer(cfg, certPEM, mismatchedKeyPEM, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match")
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
		assert.NotNil(t, srv.proxy)
	})
}

func TestBuildRootCAPool(t *testing.T) {
	logger := slog.Default()

	t.Run("should include system CAs by default", func(t *testing.T) {
		cfg := &Config{Routes: []Route{}}
		pool := buildRootCAPool(cfg, logger)
		require.NotNil(t, pool)
	})

	t.Run("should add route CA certs to pool", func(t *testing.T) {
		caPEM, _ := generateTestCA(t)
		encoded := base64.StdEncoding.EncodeToString(caPEM)

		cfg := &Config{
			Routes: []Route{
				{Domain: "api.example.com:6443", Injector: "none", CACert: encoded},
			},
		}
		pool := buildRootCAPool(cfg, logger)
		require.NotNil(t, pool)

		certPEM, keyPEM := generateTestCA(t)
		srv, err := NewServer(cfg, certPEM, keyPEM, logger)
		require.NoError(t, err)
		require.NotNil(t, srv)
	})

	t.Run("should skip invalid base64 without crashing", func(t *testing.T) {
		cfg := &Config{
			Routes: []Route{
				{Domain: "bad.example.com:6443", Injector: "kubernetes", CACert: "not-valid-base64!!!"},
			},
		}
		pool := buildRootCAPool(cfg, logger)
		require.NotNil(t, pool)
	})

	t.Run("should skip non-PEM content without crashing", func(t *testing.T) {
		encoded := base64.StdEncoding.EncodeToString([]byte("not a PEM cert"))
		cfg := &Config{
			Routes: []Route{
				{Domain: "bad.example.com:6443", Injector: "kubernetes", CACert: encoded},
			},
		}
		pool := buildRootCAPool(cfg, logger)
		require.NotNil(t, pool)
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

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = fmt.Fprintf(conn, "CONNECT unknown.evil.com:443 HTTP/1.1\r\nHost: unknown.evil.com:443\r\n\r\n")
	require.NoError(t, err)

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "domain not allowed")
}

func TestServerConnectAllowsKnownDomain(t *testing.T) {
	certPEM, keyPEM := generateTestCA(t)
	cfg := &Config{
		Routes: []Route{
			{Domain: "api.allowed.com", Injector: "bearer", EnvVar: "CRED_TEST"},
		},
	}
	srv, err := NewServer(cfg, certPEM, keyPEM, slog.Default())
	require.NoError(t, err)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = fmt.Fprintf(conn, "CONNECT api.allowed.com:443 HTTP/1.1\r\nHost: api.allowed.com:443\r\n\r\n")
	require.NoError(t, err)

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	// 200 means CONNECT was accepted and MITM is set up
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestServerConnectDirectForNoneRoute(t *testing.T) {
	certPEM, keyPEM := generateTestCA(t)

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprintf(w, "upstream-ok")
	}))
	defer upstream.Close()

	_, port, err := net.SplitHostPort(upstream.Listener.Addr().String())
	require.NoError(t, err)

	cfg := &Config{
		Routes: []Route{
			{Domain: "127.0.0.1", Injector: "none"},
		},
	}
	srv, err := NewServer(cfg, certPEM, keyPEM, slog.Default())
	require.NoError(t, err)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	target := fmt.Sprintf("127.0.0.1:%s", port)
	_, err = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	require.NoError(t, err)

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// For a direct tunnel, we do TLS against the UPSTREAM cert, not the proxy CA.
	// This proves the proxy is NOT doing MITM.
	tlsConn := tls.Client(conn, &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // test against self-signed upstream
		MinVersion:         tls.VersionTLS12,
	})
	require.NoError(t, tlsConn.Handshake())

	// Verify the cert is from the upstream server, not the proxy CA
	certBlock, _ := pem.Decode(certPEM)
	require.NotNil(t, certBlock)
	caCert, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)

	state := tlsConn.ConnectionState()
	require.NotEmpty(t, state.PeerCertificates)
	upstreamCert := state.PeerCertificates[0]
	assert.NotEqual(t, caCert.Subject.CommonName, upstreamCert.Issuer.CommonName,
		"should see upstream cert, not proxy-generated MITM cert")

	req, err := http.NewRequest(http.MethodGet, "https://127.0.0.1/test", nil)
	require.NoError(t, err)
	require.NoError(t, req.Write(tlsConn))

	resp, err = http.ReadResponse(bufio.NewReader(tlsConn), req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "upstream-ok", string(body))
}

func TestServerConnectMITMForNoneRouteWithAllowedPaths(t *testing.T) {
	certPEM, keyPEM := generateTestCA(t)
	cfg := &Config{
		Routes: []Route{
			{Domain: "raw.githubusercontent.com", Injector: "none", AllowedPaths: []string{"/BerriAI/litellm/"}},
		},
	}
	srv, err := NewServer(cfg, certPEM, keyPEM, slog.Default())
	require.NoError(t, err)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = fmt.Fprintf(conn, "CONNECT raw.githubusercontent.com:443 HTTP/1.1\r\nHost: raw.githubusercontent.com:443\r\n\r\n")
	require.NoError(t, err)

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// For a MITM route, the proxy generates a cert signed by the proxy CA.
	certBlock, _ := pem.Decode(certPEM)
	require.NotNil(t, certBlock)
	caCert, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	tlsConn := tls.Client(conn, &tls.Config{
		RootCAs:    caPool,
		ServerName: "raw.githubusercontent.com",
		MinVersion: tls.VersionTLS12,
	})
	require.NoError(t, tlsConn.Handshake(), "should succeed with proxy CA — proves MITM is active")
}

func TestServerMITMCredentialInjection(t *testing.T) {
	certPEM, keyPEM := generateTestCA(t)

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"auth":"%s"}`, r.Header.Get("Authorization"))
	}))
	defer upstream.Close()

	t.Setenv("CRED_BEARER_TEST", "test-token-12345")

	_, port, err := net.SplitHostPort(upstream.Listener.Addr().String())
	require.NoError(t, err)

	cfg := &Config{
		Routes: []Route{
			{Domain: "127.0.0.1", Injector: "bearer", EnvVar: "CRED_BEARER_TEST"},
		},
	}
	srv, err := NewServer(cfg, certPEM, keyPEM, slog.Default())
	require.NoError(t, err)

	// Point the proxy's transport at the upstream's CA so it trusts the test server
	srv.proxy.Tr.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec // test only

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	target := fmt.Sprintf("127.0.0.1:%s", port)
	_, err = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	require.NoError(t, err)

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Parse the CA to build a TLS client that trusts MITM certs
	certBlock, _ := pem.Decode(certPEM)
	require.NotNil(t, certBlock)
	caCert, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	tlsConn := tls.Client(conn, &tls.Config{
		RootCAs:    caPool,
		ServerName: "127.0.0.1",
		MinVersion: tls.VersionTLS12,
	})
	require.NoError(t, tlsConn.Handshake())

	req, err := http.NewRequest(http.MethodGet, "https://127.0.0.1/test", nil)
	require.NoError(t, err)
	require.NoError(t, req.Write(tlsConn))

	resp, err = http.ReadResponse(bufio.NewReader(tlsConn), req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), "Bearer test-token-12345")
}

func TestTokenVendingResponse(t *testing.T) {
	data := TokenVendingResponse()
	assert.Contains(t, string(data), "claw-proxy-vended-token")
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
