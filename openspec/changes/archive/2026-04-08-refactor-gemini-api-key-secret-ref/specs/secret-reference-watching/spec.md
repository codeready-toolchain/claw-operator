## ADDED Requirements

### Requirement: Controller watches referenced Secrets
The controller MUST watch Secrets in the same namespace as OpenClaw CRs to detect when a referenced Secret is created, updated, or deleted.

#### Scenario: Referenced Secret is created after OpenClaw CR
- **WHEN** an OpenClaw CR references a Secret that does not exist, and then that Secret is created
- **THEN** the controller MUST reconcile the OpenClaw CR and propagate the API key to the proxy Secret

#### Scenario: Referenced Secret is updated
- **WHEN** a Secret referenced by an OpenClaw CR is updated
- **THEN** the controller MUST reconcile the OpenClaw CR and update the proxy Secret with the new API key value

#### Scenario: Referenced Secret is deleted
- **WHEN** a Secret referenced by an OpenClaw CR is deleted
- **THEN** the controller MUST reconcile the OpenClaw CR and set the Available condition to False with Reason=SecretNotFound

#### Scenario: Unrelated Secret is updated
- **WHEN** a Secret that is not referenced by any OpenClaw CR is updated
- **THEN** the controller MUST NOT reconcile any OpenClaw CRs

### Requirement: Controller uses watch predicates for efficient reconciliation
The controller MUST use watch predicates to filter Secret watch events and only reconcile OpenClaw CRs when a Secret they reference changes.

#### Scenario: Secret is referenced by multiple OpenClaw CRs
- **WHEN** a Secret referenced by multiple OpenClaw CRs is updated
- **THEN** the controller MUST reconcile all OpenClaw CRs that reference that Secret

#### Scenario: Secret watch triggers reconciliation with correct namespace scoping
- **WHEN** a Secret is updated in namespace A
- **THEN** the controller MUST only reconcile OpenClaw CRs in namespace A that reference that Secret, not CRs in other namespaces

### Requirement: Controller handles concurrent Secret updates gracefully
The controller MUST handle concurrent updates to referenced Secrets without data loss or race conditions.

#### Scenario: Secret is updated multiple times rapidly
- **WHEN** a referenced Secret is updated multiple times in quick succession
- **THEN** the controller MUST reconcile with the latest value and update the proxy Secret accordingly

#### Scenario: OpenClaw CR and referenced Secret are updated simultaneously
- **WHEN** an OpenClaw CR is updated to reference a different Secret at the same time the original Secret is updated
- **THEN** the controller MUST reconcile and use the API key from the newly referenced Secret

### Requirement: Controller provides clear status when Secret is missing
The controller MUST update the OpenClaw status conditions to clearly indicate when a referenced Secret cannot be found.

#### Scenario: Referenced Secret does not exist at reconciliation time
- **WHEN** the controller reconciles an OpenClaw CR and the referenced Secret does not exist
- **THEN** the controller MUST set the Available condition to False with Reason=SecretNotFound and a Message containing the Secret name and namespace

#### Scenario: Referenced Secret exists but key is missing
- **WHEN** the controller reconciles an OpenClaw CR and the referenced Secret exists but does not contain the specified key
- **THEN** the controller MUST set the Available condition to False with Reason=SecretKeyNotFound and a Message containing the Secret name, namespace, and missing key name

#### Scenario: Referenced Secret becomes available after being missing
- **WHEN** a referenced Secret is created after the controller set Available=False due to SecretNotFound
- **THEN** the controller MUST detect the Secret creation, propagate the API key, and update the Available condition to True with Reason=Ready
