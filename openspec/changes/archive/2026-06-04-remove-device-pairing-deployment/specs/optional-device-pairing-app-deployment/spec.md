## REMOVED Requirements

### Requirement: Device pairing resources are skipped when disabled
**Reason**: The device-pairing deployment is being removed entirely. There is no longer a concept of enabled/disabled device pairing deployment.
**Migration**: Remove `spec.auth.disableDevicePairing` from Claw CRs. The operator will automatically clean up any existing device-pairing resources on the next reconcile.

### Requirement: Device pairing Route injection is skipped when disabled
**Reason**: The device-pairing Route no longer exists.
**Migration**: No action needed.

### Requirement: Device pairing resources are recreated when re-enabled
**Reason**: The device-pairing deployment is being removed entirely. Toggle behavior no longer applies.
**Migration**: No action needed.

### Requirement: Previously deployed device-pairing resources are cleaned up
**Reason**: Replaced by unconditional cleanup on every reconcile (temporary, for upgrade path).
**Migration**: No action needed — the operator handles cleanup automatically.

### Requirement: Idle scaling skips device-pairing when disabled
**Reason**: The device-pairing deployment no longer exists, so idle scaling no longer needs to consider it.
**Migration**: No action needed.

### Requirement: E2E test covers disabled device pairing
**Reason**: No device-pairing deployment to test.
**Migration**: Remove device-pairing E2E test cases.
