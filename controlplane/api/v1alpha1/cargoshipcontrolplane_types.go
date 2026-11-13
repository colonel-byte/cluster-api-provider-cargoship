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

const (
	// ControlPlaneFinalizer is set on a CargoshipControlPlane so its machines are deleted with it.
	ControlPlaneFinalizer = "cargoshipcontrolplane.controlplane.cluster.x-k8s.io"

	// PreTerminateHookName is the suffix of the pre-terminate annotation the controller puts on
	// every machine it manages.
	//
	// It is what makes a machine's removal safe without a `cargoship reset`: Cluster API drains
	// the node and deletes the Node object, then blocks the infrastructure provider from
	// destroying the instance until this annotation is gone. The controller uses that window to
	// converge the surviving fleet -- the departing host is simply absent from the inventory, so
	// /etc/hosts and the firewall rules are rewritten from the hosts that remain -- and only then
	// removes the annotation. Without it the survivors keep a dead peer in both for as long as it
	// takes something else to trigger a reconcile.
	PreTerminateHookName = "cargoship"

	// AvailableCondition reports whether the control plane is answering.
	//
	// The name is fixed by the contract: ControlPlaneContract.AvailableConditionType is what the
	// Cluster controller reads to decide whether the cluster is available.
	AvailableCondition = clusterv1.AvailableCondition

	// ConvergedCondition reports the outcome of the last cargoship apply.
	ConvergedCondition = "Converged"

	// MachinesReadyCondition reports whether every managed machine has an address and has joined.
	MachinesReadyCondition = "MachinesReady"
)

// CargoshipControlPlaneSpec is the desired state of a cargoship-installed control plane.
type CargoshipControlPlaneSpec struct {
	// replicas is the number of control-plane machines.
	//
	// An even number is accepted rather than refused, because cargoship does not own the etcd
	// topology decision and an operator growing a cluster passes through even numbers. The
	// controller warns.
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`

	// version is the desired Kubernetes version, as the contract requires it: a semantic version
	// with a leading "v".
	//
	// It must agree with what the distro package installs. The controller reads the package's own
	// version and refuses a mismatch rather than installing something other than what was asked
	// for -- the package is the authority on what lands on a host, and a CR that disagreed with
	// it would make status.version a lie.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Version string `json:"version,omitempty"`

	// machineTemplate is the infrastructure and metadata each control-plane machine is created
	// from.
	// +required
	MachineTemplate CargoshipControlPlaneMachineTemplate `json:"machineTemplate,omitempty,omitzero"`

	// controlPlaneEndpoint is the host clients reach the API through, and becomes the cluster
	// configuration's loadbalancer.
	//
	// It is a contract field, so Cluster API copies it onto the Cluster. Leave it unset and the
	// controller fills it in from the first control-plane machine's address, which is right for a
	// single-controller cluster and wrong for any cluster that will grow one.
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint,omitempty,omitzero"`

	// distro is the cargoship package the fleet is converged to.
	// +required
	Distro DistroSource `json:"distro,omitempty,omitzero"`

	// ssh is how the controller reaches the machines.
	// +required
	SSH SSHConfig `json:"ssh,omitempty,omitzero"`

	// clusterConfig is the cargoship cluster configuration's spec.config, verbatim.
	//
	// It is carried as an opaque object rather than mirrored field by field, which is a deliberate
	// trade. What it buys: the YAML here is the YAML of a cargoship inventory, so it needs no
	// translation and cannot drift from the schema cargoship actually validates against -- and
	// cargoship does validate it, against the package's own values schema, before any phase runs.
	// What it costs: the API server will not reject a typo in it, so a mistake surfaces as a
	// reconcile error on the custom resource rather than as a rejected apply.
	//
	// loadbalancer is ignored here; controlPlaneEndpoint above is the contract field for it.
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	ClusterConfig *apiextensionsv1.JSON `json:"clusterConfig,omitempty"`

	// apply tunes the converge.
	// +optional
	Apply ApplyConfig `json:"apply,omitempty,omitzero"`
}

