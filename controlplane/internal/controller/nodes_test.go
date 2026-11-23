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

package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
)

// machine builds a surviving worker machine in a profile, with or without a node reference.
func machine(name, profile, nodeRef string) *clusterv1.Machine {
	m := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      map[string]string{clusterv1.MachineDeploymentNameLabel: profile},
			Annotations: map[string]string{preTerminateAnnotation: ""},
		},
	}
	m.Status.NodeRef = clusterv1.MachineNodeReference{Name: nodeRef}
	return m
}

// departing is the machine Cluster API is holding on the hook.
func departing(name string) *clusterv1.Machine {
	m := machine(name, "infrastructure", name)
	now := metav1.Now()
	m.DeletionTimestamp = &now
	return m
}

// node builds a Node, ready or not, carrying the role label or not.
func node(name, profile string, ready, labelled bool) corev1.Node {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	labels := map[string]string{}
	if labelled {
		labels[nodeRoleLabelPrefix+profile] = "true"
	}
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: status,
				},
			},
		},
	}
}

// TestHoldPreTerminateHooks covers the gate the departing machine's instance waits behind. Every
// "held" case here is a cluster where releasing the hook destroys the only node carrying a role
// label that workloads select on.
func TestHoldPreTerminateHooks(t *testing.T) {
	tests := []struct {
		name       string
		labelNodes bool
		machines   []*clusterv1.Machine
		nodes      []corev1.Node
		nodesErr   error
		wantFrag   string
	}{
		{
			name:     "no machine is being deleted, so there is nothing to hold",
			machines: []*clusterv1.Machine{machine("new", "infrastructure", "")},
			wantFrag: "",
		},
		{
			name: "a survivor that has not joined holds the hook",
			machines: []*clusterv1.Machine{
				departing("old"),
				machine("new", "infrastructure", ""),
			},
			wantFrag: "machine new has not joined",
		},
		{
			name: "a survivor whose node has not appeared holds the hook",
			machines: []*clusterv1.Machine{
				departing("old"),
				machine("new", "infrastructure", "new"),
			},
			nodes:    nil,
			wantFrag: "node new has not appeared",
		},
		{
			name: "a survivor whose node is not ready holds the hook",
			machines: []*clusterv1.Machine{
				departing("old"),
				machine("new", "infrastructure", "new"),
			},
			nodes:    []corev1.Node{node("new", "infrastructure", false, true)},
			wantFrag: "node new is not ready",
		},
		{
			// The bug this gate exists for: cargoship's label phase skips a host whose Node it
			// could not find, so a converge can succeed with the role label missing.
			name:       "a survivor missing its role label holds the hook",
			labelNodes: true,
			machines: []*clusterv1.Machine{
				departing("old"),
				machine("new", "infrastructure", "new"),
			},
			nodes:    []corev1.Node{node("new", "infrastructure", true, false)},
			wantFrag: "does not carry node-role.kubernetes.io/infrastructure=true",
		},
		{
			// Nothing writes the label when it was not asked for, so gating on it would hold
			// every hook forever.
			name:       "the label is not required when cargoship was not asked to write it",
			labelNodes: false,
			machines: []*clusterv1.Machine{
				departing("old"),
				machine("new", "infrastructure", "new"),
			},
			nodes:    []corev1.Node{node("new", "infrastructure", true, false)},
			wantFrag: "",
		},
		{
			name:       "a joined, ready, labelled survivor releases the hook",
			labelNodes: true,
			machines: []*clusterv1.Machine{
				departing("old"),
				machine("new", "infrastructure", "new"),
			},
			nodes:    []corev1.Node{node("new", "infrastructure", true, true)},
			wantFrag: "",
		},
		{
			// A workload API that cannot answer is the ordinary state of a cluster mid-change,
			// so it holds rather than escalating.
			name:       "a cluster that cannot be read holds the hook",
			labelNodes: true,
			machines: []*clusterv1.Machine{
				departing("old"),
				machine("new", "infrastructure", "new"),
			},
			nodesErr: errors.New("connection refused"),
			wantFrag: "unable to read the cluster's nodes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &CargoshipControlPlaneReconciler{
				Nodes: func(context.Context, *clusterv1.Cluster) ([]corev1.Node, error) {
					return tt.nodes, tt.nodesErr
				},
			}
			cp := &controlplanev1.CargoshipControlPlane{
				Spec: controlplanev1.CargoshipControlPlaneSpec{
					Apply: controlplanev1.ApplyConfig{LabelNodes: tt.labelNodes},
				},
			}

			got := r.holdPreTerminateHooks(context.Background(), &clusterv1.Cluster{}, cp, tt.machines)
			switch {
			case tt.wantFrag == "" && got != "":
				t.Errorf("holdPreTerminateHooks() = %q, want the hook released", got)
			case tt.wantFrag != "" && !strings.Contains(got, tt.wantFrag):
				t.Errorf("holdPreTerminateHooks() = %q, want it to mention %q", got, tt.wantFrag)
			}
		})
	}
}
