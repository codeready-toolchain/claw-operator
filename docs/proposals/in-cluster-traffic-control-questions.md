# In-Cluster Traffic Control — Design Questions

**Status:** All questions resolved
**Related:** [Design document](in-cluster-traffic-control-design.md)

Each question has options with trade-offs and a recommendation. Go through them one by one to form the design, then update the design document.

## Q1: What should the CRD field be named and where should it live?

The field controls whether the gateway pod can directly reach in-cluster Kubernetes services (bypassing the proxy). It needs a clear name that communicates intent, and a sensible location in the spec hierarchy.

### Option E: Refactor `spec.networkPolicy` → `spec.network` with `inClusterBypass` + `additionalEgress` ✅

Rename `spec.networkPolicy` to `spec.network` (concept-first naming instead of Kubernetes resource name), rename `allowedEgress` to `additionalEgress` (clearer that these are appended, not a complete allowlist), and add `inClusterBypass` as a sibling field.

```yaml
spec:
  network:
    inClusterBypass: false
    additionalEgress:
      - to: [...]
        ports: [...]
```

- **Pro:** Groups all network concerns under one concept-based name
- **Pro:** `inClusterBypass` is concise — bool semantics carry allow/deny, no redundant "allow" prefix
- **Pro:** `additionalEgress` is more accurate than `allowedEgress` — makes clear these are appended extras
- **Pro:** Extensible for future network knobs (`dnsPolicy`, `proxyTimeout`, etc.)
- **Pro:** Fixes the current naming issue where `spec.networkPolicy` is named after the K8s implementation detail

**Decision:** Option E — refactor `spec.networkPolicy` into `spec.network` with cleaner field names while adding `inClusterBypass`.

