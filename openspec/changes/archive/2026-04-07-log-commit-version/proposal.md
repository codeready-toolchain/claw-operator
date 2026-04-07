## Why

During troubleshooting and debugging, it's critical to know which exact version of the operator is running. Currently, there's no easy way to correlate a running operator instance with the source commit that built it.

## What Changes

- Add version logging during operator startup that displays commit SHA and build time
- Inject version information via LDFLAGS in the Makefile's `docker-build` target
- Log version information in `cmd/main.go` before starting the manager

## Capabilities

### New Capabilities
- `version-logging`: Display commit SHA and build time during operator startup

### Modified Capabilities
<!-- No existing capabilities being modified -->

## Impact

- `cmd/main.go` — add version variables and log statement during startup
- `Makefile` — modify `docker-build` target to inject version info via LDFLAGS
- No breaking changes
- No API changes
- Improves operational visibility and debugging
