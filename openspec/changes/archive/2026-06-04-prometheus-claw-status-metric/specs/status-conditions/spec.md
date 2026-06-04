## MODIFIED Requirements

### Requirement: Ready condition reasons are well-defined
The Ready condition SHALL use standardized reason values for common states. These reason values are also used to derive the `status` label for the `claw_instance_status` Prometheus metric.

#### Scenario: Provisioning reason when deployments not ready
- **WHEN** one or both Deployments are not yet available
- **THEN** the Ready condition SHALL have reason=Provisioning
- **THEN** the message SHALL indicate which deployments are pending
- **THEN** the `claw_instance_status` metric SHALL have `status="provisioning"` set to `1`

#### Scenario: Ready reason when fully available
- **WHEN** both Deployments report Available=True
- **THEN** the Ready condition SHALL have reason=Ready
- **THEN** the message SHALL confirm the instance is ready for use
- **THEN** the `claw_instance_status` metric SHALL have `status="ready"` set to `1`

#### Scenario: ValidationFailed reason maps to failed status
- **WHEN** credential validation fails and Ready condition has reason=ValidationFailed
- **THEN** the `claw_instance_status` metric SHALL have `status="failed"` set to `1`

#### Scenario: Unknown reason defaults to provisioning
- **WHEN** the Ready condition has a reason not in the known set (Ready, Provisioning, ValidationFailed)
- **THEN** the `claw_instance_status` metric SHALL default to `status="provisioning"` set to `1`
