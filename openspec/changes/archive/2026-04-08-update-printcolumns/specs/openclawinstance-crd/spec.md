## ADDED Requirements

### Requirement: CRD defines printcolumns for status visibility
The OpenClaw CRD SHALL define printcolumns to display the Available condition status and reason in kubectl table output.

#### Scenario: Status column shows Available condition status
- **WHEN** user runs `kubectl get openclaw`
- **THEN** the output SHALL include a "Status" column showing the status value from the Available condition (True, False, or Unknown)

#### Scenario: Reason column shows Available condition reason
- **WHEN** user runs `kubectl get openclaw`
- **THEN** the output SHALL include a "Reason" column showing the reason value from the Available condition (e.g., Provisioning, Ready)

#### Scenario: Age column is not displayed
- **WHEN** user runs `kubectl get openclaw`
- **THEN** the Age column SHALL NOT appear in the default table output

#### Scenario: Printcolumns use JSONPath to extract condition fields
- **WHEN** examining the CRD manifest
- **THEN** printcolumn definitions SHALL use JSONPath expressions to extract status and reason from the Available condition (e.g., `.status.conditions[?(@.type=="Available")].status`)

#### Scenario: Empty status cells when condition not yet set
- **WHEN** an OpenClaw instance is newly created and the controller has not yet set status conditions
- **THEN** the Status and Reason columns MAY be empty until the first reconciliation completes
