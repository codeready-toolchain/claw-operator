## Context

The operator currently has no built-in version identification mechanism. During troubleshooting, it's difficult to determine which exact commit is running in production. This design adds version logging to show commit SHA and build time during startup.

## Goals / Non-Goals

**Goals:**
- Display commit SHA (short format) and build time in startup logs
- Inject version info at build time via LDFLAGS (no runtime overhead)
- Keep implementation simple and maintainable

**Non-Goals:**
- Version API endpoints or metrics (can be added later if needed)
- Semantic versioning (git commit SHA is sufficient for operator deployment tracking)
- Runtime version detection (build-time injection only)

## Decisions

### Use LDFLAGS for version injection

**Decision:** Inject commit SHA and build time as string variables via `go build -ldflags` in the Makefile.

**Rationale:** 
- Standard Go practice for embedding build metadata
- Zero runtime cost (values compiled into binary)
- Already using `docker-build` target in Makefile, easy integration

**Alternatives considered:**
- Runtime git commands: rejected due to requiring git in production image
- VERSION file: rejected due to needing manual updates and sync overhead

### Variable placement in cmd/main.go

**Decision:** Define package-level variables `version` and `buildTime` in `cmd/main.go`.

**Rationale:**
- `main` package is the entry point, natural place for startup logging
- Package-level variables are easily set via LDFLAGS with `-X` flag
- No need for separate version package for this simple use case

### Log format

**Decision:** Log version info as structured log entry before manager starts, format: `"Starting OpenClaw Operator" version=<sha> buildTime=<timestamp>`.

**Rationale:**
- Uses existing logger from controller-runtime
- Parseable by log aggregation tools
- Visible in both local dev and production logs

### Commit SHA format

**Decision:** Use short commit SHA (7 characters) via `git rev-parse --short HEAD`.

**Rationale:**
- Sufficient uniqueness for operator builds (low collision risk in single repo)
- More readable in logs than full 40-char SHA
- Standard format used by GitHub UI and git tooling

### Build time format

**Decision:** Use RFC3339 timestamp via `date -u +"%Y-%m-%dT%H:%M:%SZ"`.

**Rationale:**
- Unambiguous (includes timezone, UTC)
- Human-readable
- Machine-parseable

## Risks / Trade-offs

**[Risk: LDFLAGS not set in local builds]** → Document in CLAUDE.md that local `make run` won't show version. Acceptable trade-off since version is most critical in container deployments.

**[Risk: Build time reflects build machine time, not git commit time]** → Accepted. Build time indicates when binary was created, which is useful for staleness detection.

**[Trade-off: No semantic versioning]** → Commit SHA is sufficient for debugging. If semantic versions are needed later, can add alongside commit info.
