## 1. Update CRD Printcolumn Markers

- [x] 1.1 Remove existing Age printcolumn marker from api/v1alpha1/openclaw_types.go
- [x] 1.2 Add Status printcolumn marker using JSONPath to extract Available condition status
- [x] 1.3 Add Reason printcolumn marker using JSONPath to extract Available condition reason
- [x] 1.4 Verify JSONPath syntax follows Kubernetes printcolumn conventions

## 2. Regenerate CRD Manifests

- [x] 2.1 Run `make manifests` to regenerate CRD YAML with updated printcolumns
- [x] 2.2 Verify generated CRD includes Status and Reason printcolumns in config/crd/bases/
- [x] 2.3 Verify Age printcolumn is removed from generated CRD

## 3. Update Documentation

- [x] 3.1 Update CLAUDE.md to mention new printcolumns if relevant
- [x] 3.2 Update README.md to show example kubectl get output with new columns

## 4. Testing and Verification

- [x] 4.1 Run `make test` to ensure no regressions
- [x] 4.2 Run `make lint` to verify code quality
- [x] 4.3 Install updated CRD to test cluster with `make install`
- [x] 4.4 Create test OpenClaw instance and verify `kubectl get openclaw` shows Status and Reason columns
- [x] 4.5 Verify Status column shows True/False based on deployment readiness
- [x] 4.6 Verify Reason column shows Provisioning/Ready appropriately
