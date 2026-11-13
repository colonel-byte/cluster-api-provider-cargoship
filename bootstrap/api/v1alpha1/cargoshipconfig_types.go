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
)

const (
	// ConfigFinalizer is set on a CargoshipConfig so its bootstrap secret is deleted with it.
	ConfigFinalizer = "cargoshipconfig.bootstrap.cluster.x-k8s.io"

	// DataSecretAvailableCondition reports whether the bootstrap secret has been written.
	DataSecretAvailableCondition = "DataSecretAvailable"
)

// CargoshipConfigSpec describes how to make a machine reachable.
//
// It does not describe how to install Kubernetes, which is the one thing worth saying about it. A
// cargoship cluster is converged over SSH from the management cluster, so nothing on this machine
// installs anything; the bootstrap data exists because an infrastructure provider will not create
// a machine until Machine.spec.bootstrap.dataSecretName is set. What it carries is an authorized
// key and whatever else has to be true before the first SSH connection can succeed.
type CargoshipConfigSpec struct {
	// user is the login cargoship connects as. The user must already exist in the image; this
	// configures its authorized keys rather than creating it, because an image that does not
	// carry the account is a packaging problem a bootstrap script should not paper over.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	User string `json:"user,omitempty"`

	// sshAuthorizedKeys are public keys appended to the user's authorized_keys.
	//
	// The control plane's own key is added by the controller from its sshSecretRef, so this is
	// for the extra ones: an operator's key for breaking glass, a backup agent's. A config with
	// none is valid and normal.
	// +optional
	// +kubebuilder:validation:MaxItems=32
	// +listType=atomic
	SSHAuthorizedKeys []string `json:"sshAuthorizedKeys,omitempty"`

	// files are written to the machine before cargoship connects.
	//
	// Keep this empty if you can. It exists for what must be true before SSH works at all -- a
	// proxy configuration, a CA bundle the machine needs to resolve anything, a network script
	// the image does not carry. Everything after the first connection is a cargoship phase, and
	// a file placed here is a file no phase reports on and no receipt records.
	// +optional
	// +kubebuilder:validation:MaxItems=32
	// +listType=atomic
	Files []File `json:"files,omitempty"`

	// preCargoshipCommands run on first boot, before cargoship connects. The same warning as
	// files applies, and more strongly: a command here runs once, unreported, with no retry.
	// +optional
	// +kubebuilder:validation:MaxItems=32
	// +listType=atomic
	PreCargoshipCommands []string `json:"preCargoshipCommands,omitempty"`
}

// File is a file written by the bootstrap data.
type File struct {
	// path is the absolute path to write to.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	Path string `json:"path,omitempty"`

	// owner is the "user:group" the file is written as.
	// +optional
	// +kubebuilder:validation:MaxLength=65
	Owner string `json:"owner,omitempty"`

	// permissions is the file mode, as a string, because a leading zero in YAML is how "0600"
	// becomes 384.
	// +optional
	// +kubebuilder:validation:MaxLength=4
	Permissions string `json:"permissions,omitempty"`

	// content is the file's contents, written literally.
	//
	// A secret does not belong here: bootstrap data reaches the machine through the
	// infrastructure provider's user-data channel, which on most providers is readable from the
	// instance metadata service by anything running on the host. Use contentFrom.
	// +optional
	// +kubebuilder:validation:MaxLength=131072
	Content string `json:"content,omitempty"`

	// contentFrom reads the contents from a secret key in the CargoshipConfig's namespace.
	//
	// This is no more private than content once the bootstrap secret is rendered -- the value is
	// copied into it either way. What it buys is that the value is not in the CargoshipConfig
	// itself, so it is not in the Git repository the CR is applied from.
	// +optional
	ContentFrom *FileSource `json:"contentFrom,omitempty"`
}

// FileSource names a secret key holding a file's contents.
type FileSource struct {
	// secret is the secret and key to read.
	// +required
	Secret SecretKeySelector `json:"secret,omitempty,omitzero"`
}

// SecretKeySelector selects one key of one secret in the same namespace.
type SecretKeySelector struct {
	// name of the secret.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`

	// key within the secret.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Key string `json:"key,omitempty"`
}

// CargoshipConfigStatus is the observed state of a CargoshipConfig.
type CargoshipConfigStatus struct {
	// conditions represent the observations of a CargoshipConfig's current state.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=32
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// initialization provides observations of the initialization process.
	// +optional
	Initialization CargoshipConfigInitializationStatus `json:"initialization,omitempty,omitzero"`

	// dataSecretName is the name of the secret that stores the bootstrap data.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	DataSecretName string `json:"dataSecretName,omitempty"`

	// failureReason is set when the config can never become ready, as opposed to not being ready
	// yet. The Machine controller treats it as terminal.
	// +optional
	// +kubebuilder:validation:MaxLength=256
	FailureReason string `json:"failureReason,omitempty"`

	// failureMessage is the human-readable half of failureReason.
	// +optional
	// +kubebuilder:validation:MaxLength=10240
	FailureMessage string `json:"failureMessage,omitempty"`

	// observedGeneration is the latest generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// CargoshipConfigInitializationStatus is the contract's initialization block.
type CargoshipConfigInitializationStatus struct {
	// dataSecretCreated is true when the Machine's bootstrap secret is created.
	//
	// This field is part of the Cluster API contract: an infrastructure provider waits on it
	// before creating a machine. The v1beta1 spelling was status.ready.
	// +optional
	DataSecretCreated *bool `json:"dataSecretCreated,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=cargoshipconfigs,scope=Namespaced,categories=cluster-api,shortName=csc
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.initialization.dataSecretCreated",description="Bootstrap secret created"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time since creation"

// CargoshipConfig is the bootstrap configuration for one machine in a cargoship-managed cluster.
type CargoshipConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is the desired state of CargoshipConfig.
	// +optional
	Spec CargoshipConfigSpec `json:"spec,omitempty,omitzero"`

	// status is the observed state of CargoshipConfig.
	// +optional
	Status CargoshipConfigStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// CargoshipConfigList is a list of CargoshipConfig.
type CargoshipConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CargoshipConfig `json:"items"`
}

func init() {
	objectTypes = append(objectTypes, &CargoshipConfig{}, &CargoshipConfigList{})
}
