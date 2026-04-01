## Why

The OpenClawInstance CRD currently uses the API group `openclaw.codeready-toolchain.com` but should align with Red Hat's sandbox domain naming convention `openclaw.sandbox.redhat.com` to reflect its purpose within the Developer Sandbox ecosystem.

## What Changes

- Update the OpenClawInstance CRD API group from `openclaw.codeready-toolchain.com` to `openclaw.sandbox.redhat.com`
- Update all references to the old API group in code, manifests, and documentation
- Regenerate CRD manifests with the new API group
- Update sample resources to use the new API group
- **BREAKING**: Existing OpenClawInstance resources with the old API group will not be recognized by the operator after this change

## Capabilities

### New Capabilities
<!-- No new capabilities are being introduced -->

### Modified Capabilities
- `openclawinstance-crd`: Change the API group from `openclaw.codeready-toolchain.com` to `openclaw.sandbox.redhat.com` while maintaining v1alpha1 version

## Impact

- **API types**: `api/v1alpha1/groupversion_info.go` package declaration and group name
- **CRD manifests**: Generated CRD files in `config/crd/bases/`
- **Sample resources**: `config/samples/` YAML files
- **Documentation**: README.md API group references
- **Controller**: RBAC annotations referencing the API group
- **Existing deployments**: Any existing OpenClawInstance resources will need to be recreated with the new API group after upgrade
