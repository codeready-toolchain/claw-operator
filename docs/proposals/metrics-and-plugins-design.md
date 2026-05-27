# Prometheus Metrics and Plugin Installation

**Status:** Final — all questions resolved, see
[metrics-and-plugins-questions.md](metrics-and-plugins-questions.md)

**Date:** 2026-05-26

**Addresses:** Declarative plugin management, OTEL metrics collection,
Prometheus integration

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

Together with the existing `spec.config.raw` (ADR-0013), these two features
enable fully declarative plugin and metrics setup without manual `oc exec` or
post-deployment scripting.

### What this does NOT cover

- **OTEL tracing egress** — the gateway's egress
  NetworkPolicy blocks connections to external collectors in other namespaces.
  Addressing this requires changes to `<instance>-egress`, which is a separate
  design concern.
- **`configMapRef`** for `spec.config` (deferred per ADR-0013 Q6).
- **Custom OTEL tracing pipeline** — the sidecar handles metrics only. Full
  tracing to external backends (Langfuse, MLflow) works via `spec.config.raw`
  for the config-file path; env var injection is not needed since the OTEL
  plugin reads config first.

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
  ├── init-plugins (NEW — runs openclaw plugins install)
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
| **ServiceMonitor** | NEW resource — scrapes `/metrics` on port `metrics` |

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

### Type definitions

```go
type MetricsSpec struct {
    Enabled        bool               `json:"enabled,omitempty"`
    Port           *int32             `json:"port,omitempty"`
    ServiceMonitor *ServiceMonitorSpec `json:"serviceMonitor,omitempty"`
}

type ServiceMonitorSpec struct {
    Enabled  *bool  `json:"enabled,omitempty"`
    Interval string `json:"interval,omitempty"`
}

type ClawSpec struct {
    // ... existing fields ...
    Metrics *MetricsSpec `json:"metrics,omitempty"`
    Plugins []string     `json:"plugins,omitempty"`
}
```

---

## Implementation Plan

Each phase is a self-contained PR with types, logic, tests, and docs.

### Phase 1: Prometheus metrics via OTel Collector sidecar - DONE

**CRD types** ([api/v1alpha1/claw_types.go](../../api/v1alpha1/claw_types.go)):
- Add `MetricsSpec`, `ServiceMonitorSpec` structs
- Add `Metrics *MetricsSpec` to `ClawSpec`
- Run `make manifests generate`

**Collector config**
([internal/controller/claw_metrics.go](../../internal/controller/) — new file):
- `configureMetricsSidecar(objects, instance)` — adds `otel-collector` sidecar
  container, `otel-collector.yaml` ConfigMap entry, and `config` volume mount
  to the gateway Deployment. Called from `configureDeployments`.
- `injectMetricsConfig(config, instance)` — injects
  `diagnostics.otel.metrics: true` +
  `diagnostics.otel.endpoint: "http://localhost:4318"` into `operator.json`
  when metrics enabled. Only sets if user hasn't already configured
  `diagnostics.otel`. Called from `enrichConfigAndNetworkPolicy`.
- `addMetricsPortToService(objects, instance)` — adds `metrics` port to
  Service when metrics enabled.
- `addMetricsIngressRule(objects, instance)` — adds ingress rule to
  `<instance>-ingress` NetworkPolicy for the metrics port. Uses the OpenShift
  well-known label `network.openshift.io/policy-group: monitoring` to scope
  ingress to platform monitoring namespaces.

**ServiceMonitor**
([internal/controller/claw_metrics.go](../../internal/controller/)):
- `reconcileServiceMonitor(ctx, instance)` — creates/updates ServiceMonitor
  via server-side apply when `spec.metrics.serviceMonitor.enabled` is true
  (or default). Deletes when disabled.
- Requires RBAC marker:
  `// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete`

**Reconciler image config**
([cmd/main.go](../../cmd/main.go)):
- Read `OTEL_COLLECTOR_IMAGE` env var (default:
  `mirror.gcr.io/otel/opentelemetry-collector:0.120.0` — core variant,
  Google mirror avoids Docker Hub rate limits)
- Add `OTelCollectorImage string` to `ClawResourceReconciler`

**Sidecar container spec:**
```yaml
- name: otel-collector
  image: <OTEL_COLLECTOR_IMAGE>
  args: ["--config=/etc/otel-collector/config.yaml"]
  ports:
    - name: metrics
      containerPort: 9464
      protocol: TCP
  resources:
    requests:
      memory: 32Mi
      cpu: 10m
    limits:
      memory: 128Mi
      cpu: 100m
  securityContext:
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    runAsNonRoot: true
    capabilities:
      drop: [ALL]
  volumeMounts:
    - name: config
      mountPath: /etc/otel-collector/config.yaml
      subPath: otel-collector.yaml
      readOnly: true
```

**Collector config (injected into ConfigMap):**
```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 127.0.0.1:4318
exporters:
  prometheus:
    endpoint: 0.0.0.0:9464
service:
  pipelines:
    metrics:
      receivers: [otlp]
      exporters: [prometheus]
```

