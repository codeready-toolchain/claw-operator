## ADDED Requirements

### Requirement: Version information variables
The operator SHALL define variables for commit SHA and build time that can be populated via LDFLAGS during build.

#### Scenario: Variables defined at package level
- **WHEN** the operator binary is built
- **THEN** commit SHA and build time variables exist and can be set via LDFLAGS

### Requirement: Version information logging
The operator SHALL log the commit SHA and build time during startup before starting the manager.

#### Scenario: Startup logging with version info
- **WHEN** the operator starts
- **THEN** a log message displays the commit SHA and build time

#### Scenario: Logging when version info not set
- **WHEN** the operator starts without LDFLAGS set
- **THEN** a log message indicates version information is not available or shows default values

### Requirement: LDFLAGS injection in build
The Makefile's docker-build target SHALL inject commit SHA and build time via LDFLAGS.

#### Scenario: Docker build with version info
- **WHEN** running make docker-build
- **THEN** the build command includes LDFLAGS for commit SHA and build time

#### Scenario: Commit SHA format
- **WHEN** injecting commit SHA
- **THEN** the value SHALL be the short commit SHA (7 characters) from git

#### Scenario: Build time format
- **WHEN** injecting build time
- **THEN** the value SHALL be in RFC3339 format or similar human-readable timestamp
