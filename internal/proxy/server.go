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
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/elazarl/goproxy"
)

// Server is a MITM credential-injecting forward proxy backed by goproxy.
type Server struct {
	proxy  *goproxy.ProxyHttpServer
	logger *slog.Logger
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

	proxy := goproxy.NewProxyHttpServer()

	// Override goproxy's default transport which uses InsecureSkipVerify: true.
	// We MUST verify upstream server TLS certificates to prevent credential theft via MITM.
	proxy.Tr = &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: (&net.Dialer{
			Timeout: 15 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 300 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
	}

	tlsCert := tls.Certificate{
		Certificate: [][]byte{caCert.Raw},
		PrivateKey:  caKey,
		Leaf:        caCert,
	}
	tlsCfg := goproxy.TLSConfigFromCA(&tlsCert)
	mitmConnect := &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: tlsCfg}
	rejectConnect := &goproxy.ConnectAction{Action: goproxy.ConnectReject, TLSConfig: tlsCfg}

	proxy.OnRequest().HandleConnectFunc(
		func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
			hostname := stripPort(host)
			route := cfg.MatchRoute(hostname)
			if route == nil {
				logger.Warn("blocked CONNECT to unknown domain", "host", hostname)
				ctx.Resp = goproxy.NewResponse(
					ctx.Req, goproxy.ContentTypeText, http.StatusForbidden,
					`{"error":"domain not allowed"}`,
				)
				return rejectConnect, host
			}
			logger.Debug("CONNECT allowed", "host", hostname)
			return mitmConnect, host
		},
	)

	proxy.OnRequest().DoFunc(
		func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
			hostname := stripPort(req.URL.Host)
			route := cfg.MatchRoute(hostname)
			if route == nil {
				logger.Warn("blocked request to unknown domain", "host", hostname)
				return req, goproxy.NewResponse(
					req, goproxy.ContentTypeText, http.StatusForbidden,
					`{"error":"domain not allowed"}`,
				)
			}

			StripAuthHeaders(req)

			// GCP token vending: Google's SDK tries to fetch its own OAuth2 token
			// from oauth2.googleapis.com/token before each API call. We intercept
			// this and return a dummy token because the GCP injector already handles
			// real token acquisition and injection on the actual API request. This
			// must happen here (not in the injector) because we need to short-circuit
			// the entire request and return a synthetic response.
			if route.Injector == "gcp" && isTokenVendingRequest(req) {
				return req, goproxy.NewResponse(
					req, "application/json", http.StatusOK,
					string(TokenVendingResponse()),
				)
			}

			inj := injectors[route.Domain]
			if inj == nil {
				logger.Error("no injector for matched route", "domain", route.Domain)
				return req, goproxy.NewResponse(
					req, goproxy.ContentTypeText, http.StatusBadGateway,
					`{"error":"credential injection failed"}`,
				)
			}

			if err := inj.Inject(req); err != nil {
				logger.Error("credential injection failed", "domain", route.Domain, "error", "[REDACTED]")
				return req, goproxy.NewResponse(
					req, goproxy.ContentTypeText, http.StatusBadGateway,
					`{"error":"credential injection failed"}`,
				)
			}

			req.Header.Del("Via")
			req.Header.Del("X-Forwarded-For")

			return req, nil
		},
	)

	return &Server{proxy: proxy, logger: logger}, nil
}

// Close is a no-op retained for API compatibility.
func (s *Server) Close() {}

// Handler returns an http.Handler for the proxy server.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect && r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok\n"))
			return
		}
		s.proxy.ServeHTTP(w, r)
	})
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

	if !cert.IsCA {
		return nil, nil, fmt.Errorf("CA certificate has IsCA=false, cannot sign leaf certs")
	}
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, nil, fmt.Errorf("CA certificate missing KeyUsageCertSign")
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("failed to decode CA key PEM")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA key: %w", err)
	}

	certPub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, nil, fmt.Errorf("CA certificate public key is not ECDSA")
	}
	if certPub.Curve != key.Curve ||
		certPub.X.Cmp(key.X) != 0 ||
		certPub.Y.Cmp(key.Y) != 0 {
		return nil, nil, fmt.Errorf("CA private key does not match certificate public key")
	}

	return cert, key, nil
}

func stripPort(hostPort string) string {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		return strings.ToLower(hostPort)
	}
	return strings.ToLower(host)
}
