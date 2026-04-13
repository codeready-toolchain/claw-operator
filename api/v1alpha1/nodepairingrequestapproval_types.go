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

// NodePairingRequestApprovalSpec defines the desired state of NodePairingRequestApproval
type NodePairingRequestApprovalSpec struct {
	// RequestID is the unique identifier for this pairing request
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	RequestID string `json:"requestID"`
}

// NodePairingRequestApprovalStatus defines the observed state of NodePairingRequestApproval
type NodePairingRequestApprovalStatus struct {
	// Conditions represent the latest available observations of the NodePairingRequestApproval's state
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=nodepairingrequestapprovals,scope=Namespaced
// +kubebuilder:printcolumn:name="RequestID",type="string",JSONPath=".spec.requestID"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NodePairingRequestApproval is the Schema for the nodepairingrequestapprovals API
type NodePairingRequestApproval struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec   NodePairingRequestApprovalSpec   `json:"spec"`
	Status NodePairingRequestApprovalStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NodePairingRequestApprovalList contains a list of NodePairingRequestApproval
type NodePairingRequestApprovalList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodePairingRequestApproval `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodePairingRequestApproval{}, &NodePairingRequestApprovalList{})
}
