# Claw Instance Idling — Design Questions

**Status:** Resolved — all decisions made
**Related:** [Design document](idling-design.md)

Each question has options with trade-offs and a recommendation. Go through them one by one to form the design, then update the design document.

---

## Q1: What should the reconcile loop do when `spec.idle` is true?

When the operator sees that idling is requested, it needs to decide how much
work to perform. The full reconcile is non-trivial (credential resolution, proxy
config generation, Kustomize build, Route host resolution, server-side apply).
We need to decide whether any of that is necessary during idle.

### Option B: Short-circuit early — only manage deployment scale and status

Check `spec.idle` early in Reconcile. If true, ensure Deployments are at 0
replicas, update status, and return immediately without processing the rest.

- **Pro:** Fast and cheap reconcile when idled — no unnecessary API calls.
- **Pro:** Won't fail on unrelated issues (missing secrets, Route not ready)
  when nothing is running.
- **Con:** Spec changes made while idled aren't applied until unidle. The next
  unidle reconcile will pick them up (since generation changed or field changed).
- **Con:** Slightly more complex — two code paths in Reconcile.

**Decision:** Option B — The idle state should be low-cost. Spec changes while
idled are uncommon, and the full reconcile runs naturally on unidle.

_Considered and rejected: Option A (wasteful — runs full pipeline for no-pod state, can fail on unrelated issues), Option C (most complex, marginal benefit over B since errors surface on unidle anyway)_

---

## Q2: What should the spec field be named?

The AAP operator uses `idle_aap` (snake_case, product-specific). We need a
field name that is idiomatic for a Kubernetes CRD and clear in intent.

### Option A: `idle` (bool)

```go
Idle bool `json:"idle,omitempty"`
```

- **Pro:** Concise, clear intent. Matches the AAP pattern conceptually.
- **Pro:** Easy for external systems to understand and use.
- **Con:** Very generic — could theoretically conflict with future Kubernetes
  conventions (unlikely).

**Decision:** Option A (`idle`) — Concise, intuitive for external idling systems,
trivial interaction contract (set `true` to idle, `false` to unidle).

_Considered and rejected: Option B `suspended` (implies pause/resume semantics rather than stop/start), Option C `replicas` (over-engineered for single-replica design, invites misuse)_

---

## Q3: How should idle state be represented in status?

The status needs to communicate that the instance is idle (not broken, not
provisioning — intentionally stopped). Dashboards and external tools rely on
status to determine what to show users.

### Option C: Both — Idle condition + Ready adjusted

Add the `Idle` condition AND set Ready to a specific state.
```yaml
conditions:
  - type: Ready
    status: "False"
    reason: Idle
  - type: Idle
    status: "True"
    reason: IdledByRequest
```

- **Pro:** Tools watching either condition get useful information.
- **Pro:** Clear semantic separation.
- **Con:** Slightly more status management code.

**Decision:** Option C — Maximum compatibility. Tools watching Ready see it's not
running; tools that understand the Idle condition can distinguish "intentionally
stopped" from "broken."

_Considered and rejected: Option A (Idle condition only — tools watching Ready wouldn't know why it's missing/stale), Option B (Ready reason only — conflates intentional idle with failures for tools that don't inspect reason)_

---

## Q4: What should the Ready condition report when idled?

**Resolved by Q3.** The decision on Q3 (Option C) already specifies
`Ready=False, reason=Idle` alongside the dedicated `Idle` condition. No
additional decision needed.
