# MCP and Diagnostics Egress — Design Questions

**Status:** Resolved — all decisions made
**Related:** [Design document](mcp-diagnostics-egress-design.md)

Each question has options with trade-offs and a recommendation. Go through them
one by one to form the design, then update the design document.

---

## Q1: Scope — MCP egress only, or also diagnostics/tracing?

The same gateway egress NP blocks both in-cluster MCP servers and
cross-namespace tracing endpoints. Both scenarios involve parsing a declared URL
to derive egress rules. The question is whether to solve both in one feature.

### Option E: MCP auto-egress + `spec.networkPolicy.allowedEgress` escape hatch

Auto-generate egress rules from `spec.mcpServers[].url` (typed field, operator
knows the URL). For everything else (diagnostics, tracing, custom services),
provide a `spec.networkPolicy.allowedEgress` escape hatch where users declare
raw `NetworkPolicyEgressRule` objects — following the upstream community
operator's pattern (`additionalEgress`).

- **Pro:** Auto-configures what we're certain about (MCP URLs are typed, the
  operator should "just work" when a user declares an MCP server).
- **Pro:** Escape hatch covers all other cases (Langfuse, MLflow, Postgres,
  Ollama, webhooks) without fragile JSON path parsing of `spec.config.raw`.
- **Pro:** Follows the upstream community operator's proven pattern.
- **Pro:** No new abstractions — raw K8s NP rules are what users already write
  for manual workarounds today.
- **Pro:** `spec.networkPolicy` grouping leaves room for future additions
  (`allowedEgressCIDRs`, `additionalIngress`, `enabled: false`).

**Decision:** Option E — auto-generate for MCP URLs (typed field, operator
should "just work") + `spec.networkPolicy.allowedEgress` escape hatch for
everything else (upstream pattern).

_Considered and rejected: Option A (MCP only — doesn't solve Item 7), Option B
(parse `spec.config.raw` for OTEL — fragile JSON walking), Option C (generic
`spec.egressRules` — overlaps with raw NP escape hatch), Option D (escape
hatches only — misses turnkey MCP auto-egress opportunity)._

---

## Q2: In-cluster URL detection — heuristic vs explicit?

The operator needs to distinguish in-cluster URLs (gateway connects directly,
needs gateway egress NP rule) from external URLs (traffic goes through proxy,
may need proxy egress NP port). This determines which NetworkPolicy gets
modified.

### Option A: Kubernetes DNS heuristic

Classify by hostname pattern:
- Bare name (`mcp-customer`) → in-cluster, same namespace
- `svc.ns` or `svc.ns.svc` or `svc.ns.svc.cluster.local` → in-cluster,
  namespace `ns`
- Anything else (public domain, IP address) → external

- **Pro:** Zero configuration — works automatically from existing URLs.
- **Pro:** Matches what `NO_PROXY` actually does (`.svc`, `.svc.cluster.local`).
- **Pro:** OpenShift (our only target) always uses `cluster.local` and always
  has `kubernetes.io/metadata.name` on namespaces (ships k8s 1.23+).

**Decision:** Option A — Kubernetes DNS heuristic. OpenShift always uses
`cluster.local` and the heuristic matches `NO_PROXY` behavior exactly.

### Classification rules

```
hostname has no dots              → in-cluster, same namespace
hostname ends .svc.cluster.local  → in-cluster, namespace = 2nd label
hostname ends .svc                → in-cluster, namespace = 2nd label
hostname has exactly 2 parts (a.b)→ in-cluster, namespace = 2nd label
else (3+ non-svc labels, IP, etc.)→ external
```

If the extracted namespace matches the Claw instance's own namespace, treat as
same-namespace (simpler rule, no `namespaceSelector`).

### Examples

**Same namespace** (bare service name, no dots):

```yaml
mcpServers:
  customer-api:
    url: http://mcp-customer:9001/mcp       # hostname "mcp-customer" → same NS
```

**Cross-namespace** (dotted Kubernetes service DNS):

```yaml
mcpServers:
  shared-db:
    url: http://mcp-server.shared-tools:9001/mcp                  # → NS "shared-tools"
  shared-db-svc:
    url: http://mcp-server.shared-tools.svc:9001/mcp              # → NS "shared-tools"
  shared-db-fqdn:
    url: http://mcp-server.shared-tools.svc.cluster.local:9001    # → NS "shared-tools"
```

All three resolve to namespace `shared-tools`. The heuristic extracts the
second DNS label as the namespace.

A FQDN pointing to the instance's own namespace (e.g.,
`http://mcp-customer.my-ns.svc.cluster.local:9001`) is detected as same-
namespace because the extracted namespace matches `instance.Namespace`.

**External** (public hostname, IP, or non-Kubernetes DNS):

```yaml
mcpServers:
  cloud-mcp:
    url: https://mcp.example.com/mcp           # public hostname, port 443 → no NP change
  cloud-nonstandard:
    url: https://mcp.example.com:8443/mcp      # public hostname, port 8443 → proxy egress port added
  ip-based:
    url: http://203.0.113.50:9001/mcp           # IP → external, port 9001 → proxy egress port added
```

External URLs on port 443 need no NP change (already allowed). Non-443 ports
are added to the proxy egress NP.

**Stdio** (no URL, no egress effect):

```yaml
mcpServers:
  filesystem:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
```

No `url` field → stdio transport, subprocess in-container. No NP changes.

