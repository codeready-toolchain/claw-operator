## ADDED Requirements

### Requirement: Gateway token secret name uses instance name
The gateway token secret SHALL be named using the pattern `{claw-name}-gateway-token` where `{claw-name}` is the Claw instance name.

#### Scenario: Secret name for instance 'my-openclaw'
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** the gateway token secret is named 'my-openclaw-gateway-token'

#### Scenario: Secret name for instance 'production'
- **WHEN** reconciling a Claw instance named 'production'
- **THEN** the gateway token secret is named 'production-gateway-token'

#### Scenario: Multiple instances have separate secrets
- **WHEN** two Claw instances 'claw-a' and 'claw-b' exist in the same namespace
- **THEN** secret 'claw-a-gateway-token' is created for instance 'claw-a'
- **THEN** secret 'claw-b-gateway-token' is created for instance 'claw-b'
- **THEN** secrets do not conflict or share tokens between instances

### Requirement: Status field references dynamic secret name
The Claw status field `gatewayTokenSecretRef` SHALL contain the dynamically generated secret name.

#### Scenario: Status references correct secret name
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** status field `gatewayTokenSecretRef` contains 'my-openclaw-gateway-token'
- **THEN** users can find the secret by reading the status field
