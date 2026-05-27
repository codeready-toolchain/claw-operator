# ADR-0016: MCP Egress and NetworkPolicy Escape Hatch

**Status:** Implemented
**Date:** 2026-05-27

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
   (same as metrics ingress rules and kube port injection).
   No new NetworkPolicy resources created.

5. **Backward compatible** — no MCP servers declared and no `allowedEgress` →
   no rules added → identical behavior to today.

---

## Decisions

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| 1 | Scope — MCP only, or also diagnostics/tracing? | MCP auto-egress + `spec.networkPolicy.allowedEgress` escape hatch | Auto-configures typed MCP URLs (operator should "just work"); escape hatch covers all other cases without fragile JSON path parsing of `spec.config.raw`; follows upstream community operator pattern; `spec.networkPolicy` grouping leaves room for future additions |
| 2 | In-cluster URL detection — heuristic vs explicit? | Kubernetes DNS heuristic | Zero config; matches `NO_PROXY` behavior exactly; OpenShift always uses `cluster.local` and ships k8s 1.23+ |
| 3 | Same-namespace egress rule granularity | `podSelector: {}` with port | Scoped to same-namespace pods; no target label knowledge needed; tighter than port-only |
| 4 | Cross-namespace targeting strategy | `kubernetes.io/metadata.name` namespace label | Precise scoping; namespace parsed from URL; guaranteed on OpenShift |
| 5 | External non-443 ports — proxy egress? | Add parsed ports to proxy egress NP | Consistent with kube API port injection pattern; proxy L7 allowlist remains primary enforcement |
| 6 | Diagnostics endpoint source | Superseded by Q1 — escape hatch handles diagnostics | No `spec.config.raw` parsing or new typed diagnostics fields needed |
| 7 | Unparseable URL handling | Skip and log warning | Non-blocking; consistent with existing proxy allowlist behavior; users can use escape hatch |
| 8 | NP rule strategy | Append to existing NPs | Consistent with established patterns; self-cleaning via server-side apply; no new resources |

---

## Architecture

### Traffic model

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
hostname has exactly 2 parts (a.b)→ in-cluster, namespace = 2nd label
else (3+ non-svc labels, IP, etc.)→ external
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

```yaml
spec:
  networkPolicy:
    allowedEgress:
      - to:
          - namespaceSelector:
              matchLabels:
                kubernetes.io/metadata.name: langfuse
        ports:
          - port: 3000
            protocol: TCP
```

`NetworkPolicySpec` contains `AllowedEgress []networkingv1.NetworkPolicyEgressRule`
— raw Kubernetes types validated by the API server on CR admission. No
operator-side validation needed.

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
  generated. Consistent with proxy allowlist behavior.
- Port defaults: HTTP → 80, HTTPS → 443. Only non-443 external ports generate
  proxy egress rules.
- `allowedEgress` rules are raw Kubernetes types — validated by the API server
  on CR admission, no operator-side validation needed.

---

## Interaction with existing features

| Feature | Interaction |
|---------|-------------|
| Proxy allowlist | Independent — allowlist is L7; this is L4 NP rules |
| Kube API port injection | Complementary — handles k8s API ports on proxy egress; MCP ports deduplicate against these |
| Metrics ingress rules | Parallel pattern — same approach (append rules to existing NP) |
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

---

## Future considerations

- `spec.networkPolicy` grouping leaves room for additions: `allowedEgressCIDRs`,
  `additionalIngress`, `enabled: false` (disable NPs entirely).
- Proxy egress customization could be added as a separate field if needed.
