# ADR-0014: Prometheus Metrics and Plugin Installation

**Status:** Implemented

**Date:** 2026-05-26

---

## Overview

Add two capabilities to the Claw operator:

1. **Prometheus metrics via OTel Collector sidecar** — turnkey metrics for Claw
   instances. User enables metrics in the CR, operator adds an OTel Collector
   sidecar, injects `diagnostics.otel.metrics` config, creates a ServiceMonitor,
   and opens the NetworkPolicy for scraping.

2. **Plugin installation via init container** — declarative plugin management.
   User lists plugins in the CR, operator runs an init container that installs
   them on the PVC before the gateway starts.

Together with `spec.config.raw` (ADR-0013), these two features enable fully
declarative plugin and metrics setup without manual `oc exec` or post-deployment
scripting.

### What this does NOT cover

- **OTEL tracing egress** — the gateway's egress NetworkPolicy blocks
  connections to external collectors in other namespaces. Addressing this
  requires changes to `<instance>-egress`, which is a separate design concern.
- **`configMapRef`** for `spec.config` (deferred per ADR-0013).
- **Custom OTEL tracing pipeline** — the sidecar handles metrics only. Full
  tracing to external backends works via `spec.config.raw` for the config-file
  path.

---

## Design Principles

1. **Turnkey metrics** — `spec.metrics.enabled: true` is all a user needs. No
   plugin installation, no config patching, no manual NetworkPolicy or
   ServiceMonitor creation.

2. **Separate concerns** — metrics sidecar handles metrics export. Application
   config for diagnostics/tracing stays in `spec.config.raw`. Plugin
   installation is orthogonal to both.

3. **Security by default** — metrics on a dedicated port (separate from gateway
   traffic). Sidecar OTLP receiver binds `localhost` only. NetworkPolicy ingress
   for metrics is scoped to labeled monitoring namespaces.

4. **Follow established patterns** — OTel Collector sidecar matches the
   upstream openclaw-operator. `spec.plugins` init container follows the same
   upstream pattern. Image management follows the existing `PROXY_IMAGE` /
   `KUBECTL_IMAGE` env var convention.

5. **Backward compatible** — no metrics or plugins by default. Existing CRs
   produce identical behavior.

---

## Architecture

### Metrics data flow

```
┌─────────────────────────────────────────────────────────┐
│  Claw gateway pod                                       │
│                                                         │
│  ┌──────────┐  OTLP/HTTP   ┌───────────────────┐        │
│  │ gateway  │─────────────▶│  otel-collector   │        │
│  │ :18789   │  localhost   │  :4318 (recv)     │        │
│  └──────────┘  :4318       │  :9464 (prom)     │        │
│                            └───────────────────┘        │
└─────────────────────────────────────────────────────────┘
                                       │
                              NetworkPolicy allows
                              ingress on :9464 from
                              monitoring namespace
                                       │
                                       ▼
                              ┌────────────────┐
                              │  Prometheus    │
                              │  (scrapes      │
                              │   /metrics)    │
                              └────────────────┘
```

OpenClaw has built-in OTLP support — when `diagnostics.otel.metrics: true` is
set in `openclaw.json`, it pushes metrics via OTLP HTTP to the configured
endpoint. The OTel Collector sidecar receives OTLP on `localhost:4318` and
exposes a Prometheus-compatible `/metrics` endpoint on port 9464.

### Plugin installation flow

```
Pod start
  │
  ├── init-volume (existing)
  ├── init-config (existing — runs merge.js)
  ├── wait-for-proxy (existing)
  ├── init-plugins (runs openclaw plugins install)
  │
  └── gateway (main container)
```

The `init-plugins` container runs after `wait-for-proxy` because plugin
installation downloads packages from ClawHub/npm, which must go through the
MITM proxy (the egress NetworkPolicy blocks direct internet access from the
gateway pod). It uses the same OpenClaw image as the gateway.

### Resources created/modified when metrics enabled

| Resource | Change |
|----------|--------|
| **Deployment** | Add `otel-collector` sidecar container + ConfigMap volume mount |
| **ConfigMap** | Add `otel-collector.yaml` entry with collector pipeline config |
| **Service** | Add `metrics` port (9464/TCP) |
| **NetworkPolicy** (`<instance>-ingress`) | Add ingress rule for metrics port from monitoring namespace |
| **ServiceMonitor** | New resource — scrapes `/metrics` on port `metrics` |

### Resources created/modified when plugins declared

| Resource | Change |
|----------|--------|
| **Deployment** | Add `init-plugins` init container (after `wait-for-proxy`) |

---

## CRD Changes

### `spec.metrics`

```yaml
spec:
  metrics:
    # Enables the OTel Collector sidecar and diagnostics.otel.metrics config
    # injection. Default: false.
    enabled: true

    # Port for the Prometheus metrics endpoint on the OTel Collector sidecar.
    # Default: 9464.
    port: 9464

    serviceMonitor:
      # Create a ServiceMonitor for Prometheus Operator auto-discovery.
      # Default: true (when metrics.enabled is true).
      # Always created on OpenShift (Prometheus Operator is a platform component).
      enabled: true

      # Scrape interval. Default: "30s".
      interval: "30s"
```

### `spec.plugins`

```yaml
spec:
  plugins:
    - "@openclaw/diagnostics-otel"
    - "@openclaw/matrix"
```

---

## Known Limitations

1. **Plugin removal is not automatic.** Removing a plugin from `spec.plugins`
   stops it from being installed on new pods, but does not uninstall it from the
   existing PVC. Users must manually remove plugin files or delete the PVC to
   clean up. This matches upstream behavior.

2. **ServiceMonitor watch not registered.** The controller does not watch
   ServiceMonitor resources. If a ServiceMonitor is deleted externally, it is
   not recreated until the next Claw reconcile (triggered by any change to the
   Claw CR or its owned resources). Acceptable for v1 — can add a watch later
   if needed, though it requires importing the prometheus-operator client-go
   types.

---

## Decisions

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| Q1 | Metrics CRD field placement | `spec.metrics` (flat) | Consistent with existing flat CRD style (`spec.auth`, `spec.credentials`). No premature nesting for a single feature. |
| Q2 | ServiceMonitor default | Always create when metrics enabled | OpenShift always has Prometheus Operator. Turnkey experience without silent failures. Users can opt out with `serviceMonitor.enabled: false`. |
| Q3 | OTel Collector image | Core (`otel/opentelemetry-collector`) | Smallest image (~50MB), contains the only two components needed (OTLP receiver, Prometheus exporter). Switching to contrib later is a single env var change. |
| Q4 | NetworkPolicy for scraping | OpenShift well-known label `network.openshift.io/policy-group: monitoring` | Works out of the box on OpenShift. Only monitoring namespaces can reach the metrics port. |
| Q5 | Plugin list format | Simple `[]string` | Matches upstream. Plugin config lives in `spec.config.raw`. Version pinning can be added later via `name@version` syntax. |
| Q6 | Plugin install command | OpenClaw CLI `openclaw plugins install` | Official plugin mechanism with ClawHub registry support. Gateway image already contains the CLI. Matches upstream. |
| Q7 | Metrics port | 9464 (OTel Prometheus exporter default) | Standard default, clearly distinct from gateway (18789) and Prometheus server (9090). |
