## Context

Current state:
- Controller has hardcoded filter: only reconciles Claw instances named "instance"
- All created resources use fixed names (e.g., `claw`, `claw-proxy`, `claw-gateway-token`)
- Filter implemented in `Reconcile()` and `findClawsReferencingSecret()` via `ClawInstanceName` constant
- Single instance per namespace limitation prevents multi-tenancy and testing scenarios

Constraints:
- Resource naming must avoid conflicts between multiple Claw instances
- Labels and selectors must uniquely identify resources per Claw instance
- Route, Service and NetworkPolicy selectors must target correct pods per instance

## Goals / Non-Goals

**Goals:**
- Remove arbitrary instance name restriction
- Support multiple Claw instances per namespace
- Use Claw CR name as base for all resource names
- Update all resource creation logic to use dynamic names
- Preserve all existing functionality (labels, owner references, selectors and status updates)

**Non-Goals:**
- Changing the Claw CRD schema
- Adding new configuration options
- Cross-namespace resource management
- Shared resource pooling between instances

## Decisions

### 1. Resource Naming Strategy

**Decision:** Use Claw CR name directly as the base name for all resources

Naming pattern:
- Gateway deployment: `{claw-name}` (was `claw`)
- Gateway service: `{claw-name}` (was `claw`)
- Gateway route: `{claw-name}` (was `claw`)
- Proxy deployment: `{claw-name}-proxy` (was `claw-proxy`)
- Proxy service: `{claw-name}-proxy` (was `claw-proxy`)
- Gateway token secret: `{claw-name}-gateway-token` (was `claw-gateway-token`)
- ConfigMap: `{claw-name}-config` (was `claw-config`)
- PVC: `{claw-name}-home-pvc` (was `claw-home-pvc`)
- Proxy CA ConfigMap: `{claw-name}-proxy-ca` (was `claw-proxy-ca`)
- Vertex ADC ConfigMap: `{claw-name}-vertex-adc` (was `claw-vertex-adc`)
- Kubeconfig ConfigMap: `{claw-name}-kube-config` (was `claw-kube-config`)
- NetworkPolicies: `{claw-name}-ingress`, `{claw-name}-egress`, `{claw-name}-proxy-egress`

**Rationale:**
- Simple and predictable: resource name = CR name (+ optional suffix)
- Easy debugging: `kubectl get claw my-instance` → `kubectl get deploy my-instance`
- DNS-safe: Kubernetes validates CR names, so derived names are automatically valid
- No prefix/suffix collisions: `-proxy`, `-gateway-token` suffixes are unambiguous

**Alternatives considered:**
- **Hash-based naming** (e.g., `claw-abc123`): Rejected because it obscures the relationship between CR and resources
- **Namespace prefix** (e.g., `ns1-instance`): Rejected because Kubernetes already provides namespace isolation
- **UUID suffix** (e.g., `instance-uuid`): Rejected as unnecessary complexity

### 2. Backward Compatibility Strategy

**Decision:** No migration needed - naming is deterministic based on CR name

For existing deployments where Claw CR is named "instance":
- All resources keep their current names (instance → claw, instance-proxy, etc.)
- Status updates continue to work
- No data migration or downtime required

**Rationale:** The current hardcoded names happen to match what the new dynamic naming would generate for a CR named "instance"

### 3. Label Selector Updates

**Decision:** Add `claw.sandbox.redhat.com/instance: {claw-name}` label to all resources

Current labels:
```yaml
app.kubernetes.io/name: claw
```

Additional label:
```yaml
claw.sandbox.redhat.com/instance: {claw-name}
```

Applied via kustomization commonLabels with dynamic substitution during resource building.

**Rationale:**
- Maintains existing `app.kubernetes.io/name: claw` for cluster-wide queries
- New label enables per-instance filtering: `kubectl get all -l claw.sandbox.redhat.com/instance=my-instance`
- NetworkPolicies can target specific instance pods
- Service selectors can disambiguate between instances

**Alternatives considered:**
- **Changing app.kubernetes.io/name to include instance**: Rejected to preserve existing label-based queries
- **Using only resource names for selection**: Rejected because NetworkPolicies and Services need label selectors

### 4. Constants Refactoring

**Decision:** Convert resource name constants to helper functions

Before:
```go
const (
    ClawInstanceName = "instance"
    ClawDeploymentName = "claw"
    ClawGatewaySecretName = "claw-gateway-token"
    // ...
)
```

After:
```go
func getClawDeploymentName(instanceName string) string {
    return instanceName
}

func getProxyDeploymentName(instanceName string) string {
    return instanceName + "-proxy"
}

func getGatewaySecretName(instanceName string) string {
    return instanceName + "-gateway-token"
}
```

**Rationale:**
- Centralizes naming logic
- Type-safe and refactor-friendly
- Self-documenting via function names
- Easy to test naming conventions

### 5. Kustomize Resource Naming

**Decision:** Inject instance name into kustomize-built resources via `namePrefix`

Approach:
1. Build manifests from embedded kustomization (as before)
2. After parsing YAML objects, iterate and set names dynamically:
   - Find resources by kind (Deployment, Service, etc.)
   - Set name using helper functions (`getClawDeploymentName(instance.Name)`)
3. Set namespace and owner references (as before)

**Rationale:**
- Kustomize `namePrefix` applies globally, but we need different prefixes for gateway vs proxy
- Post-build name injection gives fine-grained control
- Simpler than maintaining multiple kustomization variants

**Alternatives considered:**
- **Kustomize namePrefix**: Rejected because proxy resources need different suffix pattern
- **Separate kustomizations per instance**: Rejected as over-engineering for name substitution

### 6. NetworkPolicy Selector Updates

**Decision:** Update NetworkPolicy pod selectors to include instance label

Before:
```yaml
podSelector:
  matchLabels:
    app.kubernetes.io/name: claw
```

After:
```yaml
podSelector:
  matchLabels:
    app.kubernetes.io/name: claw
    claw.sandbox.redhat.com/instance: {claw-name}
```

**Rationale:** Ensures network policies only apply to pods of the specific Claw instance, not all instances

## Risks / Trade-offs

**[Risk]** Existing instances with name != "instance" suddenly start reconciling  
→ **Mitigation:** None exist today (enforced by current filter). Document the change in release notes.

**[Risk]** Resource name length limits (DNS-1123: max 63 characters)  
→ **Mitigation:** Kubernetes validates CR names to 63 chars. Longest suffix is `-gateway-token` (14 chars), leaving 49 chars for instance name. Document this in CRD validation or admission webhook.

**[Risk]** Confusion if users name a Claw instance "claw" (would create `claw` deployment)  
→ **Acceptable:** Valid use case. No conflict as long as instance names are unique in namespace.

**[Risk]** Tests may assume fixed resource names  
→ **Mitigation:** Update test fixtures to use explicit instance names, add multi-instance test cases

**[Trade-off]** More verbose resource names (e.g., `my-openclaw-instance-proxy`)  
→ **Benefit:** Clear ownership and easier to identify resources belonging to specific instance

**[Trade-off]** Helper functions instead of constants add minor indirection  
→ **Benefit:** Centralized naming logic prevents mistakes, makes future changes easier
