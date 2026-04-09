## Context

The OpenClaw CRD currently defines a single printcolumn showing the Age of the resource. With status conditions now implemented, operators need quick visibility into instance readiness when running `kubectl get openclaw`. Printcolumns control which fields appear in the table output.

Kubebuilder generates CRD printcolumns from `+kubebuilder:printcolumn` markers on the Go type definition.

## Goals / Non-Goals

**Goals:**
- Display Available condition status in kubectl table output
- Display reason for current state (Provisioning vs Ready)
- Remove Age column (not operationally useful for this resource)
- Maintain consistency with Kubernetes conventions for status reporting

**Non-Goals:**
- Adding additional custom columns beyond Status and Reason
- Changing status condition implementation (already exists)
- Modifying kubectl output format beyond printcolumns

## Decisions

### Decision 1: Use JSONPath to extract condition fields
Use JSONPath expressions in kubebuilder markers to extract `status` and `reason` from the Available condition.

**Rationale:** Kubernetes supports JSONPath in printcolumn definitions. The status conditions array is searchable by type using the `?(@.type=="Available")` filter.

**Alternatives considered:**
- Adding dedicated status fields: Would duplicate information already in conditions
- Custom printer columns via separate resource: Overly complex for this use case

### Decision 2: Two columns - Status and Reason
Display both the condition status (True/False/Unknown) and the reason (Provisioning/Ready).

**Rationale:** Status alone doesn't explain why. Reason provides operational context without needing to inspect the full resource YAML.

**Alternatives considered:**
- Single "Ready" column showing yes/no: Less informative during troubleshooting
- Three columns including message: Too verbose for table output

### Decision 3: Remove Age column
Remove the existing Age printcolumn.

**Rationale:** Age is less useful than readiness for operational visibility. Users can still see creationTimestamp in YAML output or add Age via custom kubectl formatting if needed.

**Alternatives considered:**
- Keep Age and add Status/Reason: Three columns may be too wide for typical terminal output

## Risks / Trade-offs

**[Risk: JSONPath returns empty string if condition doesn't exist yet]**
Mitigation: Controller sets Available condition immediately during first reconciliation, so the field will always be populated after initial reconcile. Empty cells during creation are acceptable.

**[Trade-off: Losing Age visibility in default output]**
Rationale: Users who need Age can view it via `kubectl get openclaw -o wide` or `kubectl describe`. Readiness is more valuable for at-a-glance operational status.

**[Risk: JSONPath syntax errors in CRD generation]**
Mitigation: JSONPath is validated during `make manifests`. Test with `kubectl get openclaw` after deployment to verify columns render correctly.
