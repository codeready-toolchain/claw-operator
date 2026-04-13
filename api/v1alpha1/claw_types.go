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

// SecretRef references a specific key in a Secret
type SecretRef struct {
	// Name is the name of the Secret
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key is the key in the Secret's data map
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// ClawSpec defines the desired state of Claw
type ClawSpec struct {
	// GeminiAPIKey is a reference to a Secret containing the Gemini API key
	// +kubebuilder:validation:Required
	GeminiAPIKey *SecretRef `json:"geminiAPIKey"`
}

// ClawStatus defines the observed state of Claw
type ClawStatus struct {
	// GatewayTokenSecretRef is the name of the Secret containing the gateway authentication token
	// +optional
	GatewayTokenSecretRef string `json:"gatewayTokenSecretRef,omitempty"`

	// Conditions represent the latest available observations of the Claw's state
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// URL is the HTTPS URL for accessing the Claw instance
	// +optional
	URL string `json:"url,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=claws,scope=Namespaced
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].reason"

// Claw is the Schema for the claws API
type Claw struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClawSpec   `json:"spec,omitempty"`
	Status ClawStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClawList contains a list of Claw
type ClawList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Claw `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Claw{}, &ClawList{})
}
