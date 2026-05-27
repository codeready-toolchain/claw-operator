# Metrics and Plugins — Design Questions

**Status:** All decided
**Related:** [Design document](metrics-and-plugins-design.md)

Each question has options with trade-offs and a recommendation. Go through them
one by one to form the design, then update the design document.

---

## Q1: Metrics CRD field placement

Where should the metrics configuration live in the Claw CR spec?

### Option A: `spec.metrics`

```yaml
spec:
  metrics:
    enabled: true
```

- **Pro:** Simple, flat, easy to discover. Matches the operator's existing
  flat structure (`spec.auth`, `spec.credentials`, `spec.idle`).
- **Pro:** No future naming commitment — if we add logging later, it can be
  `spec.logging` without requiring the `observability` parent.
- **Con:** If we later add logging, tracing, alerting as separate top-level
  fields, the spec gets wide.

**Decision:** Option A — flat `spec.metrics`, consistent with existing CRD
style.

_Considered and rejected: Option B `spec.observability.metrics` (premature
nesting for a single feature; we don't have logging/alerting CRD fields)._

---

## Q2: ServiceMonitor default when metrics enabled

When `spec.metrics.enabled: true`, should the operator auto-create a
ServiceMonitor by default, or require an explicit opt-in?

### Option A: Auto-create by default, block Ready if CRD missing

`spec.metrics.serviceMonitor.enabled` defaults to `true` when
`spec.metrics.enabled` is true. If the ServiceMonitor CRD is not installed,
the Claw CR is marked not ready with a condition explaining why. User can
either install Prometheus Operator or set `serviceMonitor.enabled: false`.

- **Pro:** Turnkey experience — enable metrics, get monitoring.
- **Pro:** No silent failures — if the user's desired state can't be achieved,
  the CR reflects it. The user finds out immediately, not weeks later when
  they notice missing data.
- **Con:** Users who don't care about ServiceMonitor must explicitly opt out
  (`serviceMonitor.enabled: false`) on non-Prometheus clusters.

**Decision:** Always create ServiceMonitor when `spec.metrics.enabled: true`.
No CRD guard, no special condition. ServiceMonitor CRD is always present on
OpenShift (Prometheus Operator is a mandatory platform component deployed by
the Cluster Monitoring Operator). Keep `serviceMonitor.enabled` as an opt-out
for edge cases (non-Prometheus scraping, debugging) but default to `true`.
On vanilla Kubernetes without Prometheus Operator, the apply fails with a
self-explanatory reconcile error.

_Considered and rejected: Option B opt-in (extra step for the common case),
Option C auto-detect CRD (overengineering — CRD is always there on
OpenShift)._

---

## Q3: OTel Collector image variant

Which OTel Collector image should be used for the sidecar?

### Option A: Core (`otel/opentelemetry-collector`)

- **Pro:** Smallest image (~50MB). Contains only core receivers/exporters
  (OTLP, Prometheus, debug).
- **Pro:** Smaller attack surface.
- **Con:** If we ever need contrib receivers/exporters (e.g., for
  PrometheusRemoteWrite), we'd need to switch images.

**Decision:** Option A (core). We need exactly two components (OTLP receiver,
Prometheus exporter), both in core. Smaller image, smaller attack surface.
Switching to contrib later is a single env var change.

_Considered and rejected: Option B contrib (4x larger image for components we
don't use)._

---

## Q4: Monitoring namespace selector for NetworkPolicy

How should the ingress NetworkPolicy identify which namespaces can scrape the
metrics port?

### Option A: OpenShift well-known label

```yaml
- namespaceSelector:
    matchLabels:
      network.openshift.io/policy-group: monitoring
```

- **Pro:** Works on OpenShift out of the box — `openshift-monitoring` and
  `openshift-user-workload-monitoring` namespaces carry this label.
- **Con:** OpenShift-specific. Won't work on vanilla Kubernetes.

**Decision:** Option A. The operator targets OpenShift. Use the
well-known `network.openshift.io/policy-group: monitoring` label to scope
metrics ingress to the platform monitoring namespaces. Consistent with
security-first approach — only Prometheus in the monitoring namespace can
reach the metrics port.

_Considered and rejected: Option B broad selector (unnecessarily permissive),
Option C configurable label (adds config burden), Option D auto-detect with
fallback (two code paths for a single-platform operator)._

---

## Q5: Plugin list format

How should plugins be declared in the CR?

### Option A: Simple string list

```yaml
spec:
  plugins:
    - "@openclaw/diagnostics-otel"
    - "@martian-engineering/lossless-claw"
```

- **Pro:** Minimal, easy to read. Matches upstream openclaw-operator.
- **Pro:** Plugin config goes in `spec.config.raw` (already available).
- **Con:** No per-plugin metadata (version pinning, enabled/disabled toggle).

**Decision:** Option A. Simple `[]string`, matches upstream. Plugin config
lives in `spec.config.raw`. Version pinning can be added later via
`name@version` syntax if needed.

_Considered and rejected: Option B structured list (verbose, version pinning
doesn't fit OpenClaw's plugin installer, `enabled: false` is confusing)._

---

## Q6: Plugin installation command

What command should the init container run to install plugins?

### Option A: OpenClaw CLI (`openclaw plugins install`)

```sh
openclaw plugins install "clawhub:@openclaw/diagnostics-otel"
```

- **Pro:** Uses OpenClaw's official plugin mechanism. Handles ClawHub
  registry, auto-discovery, dependency resolution.
- **Pro:** Matches upstream openclaw-operator exactly.
- **Con:** Requires the gateway image to have the CLI (it does — same binary).

**Decision:** Option A. OpenClaw CLI handles plugin lifecycle correctly.
Gateway image already contains the CLI. Matches upstream.

_Considered and rejected: Option B raw npm (bypasses plugin discovery,
postinstall scripts are a security concern)._

---

## Q7: Metrics port number

What port should the OTel Collector sidecar expose for Prometheus scraping?

### Option A: 9464 (OTel Prometheus exporter default)

- **Pro:** Standard OTel Prometheus exporter default. No custom config needed.
- **Pro:** Clearly distinct from the gateway port (18789) and common
  Prometheus ports.

**Decision:** Option A (9464). Standard OTel Prometheus exporter default. No
custom config needed in the collector. Clearly distinct from gateway (18789)
and Prometheus server (9090).

_Considered and rejected: Option B 9090 (collides with Prometheus server
default), Option C 18790 (non-standard, not recognizable as metrics)._
