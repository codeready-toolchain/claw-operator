/*
Copyright 2026 Red Hat.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// effectiveConfigMode returns the mergeMode that will actually be used for
// this instance, defaulting to ConfigModeMerge when unset.
func effectiveConfigMode(instance *clawv1alpha1.Claw) clawv1alpha1.ConfigMode {
	if instance.Spec.Config == nil || instance.Spec.Config.MergeMode == "" {
		return clawv1alpha1.ConfigModeMerge
	}
	return instance.Spec.Config.MergeMode
}

// checkConfigModeAllowed enforces cluster-admin policy set via the
// ClawOperatorConfig singleton (named ClawOperatorConfigSingletonName, in the
// operator's own runtime namespace — see docs/adr/0021-seed-only-config-mode.md).
//
// This fails open by design: if the singleton doesn't exist, or exists with
// an empty AllowedConfigModes, every mode is allowed. This preserves today's
// unrestricted behavior (no gating mechanism exists yet) until a cluster
// admin explicitly opts in to restricting it. Only a genuine API error (not
// "not found") is surfaced as an error — a missing or unrestricted policy is
// not a failure.
func (r *ClawResourceReconciler) checkConfigModeAllowed(ctx context.Context, instance *clawv1alpha1.Claw) (bool, error) {
	// OperatorNamespace is unset in most unit tests (which don't exercise this
	// gating) and would otherwise make the lookup below ambiguous/invalid; not
	// knowing the operator's own namespace is equivalent to "no policy is
	// configured" for gating purposes.
	if r.OperatorNamespace == "" {
		return true, nil
	}

	mode := effectiveConfigMode(instance)

	opConfig := &clawv1alpha1.ClawOperatorConfig{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: r.OperatorNamespace,
		Name:      clawv1alpha1.ClawOperatorConfigSingletonName,
	}, opConfig)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get ClawOperatorConfig %q: %w", clawv1alpha1.ClawOperatorConfigSingletonName, err)
	}

	if len(opConfig.Spec.AllowedConfigModes) == 0 {
		return true, nil
	}
	for _, allowedMode := range opConfig.Spec.AllowedConfigModes {
		if allowedMode == mode {
			return true, nil
		}
	}
	return false, nil
}
