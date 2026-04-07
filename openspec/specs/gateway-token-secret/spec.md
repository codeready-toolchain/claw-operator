## ADDED Requirements

### Requirement: Secret creation for gateway token
The OpenClawResourceReconciler SHALL create a Kubernetes Secret named `openclaw-secrets` when reconciling an OpenClaw instance named `instance`.

#### Scenario: New OpenClaw instance without existing secret
- **WHEN** an OpenClaw instance named `instance` is reconciled and no `openclaw-secrets` secret exists
- **THEN** the reconciler creates a new Secret with name `openclaw-secrets` in the same namespace

#### Scenario: Existing secret is preserved
- **WHEN** an OpenClaw instance is reconciled and the `openclaw-secrets` secret already exists with a token
- **THEN** the reconciler does not modify or regenerate the existing token value

### Requirement: Token generation
The `OPENCLAW_GATEWAY_TOKEN` data entry SHALL contain a cryptographically secure random token generated using Go's `crypto/rand` package.

#### Scenario: Token format and length
- **WHEN** a new token is generated
- **THEN** the token SHALL be exactly 64 hexadecimal characters (representing 32 random bytes)

#### Scenario: Token uniqueness
- **WHEN** multiple OpenClaw instances are created in different namespaces
- **THEN** each SHALL receive a unique randomly-generated token

### Requirement: Secret data structure
The Secret SHALL contain a single data entry with key `OPENCLAW_GATEWAY_TOKEN` and value as the generated token.

#### Scenario: Secret data entry exists
- **WHEN** the `openclaw-secrets` secret is created
- **THEN** it SHALL contain exactly one data entry named `OPENCLAW_GATEWAY_TOKEN`

#### Scenario: Token is base64-encoded
- **WHEN** the secret is stored in Kubernetes
- **THEN** the `OPENCLAW_GATEWAY_TOKEN` value SHALL be base64-encoded per Kubernetes Secret standard

### Requirement: Owner reference for lifecycle management
The Secret SHALL have an owner reference to the OpenClaw instance for automatic garbage collection.

#### Scenario: Owner reference is set
- **WHEN** the `openclaw-secrets` secret is created
- **THEN** it SHALL have a controller owner reference pointing to the OpenClaw instance

#### Scenario: Secret cleanup on instance deletion
- **WHEN** the OpenClaw instance is deleted
- **THEN** Kubernetes SHALL automatically delete the `openclaw-secrets` secret via garbage collection