**Tests:**
- Unit tests for sidecar injection, collector config generation, Service port
  addition, NetworkPolicy rule addition
- Integration tests (envtest): create Claw with `spec.metrics.enabled: true`,
  verify Deployment has sidecar, Service has metrics port, ConfigMap has
  collector config, `operator.json` has `diagnostics.otel.metrics`
- Integration test: ServiceMonitor creation and deletion

**Docs:**
- Update [user-guide.md](../../docs/user-guide.md) with Metrics section

### Phase 2: Plugin installation via init container - DONE

**CRD types** ([api/v1alpha1/claw_types.go](../../api/v1alpha1/claw_types.go)):
- Add `Plugins []string` to `ClawSpec`
- Run `make manifests generate`

**Init container**
([internal/controller/claw_plugins.go](../../internal/controller/) — new file):
- `configurePluginsInitContainer(objects, instance)` — adds `init-plugins`
  init container when `spec.plugins` is non-empty. Inserted after
  `wait-for-proxy` (last init container before gateway starts) because plugin
  installation downloads packages from ClawHub/npm through the MITM proxy.
- Uses the same OpenClaw image as the gateway (from the Kustomize `images`
  tag, no separate env var needed).
- Mounts PVC at `/home/node/.openclaw` (same `subPath: home` as gateway).
- Mounts `proxy-ca` and sets CA cert env vars so npm trusts the MITM proxy.
- Sets `HTTP_PROXY`/`HTTPS_PROXY` pointing to `<instance>-proxy:8080` and
  `NO_PROXY` for cluster-internal traffic — same values as the gateway
  container (the egress NetworkPolicy blocks direct internet access).
- Script: `set -e; openclaw plugins install clawhub:<pkg>` for each entry.
- Include plugin list in the `gateway-config-hash` annotation to trigger
  rollout when plugins change.

**Init container spec:**
```yaml
- name: init-plugins
  image: ghcr.io/openclaw/openclaw:slim  # same as gateway
  command: ["sh", "-c", "<generated script>"]
  env:
    - name: HOME
      value: /home/node
    - name: NPM_CONFIG_CACHE
      value: /home/node/.cache/npm
    - name: HTTP_PROXY
      value: http://CLAW_INSTANCE_NAME-proxy:8080
    - name: HTTPS_PROXY
      value: http://CLAW_INSTANCE_NAME-proxy:8080
    - name: NO_PROXY
      value: localhost,127.0.0.1,.svc,.svc.cluster.local
    - name: NODE_EXTRA_CA_CERTS
      value: /etc/proxy-ca/ca.crt
  resources:
    requests:
      memory: 128Mi
      cpu: 100m
    limits:
      memory: 512Mi
      cpu: 500m
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop: [ALL]
  volumeMounts:
    - name: claw-home
      mountPath: /home/node/.openclaw
      subPath: home
    - name: claw-home
      mountPath: /home/node/.local
      subPath: home/.local
    - name: claw-home
      mountPath: /home/node/.cache
      subPath: home/.cache
    - name: proxy-ca
      mountPath: /etc/proxy-ca
      readOnly: true
    - name: tmp-volume
      mountPath: /tmp
```

**Tests:**
- Unit tests for init-plugins container injection, script generation
- Integration tests (envtest): create Claw with `spec.plugins`, verify init
  container exists with correct install script, proxy env vars, and CA mount

**Docs:**
- Update [user-guide.md](../../docs/user-guide.md) with Plugins section
- ADR for the metrics/plugins design decisions

---

## Known Limitations

1. **Plugin removal is not automatic.** Removing a plugin from `spec.plugins`
   stops it from being installed on new pods, but does not uninstall it from the
   existing PVC. Users must manually remove plugin files or delete the PVC to
   clean up. This matches upstream behavior.

2. **ServiceMonitor watch not registered.** The controller does not watch
   ServiceMonitor resources (`Owns` or explicit watch). If a ServiceMonitor is
   deleted externally, it is not recreated until the next Claw reconcile
   (triggered by any change to the Claw CR or its owned resources). Acceptable
   for v1 — can add a watch later if needed, though it requires importing the
   prometheus-operator client-go types.

---

## Decisions Summary

All questions resolved — see
[metrics-and-plugins-questions.md](metrics-and-plugins-questions.md) for full
discussion.

| # | Question | Decision |
|---|----------|----------|
| Q1 | Metrics CRD field placement | `spec.metrics` (flat, consistent with existing CRD) |
| Q2 | ServiceMonitor default | Always create when metrics enabled (OpenShift always has the CRD) |
| Q3 | OTel Collector image | Core (`otel/opentelemetry-collector`) — smaller, sufficient |
| Q4 | NetworkPolicy for scraping | OpenShift well-known label `network.openshift.io/policy-group: monitoring` |
| Q5 | Plugin list format | Simple `[]string` (matches upstream) |
| Q6 | Plugin install command | OpenClaw CLI `openclaw plugins install` (matches upstream) |
| Q7 | Metrics port | 9464 (OTel Prometheus exporter default) |
