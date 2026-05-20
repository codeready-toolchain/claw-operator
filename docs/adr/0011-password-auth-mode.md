# ADR-0011: Password Authentication Mode

**Status:** Implemented
**Date:** 2026-05-20

## Overview

Add an alternative gateway authentication mode to the Claw CRD. The default token-based authentication generates a per-instance cryptographic token and requires device pairing — each browser must complete a one-time approval before interacting with the instance. This provides strong per-device identity but adds friction when multiple users need quick access to the same instance (workshops, shared team environments, demos).

Password mode offers a simpler alternative: users authenticate by entering a shared password in the browser. The operator reads the password from a Kubernetes Secret and injects it into the gateway config.

The `spec.auth` field provides three controls:
- `mode` — selects between `token` (default) and `password` authentication
- `passwordSecretRef` — references the Secret holding the shared password (required for password mode)
- `disableDevicePairing` — independently controls browser device identity checks (defaults based on mode)

## Decisions

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| 1 | Where should the password live? | Kubernetes Secret via `passwordSecretRef` | Consistent with every other secret in the operator. Keeps passwords out of the CR spec. Secret watch triggers re-reconcile on rotation. |
| 2 | Should `passwordSecretRef` reuse `SecretRefEntry`? | Yes | Same struct used by credentials, web search, MCP. No new types needed. |
| 3 | How to validate the conditional requirement? | CEL `XValidation` rule on `AuthSpec` | Rejects invalid CRs at admission. Same pattern as `CredentialSpec`, `McpServerSpec`, `WebSearchSpec`. No webhook infrastructure needed. |
| 4 | Should device pairing be automatically disabled? | Default yes for password, but configurable via `disableDevicePairing` | Upstream OpenClaw treats auth mode and device pairing as orthogonal concerns. Defaulting to disabled in password mode covers the common case, but an explicit `disableDevicePairing` field lets users override in either direction. |
| 5 | Should `status.url` include the token fragment? | No — omit `#token=` in password mode | The token fragment auto-authenticates the browser, bypassing the password prompt. In password mode the user enters the password in the UI. |
| 6 | Should this be a new CRD field or a config override? | New `spec.auth` field | Auth mode is infrastructure-level. It affects gateway config, status URL format, and device pairing behavior. Too cross-cutting for a config patch. |

## Architecture

### Reconciliation Flow

```
Claw CR spec.auth
       │
       ▼
 resolveAuthPassword()          ◄── read password from Secret
       │                             return "" for nil/token mode
       │                             error → Ready=False + ValidationFailed
       │
       ▼
 enrichConfigAndNetworkPolicy()
       ├─► injectAuthModeIntoConfigMap()
       │     ├── password injection (mode == password):
       │     │     gateway.auth.mode = "password"
       │     │     gateway.auth.password = <secret value>
       │     ├── device pairing (shouldDisableDevicePairing()):
       │     │     explicit disableDevicePairing overrides mode default
       │     │     gateway.controlUi.dangerouslyDisableDeviceAuth = true
       │     └── no-op when both are unnecessary
       │
       ▼
 updateStatus()
       └── password mode: status.url = routeURL (no #token= fragment)
           token mode:    status.url = routeURL + #token=<gateway-token>
```

### Secret Watch

`clawReferencesSecret` includes `spec.auth.passwordSecretRef` so that password Secret updates trigger re-reconcile — the new password is injected into the ConfigMap and the gateway pod rolls out.

## CRD Schema

### AuthSpec Fields

```yaml
spec:
  auth:
    mode: password              # "token" (default) or "password"
    passwordSecretRef:          # required when mode is "password"
      name: my-password
      key: password
    disableDevicePairing: true  # optional; defaults to true for password, false for token
```

### CEL Validation

On `AuthSpec`:
- `passwordSecretRef` is required when `mode` is `password`

### Reconciler Validation

- Secret referenced by `passwordSecretRef` must exist and contain the specified key with a non-empty value
- Failures set `Ready=False` with reason `ValidationFailed`

## ConfigMap Injection

`injectAuthModeIntoConfigMap` handles two independent concerns:

1. **Password injection** (when `mode` is `password`): replaces the `gateway.auth` block:
   ```json
   { "mode": "password", "password": "<secret value>" }
   ```

2. **Device pairing** (when `shouldDisableDevicePairing()` returns true): sets `gateway.controlUi.dangerouslyDisableDeviceAuth` to `true`, merged alongside existing `controlUi` settings.

The `shouldDisableDevicePairing` helper resolves the effective value: if `disableDevicePairing` is explicitly set, that value is used; otherwise it defaults to `true` for password mode and `false` for token mode.

When neither concern applies (auth is nil, mode is token, no explicit override), the function is a no-op.

## Examples

### Password mode

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  auth:
    mode: password
    passwordSecretRef:
      name: my-password
      key: password
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        - name: gemini-api-key
          key: api-key
      provider: google
```

Device pairing is disabled by default in password mode. Users authenticate by entering the password in the browser.

### Password mode with device pairing enabled

```yaml
spec:
  auth:
    mode: password
    passwordSecretRef:
      name: team-password
      key: password
    disableDevicePairing: false
```

Users enter the shared password and also go through device pairing. Useful when you want simplified credential sharing but still need per-device identity tracking.

### Token mode (default)

```yaml
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        - name: gemini-api-key
          key: api-key
      provider: google
```

No `auth` field needed — token mode with device pairing is the default.

## Future Considerations

- Per-user password mode (multiple passwords mapped to user identities)
- OIDC/SSO integration as a third auth mode
- Password complexity validation or minimum length enforcement
