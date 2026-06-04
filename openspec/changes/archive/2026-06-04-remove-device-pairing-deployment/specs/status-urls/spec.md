## REMOVED Requirements

### Requirement: DevicePairingURL status field
**Reason**: The device-pairing deployment is being removed. The URL field has no meaning without the deployment.
**Migration**: Consumers reading `status.devicePairingURL` should stop relying on this field. It will no longer be populated.
