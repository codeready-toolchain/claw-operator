# ADR-0019: In-Cluster Traffic Control

**Status:** Implemented
**Date:** 2026-06-01

## Overview

The claw-operator creates gateway pods that run OpenClaw — an AI coding agent capable of executing arbitrary code. Previously, in-cluster traffic (to any Kubernetes Service in `.svc` or `.svc.cluster.local`) bypassed the MITM proxy entirely via the `NO_PROXY` environment variable, and Kubernetes service links leaked namespace service topology as environment variables.

This ADR introduces three changes:

1. **`enableServiceLinks: false`** on all managed pod specs (unconditional hardening, no CRD field)
2. **`spec.network`** — a refactored CRD section replacing `spec.networkPolicy`, with `inClusterBypass` (controls proxy bypass for in-cluster traffic) and `additionalEgress` (renamed from `allowedEgress`)
3. **`credentialRef` on `McpServerSpec`** — proxy-injected credential support for HTTP MCP servers, keeping tokens out of the gateway pod

## Design Principles

- **Secure by default** — `inClusterBypass` defaults to `false`; the gateway cannot reach arbitrary in-cluster services
- **Uniform credential injection** — MCP servers get the same proxy-based auth as LLM providers and custom providers
- **NP rules match traffic flow** — NetworkPolicy rules reflect the actual path (gateway→proxy→target when bypass is off)
- **Gateway-focused** — only the gateway pod (which runs untrusted code) is restricted; proxy and device-pairing pods are unaffected

## Architecture

### When `inClusterBypass: false` (default)

```
Gateway Pod
├── enableServiceLinks: false (always)
├── HTTP_PROXY / HTTPS_PROXY → proxy:8080
├── NO_PROXY → localhost,127.0.0.1,{instance}-proxy
│                 (no .svc, no .svc.cluster.local)
├── NP ({instance}-egress): gateway → proxy + DNS only
│
Proxy Pod
├── Proxy config routes:
│   ├── LLM providers (credential injection)
│   ├── MCP servers with credentialRef (credential injection)
│   ├── MCP servers without credentialRef (passthrough, injector: "none")
│   └── Builtins (clawhub.ai, registry.npmjs.org, etc.)
├── NP ({instance}-proxy-egress): proxy → HTTPS:443 + DNS + in-cluster MCP targets
```

All egress from the gateway flows through the proxy. The proxy's L7 allowlist determines what's reachable. MCP servers with `credentialRef` get credential injection; those without get passthrough. The gateway never sees raw tokens.

### When `inClusterBypass: true`

```
Gateway Pod
├── enableServiceLinks: false (always)
├── HTTP_PROXY / HTTPS_PROXY → proxy:8080
├── NO_PROXY → localhost,127.0.0.1,{instance}-proxy,.svc,.svc.cluster.local
│                 (all in-cluster traffic bypasses proxy)
├── NP ({instance}-egress): gateway → proxy + DNS + in-cluster MCP targets
│
Proxy Pod
├── NP ({instance}-proxy-egress): proxy → HTTPS:443 + DNS (current behavior)
```

In-cluster traffic goes directly from gateway to target. MCP servers with `credentialRef` are a configuration error in this mode — the proxy is bypassed, so credentials can't be injected.

## CRD Changes

### `spec.network` (replaces `spec.networkPolicy`)

```yaml
spec:
  network:
    inClusterBypass: false        # default; all egress through proxy
    additionalEgress:             # renamed from allowedEgress
      - to: [...]
        ports: [...]
```

- `inClusterBypass` (`*bool`, default `false`) — controls whether the gateway pod can directly reach in-cluster Kubernetes services, bypassing the MITM proxy
- `additionalEgress` (`[]NetworkPolicyEgressRule`) — appends raw NetworkPolicy egress rules to the gateway's egress policy for targets the operator can't auto-detect

### `McpServerSpec.credentialRef` (new field)

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

CRD validation enforces that `credentialRef` is only allowed for HTTP MCP servers (those with `url`).

### Migration from `spec.networkPolicy`

```yaml
# Before
spec:
  networkPolicy:
    allowedEgress:
      - to: [...]

# After
spec:
  network:
    inClusterBypass: true  # explicit opt-in to preserve current behavior
    additionalEgress:
      - to: [...]
```

## Decision Summary

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| Q1 | Field name and location | `spec.network.inClusterBypass` + `additionalEgress` (refactor from `spec.networkPolicy`) | Groups all network concerns under one concept-based name instead of naming after a K8s resource; `additionalEgress` is clearer than `allowedEgress`; extensible for future network knobs |
| Q2 | Default value | `false` (secure by default) | Already breaking compat with the section rename — consolidate into one migration; forces conscious opt-in for in-cluster access |
| Q3 | In-cluster MCP servers | Add `credentialRef` to `McpServerSpec` for proxy-injected auth; passthrough for unauthenticated. `credentialRef` works for external MCP servers unconditionally and for in-cluster servers only when `inClusterBypass` is `false`; setting `credentialRef` with `inClusterBypass: true` is a validation error (proxy is bypassed, credentials can't be injected) | Gateway never sees raw MCP tokens (same security model as LLM providers); follows established `CustomProviderSpec.credentialRef` pattern |
| Q4 | NetworkPolicy rules | Adjust to match traffic path — gateway or proxy egress based on `inClusterBypass` | NP rules accurately reflect traffic flow; tighter security since gateway can only reach proxy when bypass is off |
| Q5 | Scope | Gateway only — proxy and device-pairing unaffected | Proxy must reach in-cluster MCP targets for Q3 to work; device-pairing is operator-controlled infrastructure; neither runs untrusted code |

## Future Considerations

- `spec.network` is extensible for future network knobs such as `dnsPolicy`, `proxyTimeout`, etc.
- `credentialRef` could be extended to support additional credential injection patterns beyond bearer tokens
