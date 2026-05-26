## 1. Update Deployment Manifest

- [x] 1.1 Add `startupProbe` to the device-pairing container with httpGet on `/healthz`, named port `http`, `periodSeconds: 2`, `timeoutSeconds: 2`, `failureThreshold: 3`
- [x] 1.2 Update `livenessProbe` to use named port `http`, set `initialDelaySeconds: 3`, `periodSeconds: 15`, `timeoutSeconds: 2`, `failureThreshold: 3`
- [x] 1.3 Update `readinessProbe` to use named port `http`, set `initialDelaySeconds: 2`, `periodSeconds: 10`, `timeoutSeconds: 2`, `failureThreshold: 2`

## 2. Verify

- [x] 2.1 Run `make build` to ensure the embedded manifest is valid
- [x] 2.2 Run `make test` to verify no existing tests break