// CargoshipControlPlaneMachineTemplate is the control-plane machines' template.
type CargoshipControlPlaneMachineTemplate struct {
	// metadata is the labels and annotations stamped onto each machine.
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec is the machine template's own spec.
	// +required
	Spec CargoshipControlPlaneMachineTemplateSpec `json:"spec,omitempty,omitzero"`
}

// CargoshipControlPlaneMachineTemplateSpec holds the infrastructure reference.
type CargoshipControlPlaneMachineTemplateSpec struct {
	// infrastructureRef is the infrastructure machine template each control-plane machine is
	// created from.
	// +required
	InfrastructureRef clusterv1.ContractVersionedObjectReference `json:"infrastructureRef,omitempty,omitzero"`

	// failureDomains are the failure domains to spread control-plane machines across, in order.
	// Empty lets the infrastructure provider choose.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	FailureDomains []string `json:"failureDomains,omitempty"`
}

// DistroSource names the cargoship package to converge the fleet to.
type DistroSource struct {
	// ociRef is the package OCI reference, e.g. "oci://ghcr.io/example/k3s@sha256:...".
	//
	// A tag is resolved to a digest once and the digest is recorded in status.distro.digest.
	// Every later reconcile uses the digest, so a tag that moves underneath a running cluster
	// does not silently change what the fleet converges to; change the reference to adopt it.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	OCIRef string `json:"ociRef,omitempty"`

	// arch is the package architecture to pull, "amd64" or "arm64". Empty takes the package's
	// sole architecture, and fails if it carries more than one.
	// +optional
	// +kubebuilder:validation:Enum=amd64;arm64
	Arch string `json:"arch,omitempty"`

	// pullSecretRef is a docker-registry secret for the registry holding the package. Empty uses
	// anonymous access.
	//
	// The secret must be of type kubernetes.io/dockerconfigjson, which is what
	// `kubectl create secret docker-registry` produces. Its registries are merged into a Docker
	// config file the controller keeps on its cache volume, because that file is the only place
	// the pull reads credentials from; see docs/agent/choice-registry-credentials.md.
	// +optional
	PullSecretRef *LocalObjectReference `json:"pullSecretRef,omitempty"`

	// insecureSkipTLSVerify accepts the registry's certificate without verifying it. It covers
	// the package pull only, and neither the SSH connections to the fleet nor the workload
	// cluster's API server.
	//
	// It is here because a self-signed registry cannot otherwise be pulled from per resource: the
	// pull takes no CA bundle, so trusting one properly means mounting it into the controller and
	// pointing SSL_CERT_FILE at it, which is a property of the deployment rather than of one
	// cluster. Prefer that where the same registry serves every cluster. See
	// docs/guides/registry.md.
	// +optional
	InsecureSkipTLSVerify bool `json:"insecureSkipTLSVerify,omitempty"`
}

// SSHConfig is how the controller connects to the machines.
type SSHConfig struct {
	// user is the login to connect as.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	User string `json:"user,omitempty"`

	// port is the SSH port. Zero means 22.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`

	// privateKeySecretRef is a secret holding the private key, in the "ssh-privatekey" key of a
	// kubernetes.io/ssh-auth secret.
	//
	// The key is read into memory and handed to the SSH client as an auth method. It is never
	// written to the pod's filesystem, so the controller needs no writable mount for it and a
	// container compromise has to read process memory rather than a file.
	// +required
	PrivateKeySecretRef LocalObjectReference `json:"privateKeySecretRef,omitempty,omitzero"`

	// useSudo runs privileged commands through sudo rather than as the login user. Needed for any
	// user that is not root, which should be every user.
	// +optional
	// +kubebuilder:default=true
	UseSudo *bool `json:"useSudo,omitempty"`
}

