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

// The composition half of ClusterClass support, against the API server that has to accept it.
//
// There is no controller to test: a template is stamped by Cluster API's topology controller,
// which does not run here. What is worth pinning down is that the two objects validate together -
// a ClusterClass's templateRef carries pattern constraints on the kind and the apiVersion, and a
// template whose own spec required the fields the topology controller supplies would be refused
// the moment somebody wrote one.
package controller

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
	"github.com/colonel-byte/cluster-api-provider-cargoship/internal/envtest"
)

func TestClusterClassReferencesTheControlPlaneTemplate(t *testing.T) {
	c := envtest.Client(t)
	ns := envtest.Namespace(t, c)
	ctx := t.Context()

	// No version, no replicas, no machineTemplate.spec.infrastructureRef: all three come from
	// the class and the topology, and this is the assertion that the template does not demand
	// them.
	template := &controlplanev1.CargoshipControlPlaneTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "fleet", Namespace: ns},
		Spec: controlplanev1.CargoshipControlPlaneTemplateSpec{
			Template: controlplanev1.CargoshipControlPlaneTemplateResource{
				Spec: controlplanev1.CargoshipControlPlaneTemplateResourceSpec{
					Distro: controlplanev1.DistroSource{
						Registry:       "zarf-docker-registry.zarf.svc.cluster.local:5000",
						Name:           "k3s",
						PackageVersion: "1.34.1",
						FromZarfState:  true,
					},
					SSH: controlplanev1.SSHConfig{
						User:                "rocky",
						PrivateKeySecretRef: controlplanev1.LocalObjectReference{Name: "fleet-ssh"},
					},
					Apply: controlplanev1.ApplyConfig{
						ModifyHosts:    true,
						ModifyFirewall: true,
					},
				},
			},
		},
	}
	if err := c.Create(ctx, template); err != nil {
		t.Fatalf("creating the control-plane template: %v", err)
	}

	class := &clusterv1.ClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "cargoship", Namespace: ns},
		Spec: clusterv1.ClusterClassSpec{
			Infrastructure: clusterv1.InfrastructureClass{
				TemplateRef: clusterv1.ClusterClassTemplateReference{
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
					Kind:       "StubClusterTemplate",
					Name:       "fleet",
				},
			},
			ControlPlane: clusterv1.ControlPlaneClass{
				TemplateRef: clusterv1.ClusterClassTemplateReference{
					APIVersion: controlplanev1.GroupVersion.String(),
					Kind:       "CargoshipControlPlaneTemplate",
					Name:       template.Name,
				},
				// Supported only for a machine-based control plane that takes replicas, which
				// this one is: it is where the stamped machineTemplate.spec.infrastructureRef
				// comes from.
				MachineInfrastructure: clusterv1.ControlPlaneClassMachineInfrastructureTemplate{
					TemplateRef: clusterv1.ClusterClassTemplateReference{
						APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
						Kind:       "StubMachineTemplate",
						Name:       "fleet-cp",
					},
				},
			},
		},
	}
	if err := c.Create(ctx, class); err != nil {
		t.Fatalf("creating the ClusterClass: %v", err)
	}

	// Read back through the scheme, which is the part a missing objectTypes entry breaks.
	got := &controlplanev1.CargoshipControlPlaneTemplate{}
	key := types.NamespacedName{Namespace: ns, Name: template.Name}
	if err := c.Get(ctx, key, got); err != nil {
		t.Fatalf("reading the template back: %v", err)
	}
	if got.Spec.Template.Spec.Distro.Name != "k3s" {
		t.Errorf("template distro name = %q, want k3s", got.Spec.Template.Spec.Distro.Name)
	}
	if !got.Spec.Template.Spec.Distro.FromZarfState {
		t.Error("template lost spec.distro.fromZarfState, so a class could not carry it")
	}
}

// The CEL rules on spec.distro apply inside a template too, which is worth knowing because the
// message an operator sees names fields under spec.template.spec rather than spec.
func TestClusterClassTemplateValidatesTheDistroSource(t *testing.T) {
	c := envtest.Client(t)
	ns := envtest.Namespace(t, c)

	template := &controlplanev1.CargoshipControlPlaneTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "fleet", Namespace: ns},
		Spec: controlplanev1.CargoshipControlPlaneTemplateSpec{
			Template: controlplanev1.CargoshipControlPlaneTemplateResource{
				Spec: controlplanev1.CargoshipControlPlaneTemplateResourceSpec{
					// A name with no version, which the live type refuses too.
					Distro: controlplanev1.DistroSource{
						Registry: "ghcr.io/example",
						Name:     "k3s",
					},
					SSH: controlplanev1.SSHConfig{
						User:                "rocky",
						PrivateKeySecretRef: controlplanev1.LocalObjectReference{Name: "fleet-ssh"},
					},
				},
			},
		},
	}

	err := c.Create(t.Context(), template)
	if err == nil {
		t.Fatal("the api server accepted a template whose distro names no packageVersion")
	}
	if !strings.Contains(err.Error(), "needs both name and packageVersion") {
		t.Fatalf("the api server refused with %q, want the packageVersion rule", err)
	}
}
