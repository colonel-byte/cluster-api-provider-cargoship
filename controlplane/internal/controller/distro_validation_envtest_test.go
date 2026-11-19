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

//go:build envtest

// The CEL rules on spec.distro, against the API server that enforces them.
//
// They are here rather than in a unit test because nothing in Go runs them: controller-gen copies
// the expressions into the CRD and the API server evaluates them, so a rule that is wrong is wrong
// only at `kubectl apply` time. One of these was - `!self.fromZarfState` on a boolean nobody set
// is an evaluation error rather than a false, which refused every custom resource in the suite.
package controller

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
	"github.com/colonel-byte/cluster-api-provider-cargoship/internal/envtest"
)

func TestDistroSourceValidation(t *testing.T) {
	const digestRef = "oci://registry.test/k3s@sha256:" +
		"0000000000000000000000000000000000000000000000000000000000000000"

	tests := []struct {
		name    string
		distro  controlplanev1.DistroSource
		wantErr string
	}{
		{
			name: "a reference on its own",
			distro: controlplanev1.DistroSource{
				OCIRef: digestRef,
			},
		},
		{
			name: "a registry, a name and a version",
			distro: controlplanev1.DistroSource{
				Registry:       "zarf-docker-registry.zarf.svc.cluster.local:5000",
				Name:           "k3s",
				PackageVersion: "1.36.1",
			},
		},
		{
			// Refused before CEL runs: spec.distro is required and omitzero, so an empty one is
			// an absent one and the rules are never evaluated. Kept because the message an
			// operator sees is this rather than the rule's.
			name:    "neither",
			distro:  controlplanev1.DistroSource{},
			wantErr: "spec.distro: Required value",
		},
		{
			name: "both",
			distro: controlplanev1.DistroSource{
				OCIRef:         digestRef,
				Registry:       "ghcr.io/example",
				Name:           "k3s",
				PackageVersion: "1.36.1",
			},
			wantErr: "set either ociRef, or both registry and name",
		},
		{
			name: "a registry with no name",
			distro: controlplanev1.DistroSource{
				Registry: "ghcr.io/example",
			},
			wantErr: "set either ociRef, or both registry and name",
		},
		{
			name: "a name with no version, which would otherwise take the newest published",
			distro: controlplanev1.DistroSource{
				Registry: "ghcr.io/example",
				Name:     "k3s",
			},
			wantErr: "needs both name and packageVersion",
		},
		{
			name: "a version against a reference, which resolves nothing",
			distro: controlplanev1.DistroSource{
				OCIRef:         digestRef,
				PackageVersion: "1.36.1",
			},
			wantErr: "needs both name and packageVersion",
		},
		{
			name: "zarf state, with the registry its credentials are keyed by",
			distro: controlplanev1.DistroSource{
				FromZarfState:  true,
				Registry:       "zarf-docker-registry.zarf.svc.cluster.local:5000",
				Name:           "k3s",
				PackageVersion: "1.36.1",
			},
		},
		{
			name: "zarf state against a reference, which names no registry to key them by",
			distro: controlplanev1.DistroSource{
				FromZarfState: true,
				OCIRef:        digestRef,
			},
			wantErr: "fromZarfState needs registry",
		},
		{
			name: "zarf state and a pull secret, which is two answers to one question",
			distro: controlplanev1.DistroSource{
				FromZarfState:  true,
				Registry:       "zarf-docker-registry.zarf.svc.cluster.local:5000",
				Name:           "k3s",
				PackageVersion: "1.36.1",
				PullSecretRef:  &controlplanev1.SecretReference{Name: "zarf-registry"},
			},
			wantErr: "pullSecretRef and caBundleSecretRef must be unset",
		},
		{
			// The field is a host with an optional path prefix. A scheme here builds a reference
			// like "oci://https://registry/k3s:1.36.1" and keys the pull credentials under a
			// host of "https:".
			name: "a registry carrying a scheme",
			distro: controlplanev1.DistroSource{
				Registry:       "https://ghcr.io/example",
				Name:           "k3s",
				PackageVersion: "1.36.1",
			},
			wantErr: "carries no scheme",
		},
		{
			name: "zarf state and a CA bundle, likewise",
			distro: controlplanev1.DistroSource{
				FromZarfState:     true,
				Registry:          "zarf-docker-registry.zarf.svc.cluster.local:5000",
				Name:              "k3s",
				PackageVersion:    "1.36.1",
				CABundleSecretRef: &controlplanev1.SecretReference{Name: "zarf-registry-ca"},
			},
			wantErr: "pullSecretRef and caBundleSecretRef must be unset",
		},
	}

	c := envtest.Client(t)
	ns := envtest.Namespace(t, c)

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := validControlPlane("fleet-"+string(rune('a'+i)), ns)
			cp.Spec.Distro = tt.distro

			err := c.Create(t.Context(), cp)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("creating the control plane: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("the api server accepted %+v, want it refused with %q", tt.distro, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("the api server refused with %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestSpecShapeValidation covers the schema guards on the fields this provider owns outright,
// which the API server enforces and nothing in Go does.
//
// Each of these is a value cargoship, or cluster-api, would otherwise take as far as a converge
// before refusing it -- a worker batch size is parsed by each phase as it sizes its batches, so a
// typo in one is found with earlier phases already applied to hosts.
func TestSpecShapeValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*controlplanev1.CargoshipControlPlaneSpec)
		wantErr string
	}{
		{
			name: "a worker batch size as a count",
			mutate: func(s *controlplanev1.CargoshipControlPlaneSpec) {
				s.Apply.WorkerConcurrent = "5"
			},
		},
		{
			name: "a worker batch size as a percentage",
			mutate: func(s *controlplanev1.CargoshipControlPlaneSpec) {
				s.Apply.WorkerConcurrent = "25%"
			},
		},
		{
			name: "the whole fleet at once",
			mutate: func(s *controlplanev1.CargoshipControlPlaneSpec) {
				s.Apply.WorkerConcurrent = "100%"
			},
		},
		{
			name: "a worker batch size that is neither",
			mutate: func(s *controlplanev1.CargoshipControlPlaneSpec) {
				s.Apply.WorkerConcurrent = "a quarter"
			},
			wantErr: "spec.apply.workerConcurrent",
		},
		{
			name: "a percentage over 100",
			mutate: func(s *controlplanev1.CargoshipControlPlaneSpec) {
				s.Apply.WorkerConcurrent = "150%"
			},
			wantErr: "spec.apply.workerConcurrent",
		},
		{
			// The build suffix a distro package carries is part of the version it installs, and
			// refusing it would refuse the only versions this provider converges to.
			name: "a version with a package build suffix",
			mutate: func(s *controlplanev1.CargoshipControlPlaneSpec) {
				s.Version = "v1.31.2+k3s1"
			},
		},
		{
			name: "a version with no leading v",
			mutate: func(s *controlplanev1.CargoshipControlPlaneSpec) {
				s.Version = "1.34.1"
			},
			wantErr: "spec.version",
		},
		{
			name: "a version with no patch",
			mutate: func(s *controlplanev1.CargoshipControlPlaneSpec) {
				s.Version = "v1.34"
			},
			wantErr: "spec.version",
		},
	}

	c := envtest.Client(t)
	ns := envtest.Namespace(t, c)

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := validControlPlane("shape-"+string(rune('a'+i)), ns)
			tt.mutate(&cp.Spec)

			err := c.Create(t.Context(), cp)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("creating the control plane: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("the api server accepted %+v, want it refused with %q", cp.Spec, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("the api server refused with %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// validControlPlane is a control plane the schema accepts, for a case to spoil one field of.
//
// One namespace and one object per case, named per index rather than a namespace per case, which
// would be a dozen namespaces for a dozen creates.
func validControlPlane(name, namespace string) *controlplanev1.CargoshipControlPlane {
	return &controlplanev1.CargoshipControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: controlplanev1.CargoshipControlPlaneSpec{
			Replicas: ptr.To(int32(1)),
			Version:  "v1.34.1",
			MachineTemplate: controlplanev1.CargoshipControlPlaneMachineTemplate{
				Spec: controlplanev1.CargoshipControlPlaneMachineTemplateSpec{
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: "infrastructure.cluster.x-k8s.io",
						Kind:     "StubMachineTemplate",
						Name:     "stub",
					},
				},
			},
			Distro: controlplanev1.DistroSource{
				OCIRef: "oci://registry.test/k3s@sha256:" +
					"0000000000000000000000000000000000000000000000000000000000000000",
			},
			SSH: controlplanev1.SSHConfig{
				User:                "rocky",
				PrivateKeySecretRef: controlplanev1.SecretReference{Name: "fleet-ssh"},
			},
		},
	}
}
