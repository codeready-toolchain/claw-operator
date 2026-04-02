## Context

The OpenClawInstance controller currently reconciles all resources of type OpenClawInstance regardless of their name. The requirement specifies that only resources named "instance" should be managed. This is a simple filtering requirement that needs to be enforced early in the reconciliation loop.

## Goals / Non-Goals

**Goals:**
- Filter reconciliation to only process OpenClawInstance resources named "instance"
- Skip processing early to avoid unnecessary work for non-matching resources
- Maintain existing reconciliation behavior for the resource named "instance"

**Non-Goals:**
- Making the filter name configurable (hardcoded to "instance")
- Supporting multiple allowed names or patterns
- Changing the watch configuration (still watch all resources, filter at reconcile time)
- Deleting or rejecting non-matching resources

## Decisions

### Decision 1: Filter at reconcile time, not watch time

**Choice:** Add name check in the Reconcile function after fetching the resource, rather than modifying the watch predicates.

**Rationale:**
- Simpler implementation - single line check
- Easier to test and understand
- Watch predicates are more complex and error-prone
- Minimal performance impact (name check is trivial)
- Controller still sees all events for monitoring/debugging

**Alternative considered:** Use watch predicates to filter before reconciliation
- Rejected: More complex, harder to test, less transparent about what's being filtered

### Decision 2: Skip after successful Get, before any processing

**Choice:** Check the name immediately after successfully fetching the OpenClawInstance, before parsing manifests or creating resources.

**Rationale:**
- Fail fast - avoid unnecessary work
- Clear placement in code flow
- Easy to add logging for visibility
- Resource exists check still happens (useful for debugging)

### Decision 3: Return success (not error) for non-matching names

**Choice:** Return `ctrl.Result{}, nil` (success) when name doesn't match, similar to NotFound handling.

**Rationale:**
- Not an error condition - expected filtering behavior
- Prevents reconciliation queue thrashing
- Consistent with how NotFound is handled
- Logs at info level for visibility without noise

## Risks / Trade-offs

**[Trade-off]** Resources named differently than "instance" will be ignored even if users expect them to work  
→ **Accepted:** This is the intended behavior per requirements; can be documented

**[Risk]** If name check is removed/modified accidentally, all resources will be reconciled again  
→ **Mitigation:** Test coverage verifies filtering behavior; spec documents the requirement

**[Trade-off]** Watch still triggers for all resources (small overhead)  
→ **Accepted:** Minimal performance impact; filtering at reconcile time is simpler and sufficient