_Considered and rejected: Option A `spec.proxy.allowInClusterBypass` (proxy section too narrow — NP egress rules are also network config but not "proxy"), Option B `spec.networkPolicy.allowInClusterTraffic` (misleading section name — NO_PROXY is not a NetworkPolicy), Option C `spec.security.allowInClusterTraffic` ("security" too broad — everything in the operator is security-related), Option D `spec.allowInClusterTraffic` (top-level ClawSpec already at 14 fields, doesn't scale)_

## Q2: What should the default value be?

This balances backwards compatibility with the secure-by-default principle.

### Option B: Default to `false` (secure by default) ✅

New Claw instances have in-cluster traffic disabled. Users who need it must opt in.

- **Pro:** Secure by default — the AI agent in the gateway can't reach arbitrary services
- **Pro:** Forces users to consciously decide to allow in-cluster access
- **Pro:** We're already breaking compat with the `spec.networkPolicy` → `spec.network` rename — consolidate breaking changes into one migration
- **Con:** Breaking change for existing users who rely on in-cluster MCP servers
- **Con:** Requires documentation and migration guidance

**Decision:** Option B — default `false` (secure by default). Since we're already making a breaking change with the section rename, land on the secure posture in one go.

_Considered and rejected: Option A `true` default (insecure by default when we have the opportunity to fix it), Option C phased approach (two migrations instead of one)_

## Q3: How should in-cluster MCP servers work when `inClusterBypass` is `false`?

Since Q2 decided `inClusterBypass` defaults to `false`, this question is critical — in-cluster MCP servers are a common configuration and would stop working without special handling. When `inClusterBypass` is `false`, the gateway's `NO_PROXY` no longer includes `.svc`, so traffic to in-cluster MCP servers (e.g., `http://mcp-server:8080/mcp`) would be routed through the proxy.

Key insight: the proxy already has an HTTP forward handler (not just HTTPS CONNECT), and the operator already generates passthrough routes for MCP server domains via `mcpPassthroughRoutes()`. So routing in-cluster MCP traffic through the proxy is already functional — the `NO_PROXY` bypass just prevents it from happening today.

This opens a better question: should MCP servers get **credential injection** through the proxy (like LLM providers and custom providers do), rather than just passthrough?

### Option B: Add `credentialRef` to `McpServerSpec` for proxy-injected auth ✅

Add a `credentialRef` field to `McpServerSpec` (mirroring `CustomProviderSpec.credentialRef`). When set, the proxy injects credentials for the MCP server's domain — the gateway never sees the raw token. When `credentialRef` is omitted, the existing passthrough behavior applies (proxy allows traffic with `injector: "none"`).

```yaml
spec:
  credentials:
    - name: mcp-auth
      type: bearer
      secretRef:
        - name: mcp-token-secret
          key: token
      domain: mcp-server

  mcpServers:
    my-server:
      url: http://mcp-server:8080/mcp
      credentialRef: mcp-auth   # proxy injects bearer token
```

Traffic flow: `Gateway ──HTTP_PROXY──▶ Proxy ──inject token──▶ mcp-server:8080`

- **Pro:** Gateway never sees raw MCP tokens — same security model as LLM providers
- **Pro:** Follows the established `CustomProviderSpec.credentialRef` pattern
- **Pro:** Works for both in-cluster and external MCP servers
- **Pro:** Replaces the `envFrom` tier 3 pattern for HTTP MCP servers with a cleaner proxy-based approach
- **Pro:** Unauthenticated MCP servers still work (just omit `credentialRef`)
- **Con:** New CRD field + validation logic
- **Con:** Requires the MCP server's domain in the referenced credential to match the URL hostname

**Decision:** Option B — add `credentialRef` to `McpServerSpec`. The proxy injects credentials for MCP servers with `credentialRef`; unauthenticated MCP servers use the existing passthrough routes. Unifies all authenticated egress through the proxy.

_Considered and rejected: Option A passthrough-only (tokens still exposed in gateway env via `envFrom`), Option C NO_PROXY hostname exemptions (traffic escapes proxy, tokens in gateway env), Option D require explicit user action (poor UX with `false` default)_

## Q4: Should NetworkPolicy rules change based on `inClusterBypass`?

Q3 decided that all in-cluster MCP traffic flows through the proxy (gateway→proxy→target) when `inClusterBypass` is `false`. This means the gateway shouldn't need direct egress NP rules for MCP targets — but the proxy does.

### Option B: Adjust NP rules to match actual traffic path ✅

When `inClusterBypass` is `false`:
- **Skip** in-cluster MCP egress rules on `{instance}-egress` (gateway only talks to proxy)
- **Add** in-cluster MCP egress rules to `{instance}-proxy-egress` (proxy talks to MCP targets)

When `inClusterBypass` is `true`: keep current behavior (gateway egress rules for direct connections).

- **Pro:** NP rules accurately reflect traffic flow — gateway can only reach proxy, proxy reaches MCP targets
- **Pro:** Tighter security — gateway cannot reach in-cluster services even if `NO_PROXY` is tampered with
- **Pro:** Implementation is trivial — an `if/else` around two existing function calls (`injectMcpGatewayEgressRules` vs `injectMcpProxyEgressPorts` with in-cluster targets)

**Decision:** Option B — adjust NP rules to match traffic path. Follows directly from Q3: since all in-cluster MCP traffic goes through the proxy when `inClusterBypass` is `false`, NP rules should reflect that. The reconciler already handles both gateway and proxy NP manipulation.

_Considered and rejected: Option A keep gateway rules unchanged (misleading — grants unused direct access), Option C keep both gateway + proxy rules (overly permissive — gateway has NP rules for direct access it shouldn't use)_

## Q5: Should `inClusterBypass` apply to the proxy and device-pairing deployments too?

Q3 decided that the proxy must reach in-cluster MCP targets when `inClusterBypass` is `false`. Restricting the proxy's in-cluster access would break that design.

### Option A: Gateway only ✅

The field only controls `NO_PROXY` on the gateway (claw) deployment and its init containers. The proxy and device-pairing pods are unaffected.

- **Pro:** Focused — the gateway is where the security concern exists (arbitrary code execution)
- **Pro:** Proxy must reach in-cluster MCP targets (Q3) — restricting it would break the design
- **Pro:** Device-pairing needs a service account and cluster access by design

**Decision:** Option A — gateway only. The proxy must reach in-cluster MCP targets for Q3 to work. The device-pairing pod is operator-controlled infrastructure. Neither runs untrusted code.

_Considered and rejected: Option B all deployments (would break Q3's proxy-routed MCP traffic, and restricts pods that don't run untrusted code)_
