## ADDED Requirements

### Requirement: Controller SHALL execute device approval command in matched pod
When exactly one pod matches the selector, the controller SHALL set `Ready=False, Reason=Processing` on the CR, then execute `openclaw devices approve <requestID> --json` in the `gateway` container of the matched pod using pod exec.

#### Scenario: Successful device pairing
- **WHEN** exactly one pod matches the selector and the exec command exits with code 0
- **THEN** the controller SHALL set the CR status condition to `Ready=True, Reason=DevicePaired` with a message containing the pod name, and SHALL log the JSON stdout from the command

#### Scenario: Failed device pairing (non-zero exit)
- **WHEN** exactly one pod matches the selector and the exec command exits with a non-zero exit code
- **THEN** the controller SHALL set the CR status condition to `Ready=False, Reason=PairingFailed` with a message containing the error details, and SHALL NOT requeue

#### Scenario: Failed device pairing (exec error)
- **WHEN** exactly one pod matches the selector but the exec call itself fails (e.g., pod not running, container not found)
- **THEN** the controller SHALL set the CR status condition to `Ready=False, Reason=PairingFailed` with a message containing the error, and SHALL NOT requeue

### Requirement: Controller SHALL set Processing condition before exec
Before executing the command in the pod, the controller SHALL persist a `Ready=False, Reason=Processing` status condition on the CR to indicate the pairing is in progress.

#### Scenario: Processing condition visible during exec
- **WHEN** the controller finds a matching pod and is about to execute the approval command
- **THEN** the controller SHALL update the CR status to `Ready=False, Reason=Processing` with a message indicating the target pod name BEFORE invoking pod exec

### Requirement: Controller SHALL skip already-paired requests
The controller SHALL NOT re-execute the approval command if the CR already has `Ready=True, Reason=DevicePaired`.

#### Scenario: Re-reconcile of completed pairing request
- **WHEN** a reconcile is triggered for a CR that already has `Ready=True, Reason=DevicePaired`
- **THEN** the controller SHALL return immediately without executing the command or modifying the status

### Requirement: Controller SHALL have pods/exec RBAC permission
The controller MUST have RBAC permission to create `pods/exec` subresources in order to execute commands inside pods.

#### Scenario: RBAC markers present
- **WHEN** the controller source is inspected
- **THEN** a kubebuilder RBAC marker for `pods/exec` with `create` verb SHALL be present

### Requirement: Controller SHALL use a timeout for exec operations
The controller SHALL use a context with a 30-second timeout when executing the pod exec command to prevent indefinite blocking.

#### Scenario: Exec command exceeds timeout
- **WHEN** the exec command takes longer than 30 seconds
- **THEN** the context SHALL be cancelled and the controller SHALL set `Ready=False, Reason=PairingFailed` with a timeout-related error message
