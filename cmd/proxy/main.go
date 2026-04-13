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

package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"

	"github.com/codeready-toolchain/openclaw-operator/internal/proxy"
)

func main() {
	var (
		configPath = flag.String("config", "/etc/proxy/proxy-config.json", "path to proxy config JSON")
		cacertPath = flag.String("ca-cert", "/etc/proxy/ca/ca.crt", "path to CA certificate PEM")
		cakeyPath  = flag.String("ca-key", "/etc/proxy/ca/ca.key", "path to CA private key PEM")
		listenAddr = flag.String("listen", ":8080", "listen address")
	)
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := proxy.LoadConfig(*configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logger.Info("loaded proxy config", "routes", len(cfg.Routes))

	caCert, err := os.ReadFile(*cacertPath)
	if err != nil {
		logger.Error("failed to read CA cert", "error", err)
		os.Exit(1)
	}
	caKey, err := os.ReadFile(*cakeyPath)
	if err != nil {
		logger.Error("failed to read CA key", "error", err)
		os.Exit(1)
	}

	srv, err := proxy.NewServer(cfg, caCert, caKey, logger)
	if err != nil {
		logger.Error("failed to create proxy server", "error", err)
		os.Exit(1)
	}

	logger.Info("starting proxy", "addr", *listenAddr)
	if err := http.ListenAndServe(*listenAddr, srv.Handler()); err != nil {
		logger.Error("proxy server failed", "error", err)
		os.Exit(1)
	}
}
