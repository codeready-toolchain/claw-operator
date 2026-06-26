## MODIFIED Requirements

### Requirement: Claw has Go API types
The system SHALL define Go structs for `Claw`, `ClawSpec`, and `ClawStatus` with appropriate Kubernetes API machinery tags.

#### Scenario: API types are available for controller use
- **WHEN** controller code imports the API package
- **THEN** it SHALL have access to typed Claw structs

#### Scenario: Types support JSON serialization
- **WHEN** Claw is marshaled to JSON
- **THEN** it SHALL include standard Kubernetes fields (apiVersion, kind, metadata, spec, status)

#### Scenario: ClawSpec includes image field
- **WHEN** ClawSpec is defined
- **THEN** it SHALL include an `Image` field of type `string` with JSON tag `image`, defaulting to `ghcr.io/openclaw/openclaw:slim`

#### Scenario: ClawStatus includes image field
- **WHEN** ClawStatus is defined
- **THEN** it SHALL include an `Image` field of type `string` with JSON tag `image`
