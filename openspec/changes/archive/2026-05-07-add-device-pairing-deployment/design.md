## Context

The operator currently deploys two component groups via embedded Kustomize manifests: `claw` (gateway) and `claw-proxy`. Each group lives in its own subdirectory under `internal/assets/manifests/` with a `kustomization.yaml` and resource YAMLs. The controller's `buildKustomizedObjects()` loads both groups, performs `CLAW_INSTANCE_NAME` template replacement, builds each via Kustomize, and merges the resulting objects. Routes are applied in Phase 2 (`applyRouteOnly`), and all remaining resources in Phase 3.

A new device pairing web application (`claw-device-pairing`) needs to be deployed alongside these existing components, sharing the same Route host but serving traffic on a `/integration/device-pairing` path prefix.

## Goals / Non-Goals

**Goals:**
- Deploy the `claw-device-pairing` application as an operator-managed component with ServiceAccount, Deployment, Service, and Route
- Follow the existing manifest embedding and Kustomize build patterns exactly
- Share the Claw Route host using path-based routing at `/integration/device-pairing`
- Apply device-pairing resources in the same reconciliation cycle, after claw and claw-proxy

**Non-Goals:**
- Adding new CRD fields to configure the device-pairing application (image is hardcoded in the manifest)
- Health checks or readiness dependencies between claw-device-pairing and the gateway/proxy
- Network policies for the device-pairing component (can be added later if needed)
- Exposing the device-pairing service on vanilla Kubernetes (Route is OpenShift-only; same graceful skip as the claw Route)

## Decisions

### 1. Manifest structure: separate Kustomize directory

The device-pairing manifests live in `internal/assets/manifests/claw-device-pairing/` with their own `kustomization.yaml`, following the same pattern as `claw/` and `claw-proxy/`. This keeps each component self-contained.

**Alternative considered**: Adding the resources to the existing `claw/` kustomization. Rejected because it conflates concerns and makes it harder to reason about or remove the device-pairing component independently.

### 2. Route host sharing via path-based routing

The device-pairing Route sets `.spec.host` to match the Claw Route host (obtained from `getRouteURL()`) and uses `.spec.path: /integration/device-pairing`. OpenShift HAProxy router supports path-based routing natively with longest-prefix matching.

The Route host is injected into the device-pairing Route manifest the same way it's injected into the ConfigMap: by replacing a `OPENCLAW_ROUTE_HOST` placeholder in the Route's `.spec.host` field after the Claw Route status is resolved. Since all Route objects are already filtered and applied together in `applyRouteOnly()`, the device-pairing Route is naturally included.

However, the host injection needs a new step: `injectRouteHostIntoDevicePairingRoute()` that replaces the placeholder in the device-pairing Route's `.spec.host` field. This runs after `getRouteURL()` resolves the host, alongside the existing `injectRouteHostIntoConfigMap()`.

### 3. Resource naming and labeling

All resources use `CLAW_INSTANCE_NAME-device-pairing` as their name in the YAML templates. The existing `CLAW_INSTANCE_NAME` replacement in `buildKustomizedObjects()` handles this automatically since it does a global `bytes.ReplaceAll` on the manifest content.

The `kustomization.yaml` applies `app.kubernetes.io/name: claw-device-pairing` to all resources via the Kustomize `labels` directive. This follows the same label key as the existing `claw` and `claw-proxy` groups (`app.kubernetes.io/name`) but with a distinct value (`claw-device-pairing`) for independent selection by Services and selectors.

### 4. ServiceAccount inclusion

A dedicated ServiceAccount (`CLAW_INSTANCE_NAME-device-pairing`) is created for the device-pairing Deployment. This follows security best practices — the device-pairing pod runs with its own identity rather than the default ServiceAccount, enabling future RBAC scoping.

### 5. Deployment in Phase 3 (alongside remaining resources)

The device-pairing resources (ServiceAccount, Deployment, Service) are applied in Phase 3 alongside other non-Route resources. The device-pairing Route is applied in Phase 2 alongside the Claw Route via `applyRouteOnly()`, which already filters all Route-kind objects. This means no changes to the phase structure — the existing three-phase reconciliation handles the new component naturally.

## Risks / Trade-offs

- **[Risk] Route host not available on vanilla Kubernetes** → Same graceful skip as the Claw Route. The device-pairing Route is silently skipped when the Route CRD is not registered. The Service remains accessible via port-forward.
- **[Risk] Path-based routing conflicts** → The `/integration/device-pairing` path prefix is specific enough to avoid conflicts with the gateway's paths. OpenShift uses longest-prefix matching.
- **[Trade-off] Hardcoded image in manifest** → The `quay.io/xcoulon/claw-device-pairing:latest` image is embedded in the manifest. To make it configurable, a future change could add a `DevicePairingImage` field to the controller (like `ProxyImage`) with env var override. Not needed now.
- **[Risk] Route host placeholder injection timing** → The device-pairing Route needs the Claw Route host, which is only available after Phase 2. Since the device-pairing Route is also applied in Phase 2 via `applyRouteOnly()`, the host injection must happen between applying the Claw Route and applying the device-pairing Route — or both Routes can be applied with the placeholder replaced beforehand. The chosen approach replaces the placeholder in all Route objects before `applyRouteOnly()` runs, which simplifies the flow.
