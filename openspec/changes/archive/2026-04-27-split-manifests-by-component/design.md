## Context

Current state:
- All manifests live in `internal/assets/manifests/` (11 files total: 1 kustomization, 5 claw resources, 3 proxy resources, 2 network policies)
- Single `kustomization.yaml` manages all resources
- Controller uses `krusty.MakeKustomizer().Run()` once to build all manifests
- Resources are differentiated by naming convention (`claw-*` vs `claw-proxy-*`)

Desired state:
- Component-based directory structure:
  - `internal/assets/manifests/claw/` with its own `kustomization.yaml`
  - `internal/assets/manifests/claw-proxy/` with its own `kustomization.yaml`
- Controller builds kustomize from each component directory separately
- Clear ownership: each directory contains only resources for that component

## Goals / Non-Goals

**Goals:**
- Improve code organization and maintainability by grouping related manifests
- Make it easier to identify which resources belong to which component
- Enable potential future improvements (e.g., component-specific labels, independent versioning)
- Maintain backward compatibility (no changes to deployed resources)

**Non-Goals:**
- Changing resource definitions or behavior
- Modifying how resources are applied (still using server-side apply)
- Changing embedded filesystem structure (still using `//go:embed manifests`)
- Creating a parent kustomization (each component is independent)

## Decisions

### 1. Manifest File Allocation

**Decision:** Split manifests by primary ownership:

`claw/` directory (gateway component):
- `deployment.yaml` — OpenClaw gateway
- `service.yaml` — ClusterIP service for gateway
- `route.yaml` — OpenShift Route
- `configmap.yaml` — OpenClaw configuration (operator.json, openclaw.json, AGENTS.md, KUBERNETES.md)
- `pvc.yaml` — Persistent storage
- `networkpolicy.yaml` — Egress NetworkPolicy for claw → proxy
- `ingress-networkpolicy.yaml` — Ingress NetworkPolicy for router → gateway

`claw-proxy/` directory (proxy component):
- `proxy-deployment.yaml` — Nginx MITM proxy
- `proxy-service.yaml` — ClusterIP service for proxy
- `proxy-configmap.yaml` — Nginx configuration
- (Future) `proxy-egress-networkpolicy.yaml` — Egress NetworkPolicy for proxy → internet (currently in claw/)

**Rationale:** 
- NetworkPolicies placed with the pod they protect (ingress rules with target, egress rules with source)
- Proxy egress NetworkPolicy could move to `claw-proxy/` in the future, but keeping it in `claw/` initially since controller modifies it for kubernetes credential ports
- Each component is self-contained and can be understood independently

**Alternatives considered:**
- **Shared directory for NetworkPolicies**: Rejected because it scatters related resources
- **Moving proxy egress to proxy folder now**: Deferred to keep this change minimal (controller currently modifies it via `injectKubePortsIntoNetworkPolicy`)

### 2. Controller Build Strategy

**Decision:** Build kustomize from each component directory separately, merge results into single object list

Implementation approach:
```go
// Build claw manifests
clawObjects, err := buildKustomizeFromPath(ctx, "manifests/claw")
// Build proxy manifests
proxyObjects, err := buildKustomizeFromPath(ctx, "manifests/claw-proxy")
// Merge
allObjects := append(clawObjects, proxyObjects...)
// Continue with existing logic (injection, filtering, apply)
```

**Rationale:**
- Minimal changes to existing reconciliation flow
- Both component builds succeed or fail together (atomic)
- Merged objects go through same injection/filtering/apply pipeline

**Alternatives considered:**
- **Sequential apply**: Build and apply claw, then build and apply proxy. Rejected because it complicates error handling and status updates (partial success scenarios)
- **Single kustomization with components**: Keep top-level kustomization.yaml that references subdirectories as components. Rejected because user explicitly wants no top-level kustomization

### 3. Embedded Filesystem

**Decision:** Keep `//go:embed manifests` directive unchanged

The embed captures the entire `manifests/` directory tree, so subdirectories are automatically included. No changes needed to `manifests.go`.

**Rationale:** Go's embed directive is recursive by default

### 4. Kustomization Labels

**Decision:** Apply `app.kubernetes.io/name: claw` label in both component kustomizations

Each `kustomization.yaml` includes:
```yaml
commonLabels:
  app.kubernetes.io/name: claw
```

**Rationale:** Maintains existing behavior where all resources are queryable with `kubectl get all -l app.kubernetes.io/name=claw`. Future improvement could add component-specific labels (e.g., `app.kubernetes.io/component: gateway` vs `app.kubernetes.io/component: proxy`), but that's out of scope.

## Risks / Trade-offs

**[Risk]** Controller build logic becomes slightly more complex (two builds instead of one)  
→ **Mitigation:** Extracted helper function `buildKustomizeFromPath` reduces duplication, error handling remains centralized

**[Risk]** Tests may break if they reference manifest paths or expect single kustomization  
→ **Mitigation:** Update test helpers to use component-specific paths, verify test coverage still works

**[Trade-off]** Slightly more kustomization boilerplate (two `kustomization.yaml` files instead of one)  
→ **Benefit:** Explicit component boundaries, easier to understand which resources belong together

**[Trade-off]** Cannot use kustomize's built-in resource dependencies across components  
→ **Acceptable:** Controller already handles resource ordering via filtering and multi-phase reconciliation, not kustomize
