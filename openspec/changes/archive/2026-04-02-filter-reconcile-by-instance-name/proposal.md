## Why

The controller currently reconciles all OpenClawInstance resources regardless of their name. However, the requirement is to only manage instances specifically named "instance", allowing other named resources to exist without triggering reconciliation.

## What Changes

- Controller reconciliation logic will check the resource name before proceeding
- Resources not named "instance" will be skipped early in the reconcile loop
- Early return for non-matching resources to avoid unnecessary processing

## Capabilities

### New Capabilities

None - this change modifies existing controller filtering behavior.

### Modified Capabilities

- `openclawinstance-controller`: Controller will add name filtering to only reconcile OpenClawInstance resources named "instance"

## Impact

**Code:**
- `internal/controller/openclawinstance_controller.go` - Add name check in Reconcile function
- Controller tests - Update test cases to verify filtering behavior

**Behavior:**
- Only OpenClawInstance resources named "instance" will be reconciled
- Other named OpenClawInstance resources will be ignored (no Deployment created)
- Reduces unnecessary reconciliation overhead for resources outside scope