_Considered and rejected: Option B (explicit `internal` flag — unnecessary
config burden, users will forget), Option C (parse NO_PROXY — overengineered)._

---

## Q3: Same-namespace in-cluster egress — port-only rule vs podSelector?

For in-cluster MCP servers in the same namespace as the Claw instance, how
granular should the auto-generated gateway egress rule be?

### Option B: Same-namespace podSelector with port

```yaml
# Auto-generated from: url: http://mcp-customer:9001/mcp
egress:
  - to:
      - podSelector: {}  # any pod in same namespace
    ports:
      - port: 9001
        protocol: TCP
```

- **Pro:** Scoped to the same namespace only — no cross-namespace leak.
- **Pro:** No target label knowledge needed (`podSelector: {}` means "any pod
  in this namespace").
- **Pro:** Clear security boundary — tighter than port-only (Option A).

**Decision:** Option B — `podSelector: {}` with port. Scoped to same-namespace
pods without requiring knowledge of target labels.

_Considered and rejected: Option A (port-only, no `to` — allows port to any
destination including cross-namespace/external), Option C (targeted podSelector
with specific labels — requires Service lookup, fragile)._

---

## Q4: Cross-namespace egress — namespace targeting strategy

For in-cluster MCP servers in a different namespace (e.g.,
`http://mcp-server.shared-tools:9001`), how should the auto-generated gateway
egress rule identify the target namespace?

### Option A: Specific namespace by well-known label

Use `kubernetes.io/metadata.name`, an immutable label automatically set on all
namespaces. Guaranteed on OpenShift 4.x (ships k8s 1.23+).

```yaml
# Auto-generated from: url: http://mcp-server.shared-tools:9001/mcp
egress:
  - to:
      - namespaceSelector:
          matchLabels:
            kubernetes.io/metadata.name: shared-tools
    ports:
      - port: 9001
        protocol: TCP
```

- **Pro:** Precise — only the declared target namespace.
- **Pro:** No user configuration needed — namespace name parsed from the URL.
- **Pro:** Guaranteed to exist on OpenShift.

**Decision:** Option A — `kubernetes.io/metadata.name` label with namespace
extracted from the URL. Precise scoping, zero configuration.

_Considered and rejected: Option B (wildcard `namespaceSelector: {}` — allows
port in any namespace, broader than necessary), Option C (user-specified label
— redundant, namespace is already in the URL)._

---

## Q5: External non-443 ports — add to proxy egress NP?

For external MCP servers on non-standard ports (e.g.,
`https://mcp.example.com:8443`), should the operator add the port to the proxy
egress NetworkPolicy?

### Option A: Yes — add parsed ports to proxy egress

Follow the existing `injectKubePortsIntoNetworkPolicy` pattern: append a TCP
port rule for each unique non-443 external port.

- **Pro:** Consistent with existing pattern (kube API ports already do this).
- **Pro:** Removes a blocker users don't expect.
- **Pro:** Proxy L7 allowlist remains the primary enforcement layer.

**Decision:** Option A — add non-443 external MCP ports to proxy egress NP.
Consistent with existing kube API port injection pattern.

_Considered and rejected: Option B (require 443 only — inconsistent with kube
API auto-handling, blocks legitimate use cases)._

---

## Q6: Diagnostics endpoint source — parse spec.config.raw or new typed field?

**Superseded by Q1.** The Q1 decision (Option E) scopes auto-egress to MCP URLs
only. Diagnostics/tracing endpoints are handled via the
`spec.networkPolicy.allowedEgress` escape hatch — users declare raw NP rules
for tracing collectors, Langfuse, MLflow, etc. No `spec.config.raw` parsing or
new typed diagnostics fields needed.

---

## Q7: Unparseable URL handling — skip silently vs fail reconciliation?

If an MCP URL can't be parsed (malformed, no hostname), should the operator
skip it or fail?

### Option A: Skip silently — no egress rule generated

Log a warning, continue reconciliation. Consistent with
`mcpPassthroughRoutes()` which silently skips unparseable URLs for the proxy
allowlist.

- **Pro:** Non-blocking — NP rules are a convenience, not a correctness
  requirement.
- **Pro:** Consistent with existing proxy allowlist behavior.

**Decision:** Option A — skip and log. Matches existing `mcpPassthroughRoutes`
behavior. Users can use `spec.networkPolicy.allowedEgress` for edge cases.

_Considered and rejected: Option B (fail reconciliation — disproportionate,
could break existing CRs), Option C (condition warning — extra status
complexity for a rare edge case)._

---

## Q8: NP rule strategy — append to existing NP vs create supplemental NP?

Should auto-generated and user `allowedEgress` rules be appended to the
existing `{instance}-egress` / `{instance}-proxy-egress` NPs, or created as
separate NP resources?

### Option A: Append to existing NPs

Modify the existing NP objects before server-side apply, adding rules to the
`spec.egress[]` array. Follows `addMetricsIngressRule` and
`injectKubePortsIntoNetworkPolicy` patterns.

- **Pro:** Consistent with all existing NP modifications.
- **Pro:** No new resources to manage. Self-cleaning — remove an MCP server,
  rule disappears next reconcile.
- **Pro:** Single source of truth per direction per component.

**Decision:** Option A — append to existing NPs. Consistent with established
patterns, self-cleaning via server-side apply.

_Considered and rejected: Option B (supplemental NPs — more resources to
manage, breaks established inline modification pattern)._
