## Why

The current CRD printcolumns only show the Age column, which provides limited value for operators managing OpenClaw instances. With the newly added status conditions, we should surface the instance readiness directly in `kubectl get` output for better operational visibility.

## What Changes

- Replace the Age printcolumn with a Status column showing the Available condition status
- Add a Reason column displaying why the instance is in its current state
- Update kubebuilder markers in `api/v1alpha1/openclaw_types.go`
- Regenerate CRD manifests to reflect the new printcolumns

## Capabilities

### New Capabilities

### Modified Capabilities
- `openclawinstance-crd`: Update printcolumn definitions to display status condition readiness instead of Age

## Impact

- `api/v1alpha1/openclaw_types.go`: Update `+kubebuilder:printcolumn` markers
- `config/crd/bases/openclaw.sandbox.redhat.com_openclaws.yaml`: Auto-regenerated CRD with new printcolumns
- User experience: `kubectl get openclaw` will now show Status and Reason columns instead of Age
