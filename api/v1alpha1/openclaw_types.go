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

// OpenClawSpec defines the desired state of OpenClaw
type OpenClawSpec struct {
	// APIKey is the API key for authenticating with the LLM provider
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	APIKey string `json:"apiKey"`
}

// OpenClawStatus defines the observed state of OpenClaw
type OpenClawStatus struct {
	// Conditions represent the latest available observations of the OpenClaw's state
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// URL is the HTTPS URL for accessing the OpenClaw instance
	// +optional
	URL string `json:"url,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=openclaws,scope=Namespaced
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Available\")].status"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type==\"Available\")].reason"

// OpenClaw is the Schema for the openclaws API
type OpenClaw struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OpenClawSpec   `json:"spec,omitempty"`
	Status OpenClawStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OpenClawList contains a list of OpenClaw
type OpenClawList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OpenClaw `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OpenClaw{}, &OpenClawList{})
}
