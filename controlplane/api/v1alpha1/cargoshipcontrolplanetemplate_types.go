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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// CargoshipControlPlaneTemplateSpec is the template a ClusterClass stamps CargoshipControlPlanes
// from.
type CargoshipControlPlaneTemplateSpec struct {
	// template is the CargoshipControlPlane to create per cluster.
	// +required
	Template CargoshipControlPlaneTemplateResource `json:"template,omitempty,omitzero"`
}

// CargoshipControlPlaneTemplateResource is one templated CargoshipControlPlane.
type CargoshipControlPlaneTemplateResource struct {
	// metadata is the labels and annotations stamped onto the created CargoshipControlPlane.
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec is the CargoshipControlPlane spec to stamp.
	// +optional
	Spec CargoshipControlPlaneTemplateResourceSpec `json:"spec,omitempty,omitzero"`
}

// CargoshipControlPlaneTemplateResourceSpec is CargoshipControlPlaneSpec without the fields the
// topology controller fills in.
//
// Deliberately not CargoshipControlPlaneSpec itself, which is the obvious move and is wrong:
// `version` and `replicas` come from the Cluster's own `spec.topology`, and
// `machineTemplate.spec.infrastructureRef` comes from the class's
// `controlPlane.machineInfrastructure.templateRef`. All three are required or defaulted on the
// live type, so wrapping it would make an operator write three placeholder values that the
// topology controller then overwrites, and refuse the resource when they did not.
//
// This is the shape Cluster API's own control-plane template takes, measured rather than assumed:
// `kubeadmcontrolplanetemplates`' v1beta2 schema holds `kubeadmConfigSpec`, `machineNaming`,
// `machineTemplate`, `remediation` and `rollout` with nothing required, while the live
// `kubeadmcontrolplanes` requires `machineTemplate` and `version` and requires
// `machineTemplate.spec.infrastructureRef` within it. See
// docs/agent/choice-clusterclass-template.md.
//
// The field names that are here have to stay identical to the live spec's, because nothing in
// this repository converts between them: the topology controller copies `spec.template.spec` onto
// the new object as it is. TestTemplateSpecCoversTheLiveSpec is what holds the two together.
type CargoshipControlPlaneTemplateResourceSpec struct {
	// machineTemplate is the metadata and placement each control-plane machine is created with.
	// +optional
	MachineTemplate CargoshipControlPlaneTemplateMachineTemplate `json:"machineTemplate,omitempty,omitzero"`

	// controlPlaneEndpoint is the host clients reach the API through.
	//
	// Worth setting through a class rather than here: a kube-vip or load-balancer address is per
	// cluster, and a topology variable with a patch onto this field is how one value reaches both
	// this and wherever the vip is configured. See docs/guides/clusterclass.md.
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint,omitempty,omitzero"`

	// distro is the cargoship package the fleet is converged to.
	// +required
	Distro DistroSource `json:"distro,omitempty,omitzero"`

	// ssh is how the controller reaches the machines.
	// +required
	SSH SSHConfig `json:"ssh,omitempty,omitzero"`

	// clusterConfig is the cargoship cluster configuration's spec.config, verbatim.
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	ClusterConfig *apiextensionsv1.JSON `json:"clusterConfig,omitempty"`

	// registryAuthSecretRef is a kubernetes.io/dockerconfigjson secret holding credentials for
	// the registries named in clusterConfig.registries.
	// +optional
	RegistryAuthSecretRef *SecretReference `json:"registryAuthSecretRef,omitempty"`

	// apply tunes the converge.
	// +optional
	Apply ApplyConfig `json:"apply,omitempty,omitzero"`
}

// CargoshipControlPlaneTemplateMachineTemplate is the machine template a class carries: what is
// left of CargoshipControlPlaneMachineTemplate once the infrastructure reference the class
// supplies itself is taken out.
type CargoshipControlPlaneTemplateMachineTemplate struct {
	// metadata is the labels and annotations stamped onto each machine.
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec is the machine template's own spec.
	// +optional
	Spec CargoshipControlPlaneTemplateMachineTemplateSpec `json:"spec,omitempty,omitzero"`
}

// CargoshipControlPlaneTemplateMachineTemplateSpec is CargoshipControlPlaneMachineTemplateSpec
// without infrastructureRef.
type CargoshipControlPlaneTemplateMachineTemplateSpec struct {
	// failureDomains are the failure domains to spread control-plane machines across, in order.
	// Empty lets the infrastructure provider choose.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	FailureDomains []string `json:"failureDomains,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=cargoshipcontrolplanetemplates,scope=Namespaced,categories=cluster-api,shortName=cscpt
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time since creation"

// CargoshipControlPlaneTemplate is a template for CargoshipControlPlane, referenced by a
// ClusterClass so each cluster built from that class gets its own control plane.
//
// It has no status and no controller: a template is never reconciled, only stamped by Cluster
// API's topology controller, and what it stamps is an ordinary CargoshipControlPlane that the
// existing reconciler handles with no change at all.
//
// A ClusterClass names it through spec.controlPlane.templateRef, which carries a kind, a name and
// an apiVersion and no namespace -- so a template lives in the same namespace as the class that
// refers to it.
type CargoshipControlPlaneTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is the desired state of CargoshipControlPlaneTemplate.
	// +optional
	Spec CargoshipControlPlaneTemplateSpec `json:"spec,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// CargoshipControlPlaneTemplateList is a list of CargoshipControlPlaneTemplate.
type CargoshipControlPlaneTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CargoshipControlPlaneTemplate `json:"items"`
}

func init() {
	objectTypes = append(objectTypes, &CargoshipControlPlaneTemplate{}, &CargoshipControlPlaneTemplateList{})
}
