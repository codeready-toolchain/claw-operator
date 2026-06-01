# In-Cluster Traffic Control

**Status:** Final — all decisions resolved in [in-cluster-traffic-control-questions.md](in-cluster-traffic-control-questions.md)

## Overview

The claw-operator creates gateway pods that run OpenClaw — an AI coding agent capable of executing arbitrary code. Currently, in-cluster traffic (to any Kubernetes Service in `.svc` or `.svc.cluster.local`) bypasses the MITM proxy entirely via the `NO_PROXY` environment variable, and Kubernetes service links leak namespace service topology as environment variables.

This design introduces:

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
│                 (current behavior — all in-cluster traffic bypasses proxy)
├── NP ({instance}-egress): gateway → proxy + DNS + in-cluster MCP targets
│
Proxy Pod
├── NP ({instance}-proxy-egress): proxy → HTTPS:443 + DNS (current behavior)
```

In-cluster traffic goes directly from gateway to target (current behavior). MCP servers with `credentialRef` are a configuration error in this mode — the proxy is bypassed, so credentials can't be injected.

## CRD Changes

### `spec.network` (replaces `spec.networkPolicy`)

```go
type NetworkSpec struct {
    // InClusterBypass controls whether the gateway pod can directly reach
    // in-cluster Kubernetes services, bypassing the MITM proxy.
    // When false (default), all egress goes through the proxy.
    // When true, .svc and .svc.cluster.local traffic bypasses the proxy.
    // +optional
    // +kubebuilder:default=false
    InClusterBypass *bool `json:"inClusterBypass,omitempty"`

    // AdditionalEgress appends raw NetworkPolicy egress rules to the
    // gateway's egress policy for targets the operator can't auto-detect.
    // +optional
    AdditionalEgress []networkingv1.NetworkPolicyEgressRule `json:"additionalEgress,omitempty"`
}
```

### `McpServerSpec.credentialRef` (new field)

```go
type McpServerSpec struct {
    // ... existing fields ...

    // CredentialRef is the name of a credential in spec.credentials that
    // handles proxy routing and authentication for this MCP server's domain.
    // Only valid for HTTP MCP servers (url). The proxy injects credentials
    // so the gateway never sees raw tokens.
    // +optional
    CredentialRef string `json:"credentialRef,omitempty"`
}
```

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

## Implementation Plan (single PR)

### Manifests: `enableServiceLinks: false`

1. Add `enableServiceLinks: false` to all three deployment manifests (`claw/deployment.yaml`, `claw-proxy/proxy-deployment.yaml`, `claw-device-pairing/deployment.yaml`)
2. No `NormalizeDeployment()` change needed — it already defaults `nil` to `true` (line 87-88 of `claw_normalize.go`); the explicit `false` in manifests won't be overridden

### CRD: refactor `spec.network` + `inClusterBypass` + `McpServerSpec.credentialRef`

3. Replace `NetworkPolicySpec` with `NetworkSpec` in `api/v1alpha1/claw_types.go`
4. Rename `ClawSpec.NetworkPolicy` → `ClawSpec.Network`, update JSON tag from `networkPolicy` to `network`
5. Add `InClusterBypass *bool` field with `+kubebuilder:default=false`
6. Rename `AllowedEgress` → `AdditionalEgress`, update JSON tag from `allowedEgress` to `additionalEgress`
7. Add `CredentialRef string` to `McpServerSpec`
8. Add XValidation marker: `+kubebuilder:validation:XValidation:rule="!has(self.credentialRef) || has(self.url)",message="credentialRef is only allowed for HTTP MCP servers (url)"`
9. Run `make manifests generate`

### Controller: `NO_PROXY` + NP branching + credential-injecting MCP routes

10. Update controller: set `NO_PROXY` on gateway deployment based on `inClusterBypass` (remove `.svc,.svc.cluster.local` when false)
11. Update controller: branch NP generation on `inClusterBypass` — in-cluster MCP targets go to `injectMcpGatewayEgressRules()` (bypass on) or a new proxy-egress equivalent (bypass off)
12. Rename `injectAllowedEgress()` → `injectAdditionalEgress()` in `claw_egress.go` and update the call site in `claw_resource_controller.go` (line 781)
13. Update all field references from `instance.Spec.NetworkPolicy.AllowedEgress` to `instance.Spec.Network.AdditionalEgress`
14. Update `generateProxyConfig()`: when `credentialRef` is set, look up the referenced credential and generate a credential-injecting route (not passthrough) for the MCP server's domain
15. Update `mcpPassthroughRoutes()`: skip domains that have a `credentialRef` (they get a real route via step 14 instead)
16. Add cross-field validation in the controller: if any in-cluster MCP server has `credentialRef` and `inClusterBypass` is `true`, set a condition warning

### Tests + docs

17. Update tests to assert `enableServiceLinks: false` on all deployments
18. Update tests for `inClusterBypass` behavior (NO_PROXY values, NP rule placement)
19. Update tests for `credentialRef` proxy route generation
20. Update `docs/user-guide.md` sections: "Escape hatch: `spec.networkPolicy.allowedEgress`" and all `allowedEgress` references
21. Update `config/samples/`, `docs/architecture.md`, `CLAUDE.md` references
22. Update `PLATFORM.md` in `internal/assets/manifests/claw/configmap.yaml` — the seeded platform skill references `NO_PROXY` bypass behavior and `spec.networkPolicy.allowedEgress` (lines 319-333)

## Decision Summary

| # | Question | Decision |
|---|----------|----------|
| Q1 | Field name and location | `spec.network.inClusterBypass` + `additionalEgress` (refactor from `spec.networkPolicy`) |
| Q2 | Default value | `false` (secure by default) |
| Q3 | In-cluster MCP servers | Add `credentialRef` to `McpServerSpec` for proxy-injected auth; passthrough for unauthenticated |
| Q4 | NetworkPolicy rules | Adjust to match traffic path — gateway egress or proxy egress based on `inClusterBypass` |
| Q5 | Scope | Gateway only — proxy and device-pairing unaffected |
