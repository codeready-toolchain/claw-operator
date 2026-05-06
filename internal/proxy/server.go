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
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/elazarl/goproxy"
)

// Server is a credential-injecting proxy with two modes:
//   - Gateway mode: path-based routing for SDKs using baseUrl (e.g., /gemini/v1beta/...)
//   - Forward proxy mode: MITM CONNECT proxy for general egress via HTTP_PROXY/HTTPS_PROXY
type Server struct {
	proxy    *goproxy.ProxyHttpServer
	cfg      *Config
	gateways map[string]*httputil.ReverseProxy
	logger   *slog.Logger
}

// NewServer creates a proxy server from the given config and CA materials.
func NewServer(cfg *Config, caCertPEM, caKeyPEM []byte, logger *slog.Logger) (*Server, error) {
	caCert, caKey, err := parseCA(caCertPEM, caKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse CA: %w", err)
	}

	for i := range cfg.Routes {
		inj, err := NewInjector(&cfg.Routes[i])
		if err != nil {
			return nil, fmt.Errorf("create injector for domain %s: %w", cfg.Routes[i].Domain, err)
		}
		cfg.Routes[i].injector = inj
	}

	proxy := goproxy.NewProxyHttpServer()

	rootCAs := buildRootCAPool(cfg, logger)

	// Override goproxy's default transport which uses InsecureSkipVerify: true.
	// We MUST verify upstream server TLS certificates to prevent credential theft via MITM.
	proxy.Tr = &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: rootCAs},
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
	directConnect := &goproxy.ConnectAction{Action: goproxy.ConnectAccept, TLSConfig: tlsCfg}
	rejectConnect := &goproxy.ConnectAction{Action: goproxy.ConnectReject, TLSConfig: tlsCfg}

	proxy.OnRequest().HandleConnectFunc(
		func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
			route := cfg.MatchRoute(host, "")
			if route == nil {
				logger.Warn("blocked CONNECT to unknown domain", "host", host)
				ctx.Resp = goproxy.NewResponse(
					ctx.Req, goproxy.ContentTypeText, http.StatusForbidden,
					`{"error":"domain not allowed"}`,
				)
				return rejectConnect, host
			}
			if !cfg.NeedsMITMForHost(host) {
				logger.Debug("CONNECT tunnel (direct)", "host", host)
				return directConnect, host
			}
			logger.Debug("CONNECT allowed (MITM)", "host", host)
			return mitmConnect, host
		},
	)

	proxy.OnRequest().DoFunc(
		func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
			route := cfg.MatchRoute(req.URL.Host, req.URL.Path)
			if route == nil {
				logger.Warn("blocked request to unknown domain", "host", req.URL.Host)
				return req, goproxy.NewResponse(
					req, goproxy.ContentTypeText, http.StatusForbidden,
					`{"error":"domain not allowed"}`,
				)
			}

			if !route.PathAllowed(req.URL.Path) {
				logger.Warn("blocked request to restricted path", "host", req.URL.Host, "path", req.URL.Path)
				return req, goproxy.NewResponse(
					req, goproxy.ContentTypeText, http.StatusForbidden,
					`{"error":"path not allowed"}`,
				)
			}

			StripAuthHeaders(req)

			// GCP token vending: Google's SDK tries to fetch its own OAuth2 token
			// from oauth2.googleapis.com/token before each API call. We intercept
			// this and return a dummy token because the GCP injector already handles
			// real token acquisition and injection on the actual API request. This
			// must happen here (not in the injector) because we need to short-circuit
			// the entire request and return a synthetic response.
			if route.Injector == injectorGCP && isTokenVendingRequest(req) {
				return req, goproxy.NewResponse(
					req, "application/json", http.StatusOK,
					string(TokenVendingResponse()),
				)
			}

			if err := route.injector.Inject(req); err != nil {
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

	gws := make(map[string]*httputil.ReverseProxy)
	for i := range cfg.Routes {
		route := &cfg.Routes[i]
		if route.PathPrefix == "" || route.Upstream == "" {
			continue
		}
		upstream, err := url.Parse(route.Upstream)
		if err != nil {
			return nil, fmt.Errorf("parse upstream URL %q for route %s: %w", route.Upstream, route.Domain, err)
		}
		gw := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL.Scheme = upstream.Scheme
				req.URL.Host = upstream.Host
				req.Host = upstream.Host
			},
			Transport: &http.Transport{
				TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
				ResponseHeaderTimeout: 300 * time.Second,
				IdleConnTimeout:       90 * time.Second,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
			},
			FlushInterval: -1,
		}
		gws[route.PathPrefix] = gw
		logger.Info("registered gateway route", "pathPrefix", route.PathPrefix, "upstream", route.Upstream)
	}

	return &Server{proxy: proxy, cfg: cfg, gateways: gws, logger: logger}, nil
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

		if r.Method != http.MethodConnect {
			if route, strippedPath := s.cfg.MatchRouteByPath(r.URL.Path); route != nil {
				s.serveGateway(w, r, route, strippedPath)
				return
			}
		}

		s.proxy.ServeHTTP(w, r)
	})
}

// serveGateway handles requests matched by path prefix, forwarding to the
// configured upstream with credential injection. This is the gateway mode
// where SDKs use baseUrl to send plain HTTP requests directly to the proxy.
func (s *Server) serveGateway(w http.ResponseWriter, r *http.Request, route *Route, strippedPath string) {
	gw := s.gateways[route.PathPrefix]
	if gw == nil {
		http.Error(w, `{"error":"no gateway route configured"}`, http.StatusBadGateway)
		return
	}

	StripAuthHeaders(r)

	if route.Injector == injectorGCP && isTokenVendingRequest(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(TokenVendingResponse())
		return
	}

	if err := route.injector.Inject(r); err != nil {
		s.logger.Error("credential injection failed", "domain", route.Domain, "error", "[REDACTED]")
		http.Error(w, `{"error":"credential injection failed"}`, http.StatusBadGateway)
		return
	}

	for k, v := range route.DefaultHeaders {
		r.Header.Set(k, v)
	}

	r.URL.Path = strippedPath
	r.URL.RawPath = ""
	r.Header.Del("Via")
	r.Header.Del("X-Forwarded-For")

	s.logger.Debug("gateway", "pathPrefix", route.PathPrefix, "upstream", route.Upstream, "path", strippedPath)
	gw.ServeHTTP(w, r)
}

// buildRootCAPool creates an x509.CertPool containing the system CAs plus any
// route-specific CAs (e.g., Kubernetes API server CAs from kubeconfig). This
// allows the proxy to verify upstream TLS for servers with non-public CAs.
func buildRootCAPool(cfg *Config, logger *slog.Logger) *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil {
		logger.Warn("failed to load system cert pool, using empty pool", "error", err)
		pool = x509.NewCertPool()
	}

	for i := range cfg.Routes {
		if cfg.Routes[i].CACert == "" {
			continue
		}
		pemBytes, err := base64.StdEncoding.DecodeString(cfg.Routes[i].CACert)
		if err != nil {
			logger.Error("failed to base64-decode route CA cert", "domain", cfg.Routes[i].Domain, "error", err)
			continue
		}
		if pool.AppendCertsFromPEM(pemBytes) {
			logger.Info("loaded route CA cert", "domain", cfg.Routes[i].Domain)
		} else {
			logger.Error("failed to parse route CA cert PEM", "domain", cfg.Routes[i].Domain)
		}
	}

	return pool
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
