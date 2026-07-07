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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClawOperatorConfigSingletonName is the required name of the single
// ClawOperatorConfig instance the operator reads. Any other name is ignored.
const ClawOperatorConfigSingletonName = "cluster"

// ClawOperatorConfigSpec defines cluster-admin policy for the claw-operator.
type ClawOperatorConfigSpec struct {
	// AllowedConfigModes restricts which spec.config.mergeMode values Claw CRs
	// in this cluster may use. Empty/absent means all modes are allowed
	// (fail-open) — this matches today's unrestricted behavior until an admin
	// explicitly opts in to restricting it.
	// +optional
	AllowedConfigModes []ConfigMode `json:"allowedConfigModes,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=clawoperatorconfigs,scope=Namespaced

// ClawOperatorConfig is the Schema for the clawoperatorconfigs API. A single
// instance named "cluster" must live in the operator's own runtime namespace
// (resolved via the WATCH_NAMESPACE environment variable) — it is never
// looked up in, or honored from, a tenant namespace.
type ClawOperatorConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec ClawOperatorConfigSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// ClawOperatorConfigList contains a list of ClawOperatorConfig
type ClawOperatorConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClawOperatorConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClawOperatorConfig{}, &ClawOperatorConfigList{})
}