// ApplyConfig tunes the cargoship apply the reconcile runs.
type ApplyConfig struct {
	// modifyHosts lets cargoship write every node into each host's /etc/hosts.
	// +optional
	ModifyHosts bool `json:"modifyHosts,omitempty"`

	// modifyFirewall lets cargoship open the ports the cluster needs in the host's own firewall.
	// +optional
	ModifyFirewall bool `json:"modifyFirewall,omitempty"`

	// labelNodes adds the node-role.kubernetes.io/<profile> label to each node.
	// +optional
	LabelNodes bool `json:"labelNodes,omitempty"`

	// concurrency caps how many hosts a phase acts on at once. Zero is unlimited.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Concurrency int32 `json:"concurrency,omitempty"`

	// workerConcurrent is the worker batch size, a count ("5") or a percentage ("25%").
	// +optional
	// +kubebuilder:validation:MaxLength=8
	WorkerConcurrent string `json:"workerConcurrent,omitempty"`

	// allowDowngrade lets a converge continue against a host already running a version newer than
	// the package, rather than refusing.
	// +optional
	AllowDowngrade bool `json:"allowDowngrade,omitempty"`

	// timeoutSeconds bounds the retry loops inside each phase. Zero takes cargoship's own
	// default, which is 15 minutes.
	//
	// This is not a budget for the whole converge: it is how long one phase waits for one host to
	// get where it is going. Nothing in this controller bounds the total, on purpose -- a machine
	// that takes eleven minutes to boot is not an error, and the reconcile that is waiting for it
	// costs nothing.
	// +optional
	// +kubebuilder:validation:Minimum=0
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`

	// driftIntervalSeconds is how often a converged fleet is re-converged to catch drift. Zero
	// takes 600.
	// +optional
	// +kubebuilder:validation:Minimum=0
	DriftIntervalSeconds int32 `json:"driftIntervalSeconds,omitempty"`
}

// LocalObjectReference names an object in the same namespace.
type LocalObjectReference struct {
	// name of the referenced object.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// CargoshipControlPlaneStatus is the observed state of a CargoshipControlPlane.
//
// The replica counts and the initialization block are contract fields, spelled the way the v1beta2
// contract spells them: upToDateReplicas and availableReplicas, not the v1beta1 updatedReplicas
// and unavailableReplicas, and initialization.controlPlaneInitialized rather than initialized.
type CargoshipControlPlaneStatus struct {
	// conditions represent the observations of the control plane's current state.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=32
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// initialization provides observations of the initialization process.
	// +optional
	Initialization CargoshipControlPlaneInitializationStatus `json:"initialization,omitempty,omitzero"`

	// selector is the label selector in string form, for the scale subresource.
	// +optional
	// +kubebuilder:validation:MaxLength=4096
	Selector string `json:"selector,omitempty"`

	// replicas is the number of control-plane machines that exist.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// readyReplicas is the number of control-plane machines reporting ready.
	// +optional
	ReadyReplicas *int32 `json:"readyReplicas,omitempty"`

	// availableReplicas is the number of control-plane machines that are available.
	// +optional
	AvailableReplicas *int32 `json:"availableReplicas,omitempty"`

	// upToDateReplicas is the number of control-plane machines converged to the current desired
	// state.
	// +optional
	UpToDateReplicas *int32 `json:"upToDateReplicas,omitempty"`

	// version is the lowest Kubernetes version observed across the control-plane machines.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Version string `json:"version,omitempty"`

	// distro records what was actually converged to, as opposed to what was asked for.
	// +optional
	Distro DistroStatus `json:"distro,omitempty,omitzero"`

	// lastAppliedHash is the desired-state fingerprint of the last successful converge: the host
	// set, the cluster configuration and the pinned package digest.
	//
	// It is what keeps a reconcile from re-running a multi-minute apply on every resync. A
	// converge is idempotent, so re-running one is safe rather than destructive -- this is a cost
	// control, not a correctness gate, and clearing it forces a converge.
	// +optional
	// +kubebuilder:validation:MaxLength=64
	LastAppliedHash string `json:"lastAppliedHash,omitempty"`

	// hosts is what the last converge did, per host.
	// +optional
	// +listType=map
	// +listMapKey=address
	// +kubebuilder:validation:MaxItems=256
	Hosts []HostStatus `json:"hosts,omitempty"`

	// observedGeneration is the latest generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// CargoshipControlPlaneInitializationStatus is the contract's initialization block.
type CargoshipControlPlaneInitializationStatus struct {
	// controlPlaneInitialized is true once the Kubernetes API has accepted a request, which is
	// also when the kubeconfig secret exists.
	//
	// This field is part of the Cluster API contract and is what releases worker machines to be
	// created. The v1beta1 spelling was status.initialized.
	// +optional
	ControlPlaneInitialized *bool `json:"controlPlaneInitialized,omitempty"`
}

// DistroStatus records the package the fleet was converged to.
type DistroStatus struct {
	// ref is the package reference the fleet was converged to, exactly as spec.distro.ociRef
	// gave it.
	// +optional
	// +kubebuilder:validation:MaxLength=2048
	Ref string `json:"ref,omitempty"`

	// digest is the package digest, set only when the reference named one.
	//
	// It is empty for a tagged reference, which is also the case in which this provider cannot
	// tell that a republished package has changed. Pin the reference to a digest where that
	// matters; see docs/agent/choice-distro-pinning.md.
	// +optional
	// +kubebuilder:validation:MaxLength=256
	Digest string `json:"digest,omitempty"`

	// engine is the distro engine the package carries: k3s, rke2, or upstream.
	// +optional
	// +kubebuilder:validation:MaxLength=32
	Engine string `json:"engine,omitempty"`

	// version is the Kubernetes version the package installs.
	// +optional
	// +kubebuilder:validation:MaxLength=256
	Version string `json:"version,omitempty"`
}

// HostStatus is one host's line in the last converge.
type HostStatus struct {
	// address is the host as the inventory addressed it, which is the identity cargoship's own
	// run receipt uses.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Address string `json:"address,omitempty"`

	// machine is the Machine this host came from.
	// +optional
	// +kubebuilder:validation:MaxLength=253
	Machine string `json:"machine,omitempty"`

	// role is controller or worker.
	// +optional
	// +kubebuilder:validation:Enum=controller;worker
	Role string `json:"role,omitempty"`

	// changed is whether the last converge changed anything on this host.
	// +optional
	Changed bool `json:"changed,omitempty"`

	// phasesNamed is how many phases spoke for this host, and phasesReporting how many declared
	// per-host results at all.
	//
	// They are a pair because the first cannot be read without the second: a host named by two of
	// two reporting phases is fully accounted for, one named by two of fifteen is not, and the
	// two look identical without the denominator.
	// +optional
	PhasesNamed int32 `json:"phasesNamed,omitempty"`

	// phasesReporting is the denominator for phasesNamed.
	// +optional
	PhasesReporting int32 `json:"phasesReporting,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=cargoshipcontrolplanes,scope=Namespaced,categories=cluster-api,shortName=cscp
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Initialized",type="string",JSONPath=".status.initialization.controlPlaneInitialized",description="Control plane is answering"
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".spec.replicas",description="Desired control-plane machines"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".status.replicas",description="Control-plane machines that exist"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="Control-plane machines reporting ready"
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".status.version",description="Observed Kubernetes version"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time since creation"

// CargoshipControlPlane is a Kubernetes control plane installed and converged by cargoship over
// SSH, in place of kubeadm.
//
// It owns the whole fleet rather than only the control-plane machines, which is a deliberate
// departure from KubeadmControlPlane. Cargoship converges controllers and workers in one run, with
// its own per-profile concurrency, and splitting that across two controllers would mean two
// uncoordinated SSH walks over hosts that share an /etc/hosts file and a firewall. See
// docs/agent/choice-one-converge.md.
type CargoshipControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is the desired state of CargoshipControlPlane.
	// +optional
	Spec CargoshipControlPlaneSpec `json:"spec,omitempty,omitzero"`

	// status is the observed state of CargoshipControlPlane.
	// +optional
	Status CargoshipControlPlaneStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// CargoshipControlPlaneList is a list of CargoshipControlPlane.
type CargoshipControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CargoshipControlPlane `json:"items"`
}

func init() {
	objectTypes = append(objectTypes, &CargoshipControlPlane{}, &CargoshipControlPlaneList{})
}
