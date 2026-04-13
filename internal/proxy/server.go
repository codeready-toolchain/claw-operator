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
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server is the CONNECT/MITM credential-injecting forward proxy.
type Server struct {
	config    *Config
	caCert    *x509.Certificate
	caKey     *ecdsa.PrivateKey
	injectors map[string]Injector // domain -> injector
	logger    *slog.Logger

	certCache   map[string]*tls.Certificate
	certCacheMu sync.RWMutex
}

// NewServer creates a proxy server from the given config and CA materials.
func NewServer(cfg *Config, caCertPEM, caKeyPEM []byte, logger *slog.Logger) (*Server, error) {
	caCert, caKey, err := parseCA(caCertPEM, caKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse CA: %w", err)
	}

	injectors := make(map[string]Injector, len(cfg.Routes))
	for i := range cfg.Routes {
		inj, err := NewInjector(&cfg.Routes[i])
		if err != nil {
			return nil, fmt.Errorf("create injector for domain %s: %w", cfg.Routes[i].Domain, err)
		}
		injectors[cfg.Routes[i].Domain] = inj
	}

	return &Server{
		config:    cfg,
		caCert:    caCert,
		caKey:     caKey,
		injectors: injectors,
		logger:    logger,
		certCache: make(map[string]*tls.Certificate),
	}, nil
}

func parseCA(certPEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, nil, fmt.Errorf("failed to decode CA certificate PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA certificate: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("failed to decode CA key PEM")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA key: %w", err)
	}

	return cert, key, nil
}

// Handler returns an http.Handler for the proxy server.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			s.handleConnect(w, r)
			return
		}

		// Plain HTTP: health endpoint
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok\n"))
			return
		}

		http.Error(w, `{"error":"proxy accepts CONNECT or /healthz only"}`, http.StatusMethodNotAllowed)
	})
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if !strings.Contains(host, ":") {
		host += ":443"
	}
	hostOnly := strings.Split(host, ":")[0]

	route := s.config.MatchRoute(hostOnly)
	if route == nil {
		s.logger.Warn("blocked CONNECT to unknown domain", "host", hostOnly)
		http.Error(w, `{"error":"domain not allowed"}`, http.StatusForbidden)
		return
	}

	// Hijack the connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		s.logger.Error("hijack failed", "error", err)
		return
	}
	defer func() { _ = clientConn.Close() }()

	// Send 200 Connection Established
	_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Generate leaf cert for the target domain
	leafCert, err := s.getOrCreateLeafCert(hostOnly)
	if err != nil {
		s.logger.Error("leaf cert generation failed", "host", hostOnly, "error", err)
		return
	}

	// TLS-terminate the client side
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*leafCert},
		MinVersion:   tls.VersionTLS12,
	}
	tlsConn := tls.Server(clientConn, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		s.logger.Error("TLS handshake with client failed", "host", hostOnly, "error", err)
		return
	}
	defer func() { _ = tlsConn.Close() }()

	// Read the decrypted HTTP request
	reader := bufio.NewReader(tlsConn)
	for {
		req, err := http.ReadRequest(reader)
		if err != nil {
			if err != io.EOF {
				s.logger.Error("read request from client failed", "error", err)
			}
			return
		}

		s.proxyRequest(tlsConn, req, route, host)
	}
}

func (s *Server) proxyRequest(clientConn net.Conn, req *http.Request, route *Route, targetHost string) {
	// Strip all client-supplied auth headers (defense in depth)
	StripAuthHeaders(req)

	// Token vending for GCP: intercept token endpoint requests
	if route.Injector == "gcp" && isTokenVendingRequest(req) {
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(string(TokenVendingResponse()))),
		}
		_ = resp.Write(clientConn)
		return
	}

	// Look up injector and inject credentials
	inj := s.injectors[route.Domain]
	if inj == nil {
		s.logger.Error("no injector for matched route", "domain", route.Domain)
		s.writeErrorResponse(clientConn, http.StatusBadGateway, "credential injection failed")
		return
	}

	if err := inj.Inject(req); err != nil {
		s.logger.Error("credential injection failed", "domain", route.Domain, "error", "[REDACTED]")
		s.writeErrorResponse(clientConn, http.StatusBadGateway, "credential injection failed")
		return
	}

	// Set the full URL for the upstream request
	req.URL.Scheme = "https"
	req.URL.Host = targetHost
	req.RequestURI = ""

	// Forward to upstream via HTTPS
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: (&net.Dialer{
			Timeout: 15 * time.Second,
		}).DialContext,
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		s.logger.Error("upstream request failed", "host", targetHost, "error", err)
		s.writeErrorResponse(clientConn, http.StatusBadGateway, "upstream connection failed")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	_ = resp.Write(clientConn)
}

func (s *Server) writeErrorResponse(conn net.Conn, status int, message string) {
	body := fmt.Sprintf(`{"error":"%s"}`, message)
	resp := &http.Response{
		StatusCode: status,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
	_ = resp.Write(conn)
}

func (s *Server) getOrCreateLeafCert(host string) (*tls.Certificate, error) {
	s.certCacheMu.RLock()
	if cert, ok := s.certCache[host]; ok {
		s.certCacheMu.RUnlock()
		return cert, nil
	}
	s.certCacheMu.RUnlock()

	s.certCacheMu.Lock()
	defer s.certCacheMu.Unlock()

	// Double-check after acquiring write lock
	if cert, ok := s.certCache[host]; ok {
		return cert, nil
	}

	cert, err := s.generateLeafCert(host)
	if err != nil {
		return nil, err
	}
	s.certCache[host] = cert
	return cert, nil
}

func (s *Server) generateLeafCert(host string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate leaf key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: host,
		},
		DNSNames:  []string{host},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, s.caCert, &key.PublicKey, s.caKey)
	if err != nil {
		return nil, fmt.Errorf("sign leaf cert: %w", err)
	}

	cert := &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
	return cert, nil
}
