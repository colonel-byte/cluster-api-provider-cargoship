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

// Package v1alpha1 holds the bootstrap provider's API.
//
// The group is bootstrap.cluster.x-k8s.io, not a colonel-byte.dev group. Kinds within it are
// distinct, so there is nothing to collide with, and every clusterctl and Rancher Turtles example
// -- along with the contract documentation itself -- is written in terms of this group. A provider
// that invents its own is correct and unrecognisable.
//
// Nothing here imports controller-runtime, which is deliberate and is why the scheme is built from
// apimachinery's own SchemeBuilder rather than controller-runtime's `pkg/scheme` helper. An API
// package is imported by anything that wants to read these types -- a test, another provider, a
// CLI -- and each of those would otherwise pay for a controller runtime it never starts. This is
// the same arrangement sigs.k8s.io/cluster-api/api uses, and the reason that is a module of its
// own.
// +kubebuilder:object:generate=true
// +groupName=bootstrap.cluster.x-k8s.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "bootstrap.cluster.x-k8s.io", Version: "v1alpha1"}

	// schemeBuilder is used to add go types to the GroupVersionKind scheme.
	schemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = schemeBuilder.AddToScheme

	// objectTypes is appended to by each types file's init, so a new kind registers itself
	// beside its own declaration rather than in a list here that is easy to forget.
	objectTypes = []runtime.Object{}
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, objectTypes...)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
