# ADR-0022: Gateway ServiceAccount for Workload Identity

**Status:** Implemented
**Date:** 2026-07-09

## Overview

Agents need to exchange data with cloud storage services (S3, GCS, Azure
Blob) for backup/restore, document import/export, and dataset access.
The proxy's credential injection model cannot support this because cloud
storage APIs use request-level signing (AWS SigV4, GCP signed requests)
rather than simple header injection — the proxy would need to strip the
client's cryptographic signature and re-sign every request, effectively
reimplementing the full S3/GCS protocol. This is a fundamental mismatch,
not a gap that can be closed with a new injector type.

Without operator support, the only workaround is static long-lived cloud
credentials configured inside the agent container (e.g., `rclone config`
with `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`). This puts permanent
secrets directly in the agent's environment, violating the operator's
core security principle: credentials should never be visible to the
gateway pod.

Kubernetes Workload Identity solves this at the platform level: the pod
gets short-lived, auto-rotating credentials via a projected
ServiceAccount token, with no static secrets. All three major cloud
platforms use the same mechanism — annotate a ServiceAccount, assign it
to the pod, and the cloud SDK picks up temporary credentials from the
projected token.

## Decisions

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Surface area | Single optional `spec.serviceAccountName` string field | Minimal change. The SA and its cloud IAM binding are the admin's responsibility — the operator just wires the reference into the pod spec. |
| 2 | Token mounting | Enable `automountServiceAccountToken` only when the field is set | The embedded deployment manifest defaults to `automountServiceAccountToken: false`. Enabling it only on explicit opt-in preserves the current security posture for all existing instances. |
| 3 | Scope | Gateway Deployment only, not the proxy | The proxy handles credential injection via MITM — it has no need for cloud storage access. Only the gateway pod (where the agent runs) needs the projected SA token. |
| 4 | SA existence validation | None — delegate to Kubernetes | If the SA doesn't exist, the pod stays Pending with a clear event message. This matches standard Kubernetes behavior and avoids adding a reconcile-time check that could race with SA creation. |
| 5 | Field placement | Near `spec.network` in `ClawSpec` | Both are pod-level infrastructure concerns. Placing it next to `network` groups related infrastructure fields rather than burying it after operational flags like `idle`. |

## Security Considerations

Mounting a SA token gives the agent access to whatever RBAC or cloud IAM
role the SA carries. This is a deliberate, documented trade-off:

| | Without `serviceAccountName` | With `serviceAccountName` |
|---|---|---|
| **Credential type** | Static long-lived keys | Short-lived auto-rotating tokens |
| **Rotation** | Manual | Automatic (platform-managed) |
| **Blast radius on compromise** | Permanent cloud access | ~1 hour token scoped to one IAM role |
| **Visibility to agent** | Agent sees raw keys | Agent sees only a projected token path |

The operator does not audit SA permissions — that is the admin's
responsibility. Documentation emphasizes creating narrowly-scoped SAs
with minimal permissions.

## Implementation Notes

- `api/v1alpha1/claw_types.go`: `ServiceAccountName` string field added
  to `ClawSpec`, placed after `Network`.
- `internal/controller/claw_deployment.go`:
  `configureClawDeploymentServiceAccount` sets `serviceAccountName` and
  `automountServiceAccountToken: true` on the gateway Deployment's pod
  template. No-op when the field is empty.
- `internal/controller/claw_resource_controller.go`: called after
  `configureGatewayNoProxy` in the Phase 3 deployment mutation sequence.
- No proxy changes, no new conditions, no migration needed.
