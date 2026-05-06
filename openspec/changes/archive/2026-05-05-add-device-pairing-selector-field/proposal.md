## Why

The ClawDevicePairingRequest CRD currently only contains a `requestID` field, which uniquely identifies a pairing request but provides no mechanism for the controller to locate the specific Claw instance pod that should process the device pairing approval. Without a way to target the correct pod, the controller cannot implement the device pairing workflow.

## What Changes

- Add a `selector` field to ClawDevicePairingRequestSpec of type `metav1.LabelSelector`
- The selector field will enable the ClawDevicePairingRequestController to query pods running Claw instances using label matching
- Add CRD validation for the selector field (required, non-empty)
- Update the controller to use the selector field when looking up target pods

## Capabilities

### New Capabilities
- `device-pairing-selector`: Defines the selector field in ClawDevicePairingRequest.Spec and its usage by the controller to locate target Claw pods

### Modified Capabilities
<!-- None - this is a new field addition to an existing CRD -->

## Impact

- **Affected code**:
  - `api/v1alpha1/clawdevicepairingrequest_types.go` — Add selector field to ClawDevicePairingRequestSpec
  - `internal/controller/clawdevicepairingrequest_controller.go` — Use selector to find target pods
  - CRD manifests will be regenerated via `make manifests`
- **API changes**: **BREAKING** — Adds a required field to ClawDevicePairingRequest.Spec. Existing CRs without a selector will fail validation when the CRD is updated.
- **Migration path**: Users must add a selector field to existing ClawDevicePairingRequest resources before upgrading
- **No changes to external dependencies**
