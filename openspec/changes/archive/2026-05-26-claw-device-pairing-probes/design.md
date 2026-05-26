## Context

The claw-device-pairing Deployment already has liveness and readiness probes hitting `/healthz` on port 8080. However, they lack explicit `timeoutSeconds` and `failureThreshold` settings (defaulting to Kubernetes defaults of 1s timeout and 3 failures). The application is a tiny Go backend that starts nearly instantly, so probes should be tuned for fast failure detection.

## Goals / Non-Goals

**Goals:**
- Add explicit `timeoutSeconds` to liveness and readiness probes for clarity and fast failure detection
- Add explicit `failureThreshold` to both probes
- Add a `startupProbe` to cleanly separate startup detection from ongoing liveness, allowing tight liveness settings without risking restart loops during init
- Keep all timeout values small given the app's fast startup characteristics

**Non-Goals:**
- Changing the health endpoint path or port
- Adding new health endpoint logic to the application code
- Modifying probes for any other Deployment (claw gateway, proxy, etc.)

## Decisions

**Decision 1: Add a startupProbe**
A startupProbe lets Kubernetes distinguish "still starting" from "unhealthy after running." This allows the liveness probe to use aggressive settings (low timeout, low threshold) without risking premature restarts during startup. Since the app starts quickly, the startupProbe will also be tight (e.g., `failureThreshold: 3`, `periodSeconds: 2`).

Alternative considered: relying solely on `initialDelaySeconds` on liveness — but this is a static guess and less robust than a dedicated startup probe.

**Decision 2: Use small, explicit timeout values**
- `timeoutSeconds: 2` on all probes — generous enough for a health endpoint on a tiny backend, tight enough for fast detection
- `failureThreshold: 3` on liveness and startup (Kubernetes default, made explicit)
- `failureThreshold: 2` on readiness — pull from service faster on health issues
- Keep existing `periodSeconds` values (15s liveness, 10s readiness) and reduce `initialDelaySeconds` since the startupProbe handles startup gating

**Decision 3: Use named port reference**
Reference the named port `http` instead of the literal `8080` in probe definitions for consistency and maintainability.

## Risks / Trade-offs

- [Risk: Overly aggressive probes cause unnecessary restarts] → Mitigated by using startupProbe to handle init, and keeping `failureThreshold` at 2-3 so transient slowness doesn't trigger restarts
- [Risk: Port name change breaks probes] → Low risk; the named port is defined in the same manifest
