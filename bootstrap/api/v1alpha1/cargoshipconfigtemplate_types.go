// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// CargoshipConfigTemplateSpec is the template a MachineDeployment stamps CargoshipConfigs from.
type CargoshipConfigTemplateSpec struct {
	// template is the CargoshipConfig to create per machine.
	// +required
	Template CargoshipConfigTemplateResource `json:"template,omitempty,omitzero"`
}

// CargoshipConfigTemplateResource is one templated CargoshipConfig.
type CargoshipConfigTemplateResource struct {
	// metadata is the labels and annotations stamped onto the created CargoshipConfig.
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec is the CargoshipConfig spec to stamp.
	// +optional
	Spec CargoshipConfigSpec `json:"spec,omitempty,omitzero"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=cargoshipconfigtemplates,scope=Namespaced,categories=cluster-api,shortName=csct
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time since creation"

// CargoshipConfigTemplate is a template for CargoshipConfig, referenced by a MachineDeployment so
// its worker machines each get their own config.
//
// It has no status and no controller: a template is never reconciled, only stamped by the
// MachineSet controller. That is the whole of the ConfigTemplate half of the bootstrap contract.
type CargoshipConfigTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is the desired state of CargoshipConfigTemplate.
	// +optional
	Spec CargoshipConfigTemplateSpec `json:"spec,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// CargoshipConfigTemplateList is a list of CargoshipConfigTemplate.
type CargoshipConfigTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CargoshipConfigTemplate `json:"items"`
}

func init() {
	objectTypes = append(objectTypes, &CargoshipConfigTemplate{}, &CargoshipConfigTemplateList{})
}
