# MCP Egress and NetworkPolicy Escape Hatch

**Status:** Final — all decisions resolved in
[mcp-diagnostics-egress-questions.md](mcp-diagnostics-egress-questions.md)

---

## Overview

The operator's NetworkPolicies enforce a strict security posture: gateway pods
can only reach the MITM proxy (port 8080) and DNS; proxy pods can only reach
TCP 443 and DNS. This blocks legitimate in-cluster traffic patterns:

- **In-cluster MCP servers** on custom ports (e.g., `:9001`) — the gateway's
  `NO_PROXY` setting correctly bypasses the proxy for `.svc` hostnames, but the
  gateway egress NP blocks the direct connection.
- **Cross-namespace services** (tracing collectors, databases, etc.) — same
  blocker for any in-cluster service the gateway needs to reach directly.

External MCP traffic is unaffected — it already flows through the proxy via
`HTTP_PROXY`/`HTTPS_PROXY` and the proxy allowlist auto-adds MCP domains.

This feature adds two capabilities:

1. **Auto-generated egress rules** from `spec.mcpServers[].url` — the operator
   parses declared MCP URLs and appends the necessary NP rules automatically.
2. **`spec.networkPolicy.allowedEgress` escape hatch** — users declare raw
   `NetworkPolicyEgressRule` objects for anything the operator can't
   auto-detect (tracing endpoints, databases, webhooks, etc.).

---

## Design Principles

1. **Auto-configure what we're certain about** — if the user declared an MCP
   server URL, the operator knows exactly what NP rule is needed. It should
   just work without additional configuration.

2. **Escape hatch for everything else** — diagnostics, tracing, databases, and
   other services are handled via `spec.networkPolicy.allowedEgress`, following
   the upstream community operator's proven pattern.

3. **Defense in depth preserved** — new egress rules are the minimum surface
   needed. External traffic still flows through the proxy (L7 allowlist +
   credential injection). In-cluster rules are scoped to specific
   namespaces and ports.

4. **Follow existing patterns** — rules are appended to existing NP objects
   (same as `addMetricsIngressRule` and `injectKubePortsIntoNetworkPolicy`).
   No new NetworkPolicy resources created.

5. **Backward compatible** — no MCP servers declared and no `allowedEgress` →
   no rules added → identical behavior to today.

---

## Architecture

### Traffic model (unchanged)

```
External MCP/LLM traffic:
  Gateway ──HTTP_PROXY──▶ Proxy ──TCP 443──▶ Internet
  (unchanged — proxy L7 allowlist + credential injection)

In-cluster MCP traffic:
  Gateway ──NO_PROXY──▶ In-cluster Service:port
  (bypasses proxy — needs gateway egress NP rule)
```

External traffic routing is not affected by this feature. The proxy remains
the sole egress path for external HTTPS. This feature opens targeted gateway
egress rules for in-cluster direct connections that `NO_PROXY` already routes.

### URL classification

MCP URLs are classified by hostname pattern:

```
hostname has no dots              → in-cluster, same namespace
hostname ends .svc.cluster.local  → in-cluster, namespace = 2nd label
hostname ends .svc                → in-cluster, namespace = 2nd label
else (2+ labels, IP, etc.)        → external
```

If the extracted namespace matches the Claw instance's own namespace, treat as
same-namespace (simpler rule).

### Generated NP rules by scenario

**Same-namespace in-cluster MCP** (e.g., `http://mcp-customer:9001/mcp`):

```yaml
# Appended to {instance}-egress
egress:
  - to:
      - podSelector: {}    # any pod in same namespace
    ports:
      - port: 9001
        protocol: TCP
```

**Cross-namespace in-cluster MCP** (e.g., `http://mcp-server.shared-tools.svc:9001`):

```yaml
# Appended to {instance}-egress
egress:
  - to:
      - namespaceSelector:
          matchLabels:
            kubernetes.io/metadata.name: shared-tools
    ports:
      - port: 9001
        protocol: TCP
```

**External MCP on non-443 port** (e.g., `https://mcp.example.com:8443`):

```yaml
# Port added to {instance}-proxy-egress first egress rule
# (same pattern as injectKubePortsIntoNetworkPolicy)
egress:
  - ports:
      - port: 443
        protocol: TCP
      - port: 8443        # ← added
        protocol: TCP
```

