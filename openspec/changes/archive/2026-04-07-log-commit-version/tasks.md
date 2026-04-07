## 1. Add version variables to cmd/main.go

- [x] 1.1 Define package-level `version` variable (string, default "dev")
- [x] 1.2 Define package-level `buildTime` variable (string, default "unknown")
- [x] 1.3 Add log statement in main() before manager start that outputs version and buildTime

## 2. Update Makefile to inject version info

- [x] 2.1 Modify docker-build target to capture commit SHA using `git rev-parse --short HEAD`
- [x] 2.2 Modify docker-build target to capture build time using `date -u +"%Y-%m-%dT%H:%M:%SZ"`
- [x] 2.3 Add LDFLAGS to go build command with -X flags for version and buildTime variables

## 3. Testing and verification

- [x] 3.1 Test local build with `make docker-build` and verify version info in image
- [x] 3.2 Verify log output shows commit SHA and build time when operator starts
- [x] 3.3 Verify graceful handling when LDFLAGS not set (shows default values)