**External MCP on port 443** — no NP changes needed (443 already allowed).

**Stdio MCP** (no `url` field) — no NP changes needed (subprocess in-container).

### User escape hatch

`spec.networkPolicy.allowedEgress` for anything the operator can't auto-detect:

```yaml
spec:
  networkPolicy:
    allowedEgress:
      # Cross-namespace tracing collector
      - to:
          - namespaceSelector:
              matchLabels:
                kubernetes.io/metadata.name: langfuse
        ports:
          - port: 3000
            protocol: TCP
      # Same-namespace database
      - to:
          - podSelector: {}
        ports:
          - port: 5432
            protocol: TCP
```

These raw `NetworkPolicyEgressRule` objects are appended to `{instance}-egress`
(the gateway egress NP) alongside the auto-generated MCP rules.

**Note:** `allowedEgress` only modifies the **gateway** egress NP. For proxy
egress customization (rare — only needed for external non-443 services that
are not declared as MCP servers), users should create supplemental
NetworkPolicies directly.

### Data flow

```
                     ┌──────────────────────────────────────────────────┐
                     │  r.enrichConfigAndNetworkPolicy()                │
                     │                                                  │
spec.mcpServers ──▶  │  classifyMcpEgressTargets(instance)             │
                     │      ↓                                           │
                     │  injectMcpGatewayEgressRules(objects, targets)   │
                     │  injectMcpProxyEgressPorts(objects, targets, ..) │
                     │                                                  │
spec.networkPolicy   │  injectAllowedEgress(objects, instance)         │
  .allowedEgress ──▶ │                                                  │
                     └──────────────────────────────────────────────────┘
```

---

## CRD Changes

### `spec.networkPolicy`

```go
// NetworkPolicySpec configures additional NetworkPolicy rules beyond
// the operator's auto-generated defaults.
type NetworkPolicySpec struct {
    // AllowedEgress appends raw egress rules to the gateway NetworkPolicy.
    // Use for targets the operator cannot auto-detect: tracing collectors,
    // databases, webhooks, etc. Rules are appended to {instance}-egress.
    // +optional
    AllowedEgress []networkingv1.NetworkPolicyEgressRule `json:"allowedEgress,omitempty"`
}
```

Added to `ClawSpec`:

```go
// NetworkPolicy configures additional NetworkPolicy rules.
// MCP server egress rules are auto-generated from spec.mcpServers URLs.
// +optional
NetworkPolicy *NetworkPolicySpec `json:"networkPolicy,omitempty"`
```

**Import note:** `api/v1alpha1/claw_types.go` will need a new import of
`networkingv1 "k8s.io/api/networking/v1"`. The type name `NetworkPolicySpec`
does not conflict in practice — it lives in the `clawv1alpha1` package, while
the Kubernetes type is `networkingv1.NetworkPolicySpec`.

No changes to `McpServerSpec` — MCP URLs are already declared there.

---

## Rule deduplication

Multiple MCP servers may target the same namespace and port. Rules are
deduplicated by `(namespace, port)` tuple before injection. External non-443
ports are deduplicated by port number and against existing ports already
in the proxy egress rule (443, kube API ports).

---

## Validation and error handling

- MCP URLs that fail to parse are skipped with a log warning. No egress rule
  generated. Consistent with `mcpPassthroughRoutes()` behavior.
- Port defaults: HTTP → 80, HTTPS → 443. Only non-443 external ports generate
  proxy egress rules.
- `allowedEgress` rules are raw Kubernetes types — validated by the API server
  on CR admission, no operator-side validation needed.

---

## Implementation Plan

Single PR. Implementation order within the PR:

**1. CRD types + code generation**

- Add `NetworkPolicySpec` to `api/v1alpha1/claw_types.go` with
  `AllowedEgress []networkingv1.NetworkPolicyEgressRule`.
- Add `NetworkPolicy *NetworkPolicySpec` to `ClawSpec`.
- Add `networkingv1 "k8s.io/api/networking/v1"` import.
- `make manifests generate` to regenerate CRD YAML and DeepCopy.

**2. URL classification and egress rule injection**

New files: `internal/controller/claw_egress.go`,
`internal/controller/claw_egress_test.go`

- `egressTarget` struct: `Port int`, `Namespace string`, `External bool`
- `classifyServiceURL(rawURL, instanceNamespace) → egressTarget` — parses a
  URL, classifies by DNS heuristic, extracts namespace and port.
- `classifyMcpEgressTargets(instance) → []egressTarget` — iterates
  `spec.mcpServers`, calls `classifyServiceURL` for each HTTP MCP server
  (skips stdio). Deduplicates by `(namespace, port)`.
- `injectMcpGatewayEgressRules(objects, targets, instanceName)` — appends
  egress rules to `{instance}-egress` for in-cluster targets. Same-namespace
  uses `podSelector: {}` + port. Cross-namespace uses `namespaceSelector` with
  `kubernetes.io/metadata.name` + port.
- `injectMcpProxyEgressPorts(objects, targets, instanceName)` — adds non-443
  external ports to the first egress rule in `{instance}-proxy-egress`,
  following the `injectKubePortsIntoNetworkPolicy` pattern (append to
  `egress[0].ports`). Deduplicates against existing ports.
- `injectAllowedEgress(objects, instance)` — appends user's
  `spec.networkPolicy.allowedEgress` rules to `{instance}-egress`.
- All three injection functions called from `r.enrichConfigAndNetworkPolicy()`.

**3. Tests**

- Unit tests covering URL classification: bare hostname, two-part DNS, FQDN,
  same-namespace FQDN, external hostname, IP address, non-443 port, default
  ports, malformed URLs, stdio MCP (no URL).
- Unit tests for NP injection: same-namespace rule, cross-namespace rule,
  proxy egress port, `allowedEgress` append, deduplication.
- Integration tests (envtest): reconcile with in-cluster MCP → verify gateway
  NP. Reconcile with external non-443 MCP → verify proxy NP. Reconcile with
  `allowedEgress` → verify rules appended.

**4. Documentation**

- Update user guide with `spec.networkPolicy.allowedEgress` section and
  MCP auto-egress behavior.
- Update `docs/architecture.md` networking section.
- Update `PLATFORM.md` skill in `configmap.yaml`:
  - Proxy Architecture section: clarify that in-cluster traffic (`.svc`,
    `.svc.cluster.local`) bypasses the proxy via `NO_PROXY` and connects
    directly. The operator auto-generates NetworkPolicy rules for in-cluster
    MCP servers declared in `spec.mcpServers`.
  - MCP Tier 1 section: note that in-cluster HTTP MCP servers connect directly
    (not through the proxy) and get automatic NP egress rules.
  - Add a note about `spec.networkPolicy.allowedEgress` as the escape hatch
    for in-cluster services not covered by MCP auto-egress (tracing collectors,
    databases, etc.).

---

## Interaction with existing features

| Feature | Interaction |
|---------|-------------|
| Proxy allowlist (`claw_proxy.go`) | Independent — allowlist is L7; this is L4 NP rules |
| `injectKubePortsIntoNetworkPolicy` | Complementary — handles k8s API ports on proxy egress; MCP ports deduplicate against these |
| `addMetricsIngressRule` | Parallel pattern — same approach (append rules to existing NP) |
| `NO_PROXY` env var | Prerequisite — in-cluster URLs bypass proxy; NP must allow the direct connection |
| Stdio MCP servers | No effect — subprocess traffic inherits `HTTP_PROXY` |
| `spec.config.raw` diagnostics | Independent — tracing egress handled via `allowedEgress` escape hatch |
| Config hash / pod rollout | No interaction — NP changes don't require pod restarts |

---

## Examples

### Full CR with MCP auto-egress + escape hatch

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: demo
spec:
  credentials:
    - secretRef:
        name: anthropic-key
      provider: anthropic
      type: api_key

  mcpServers:
    # Same-namespace → auto gateway egress podSelector:{} + port 9001
    customer-api:
      url: http://mcp-customer:9001/mcp

    # Cross-namespace (requires .svc suffix to match NO_PROXY)
    shared-tools:
      url: http://mcp-server.shared-tools.svc:9001/mcp

    # External → auto proxy egress port (only if non-443)
    cloud-mcp:
      url: https://mcp.example.com/mcp          # 443 — no NP change

    # Stdio → no NP changes
    filesystem:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]

  networkPolicy:
    allowedEgress:
      # Langfuse tracing in another namespace
      - to:
          - namespaceSelector:
              matchLabels:
                kubernetes.io/metadata.name: langfuse
        ports:
          - port: 3000
            protocol: TCP
```
