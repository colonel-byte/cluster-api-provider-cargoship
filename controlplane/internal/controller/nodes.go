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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/converge"
)

// kubeConfigSecretKey is the key Cluster API reads a kubeconfig secret's contents from. Fixed by
// convention, like the secret's name.
const kubeConfigSecretKey = "value"

// nodeRoleLabelPrefix is the prefix of the label cargoship's LabelNodes phase writes, as
// pkg/phase/81_label_nodes.go spells it. The value it writes is the string "true".
const nodeRoleLabelPrefix = "node-role.kubernetes.io/"

// NodeListFunc lists the Nodes of the workload cluster.
//
// It is a function for the reason ConvergeFunc is: the gate below is the one piece of the
// reconcile that reads the cluster being built rather than the one the controller runs in, and a
// test has no such cluster to read. The one implementation outside tests is workloadNodes, which
// SetupWithManager installs.
type NodeListFunc func(ctx context.Context, cluster *clusterv1.Cluster) ([]corev1.Node, error)

// workloadNodes lists the workload cluster's Nodes, through the kubeconfig the converge published.
//
// A client per call, rather than a cached one per cluster: the only caller is the pre-terminate
// gate, which runs during the seconds a machine is being removed and not at all otherwise, so a
// cache would be a lifecycle to get wrong in exchange for nothing.
func (r *CargoshipControlPlaneReconciler) workloadNodes(
	ctx context.Context,
	cluster *clusterv1.Cluster,
) ([]corev1.Node, error) {
	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: cluster.Namespace, Name: fmt.Sprintf("%s-kubeconfig", cluster.Name)}
	if err := r.Get(ctx, key, secret); err != nil {
		return nil, fmt.Errorf("unable to read %s: %w", key, err)
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(secret.Data[kubeConfigSecretKey])
	if err != nil {
		return nil, fmt.Errorf("unable to read the kubeconfig in %s: %w", key, err)
	}

	c, err := client.New(restConfig, client.Options{Scheme: r.Scheme()})
	if err != nil {
		return nil, fmt.Errorf("unable to connect to the workload cluster: %w", err)
	}

	list := &corev1.NodeList{}
	if err := c.List(ctx, list); err != nil {
		return nil, fmt.Errorf("unable to list the workload cluster's nodes: %w", err)
	}
	return list.Items, nil
}

// holdPreTerminateHooks reports why the hooks on the departing machines have to stay held, or ""
// when they can be released.
//
// The converge having succeeded says the survivors' /etc/hosts and firewall rules no longer name
// the machine that is going. It does not say the machine that is replacing it has arrived: Cluster
// API may not have matched its Node yet, the Node may not be Ready, and cargoship's LabelNodes
// phase skips a host whose Node it could not find -- so the role label a workload's affinity
// selects can be missing from a fleet that converged cleanly. Releasing the hook then lets the
// infrastructure provider destroy the departing instance while nothing yet carries its label, and
// the pods that wanted it go unschedulable.
//
// So the gate is on the survivors, every one of them rather than the control-plane ones, because
// a worker pool rotation is the case that prompted it. There is no deadline here, as everywhere
// else in this reconcile: a replacement that takes eleven minutes to join holds the hook for
// eleven minutes, and the Machine watch wakes the reconcile when it does.
//
// Errors are reasons rather than errors. Asking the workload API a question it cannot answer yet
// is the ordinary state of a cluster mid-change, and escalating it would back off the one
// reconcile that has something to wait for.
func (r *CargoshipControlPlaneReconciler) holdPreTerminateHooks(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	cp *controlplanev1.CargoshipControlPlane,
	machines []*clusterv1.Machine,
) string {
	var departing, survivors []*clusterv1.Machine
	for _, m := range machines {
		if m.DeletionTimestamp.IsZero() {
			survivors = append(survivors, m)
			continue
		}
		if _, ok := m.Annotations[preTerminateAnnotation]; ok {
			departing = append(departing, m)
		}
	}
	// Nothing is being held, so there is nothing to gate -- above all the first converge of a new
	// cluster, whose machines have no Nodes yet by definition.
	if len(departing) == 0 {
		return ""
	}

	for _, m := range survivors {
		if m.Status.NodeRef.Name == "" {
			return fmt.Sprintf("machine %s has not joined the cluster yet, so %s is held",
				m.Name, departing[0].Name)
		}
	}

	nodes, err := r.Nodes(ctx, cluster)
	if err != nil {
		return fmt.Sprintf("unable to read the cluster's nodes, so %s is held: %v", departing[0].Name, err)
	}
	byName := make(map[string]*corev1.Node, len(nodes))
	for i := range nodes {
		byName[nodes[i].Name] = &nodes[i]
	}

	for _, m := range survivors {
		node, ok := byName[m.Status.NodeRef.Name]
		if !ok {
			return fmt.Sprintf("node %s has not appeared yet, so %s is held",
				m.Status.NodeRef.Name, departing[0].Name)
		}
		if !nodeReady(node) {
			return fmt.Sprintf("node %s is not ready yet, so %s is held", node.Name, departing[0].Name)
		}

		// Only when cargoship was asked to write them. Gating on a label nothing applies would
		// hold every hook forever.
		if !cp.Spec.Apply.LabelNodes {
			continue
		}
		// A machine selecting no profile gets no label, which is what the phase itself does with
		// one; see pkg/phase/81_label_nodes.go.
		profile := converge.Profile(m)
		if profile == "" {
			continue
		}
		if label := nodeRoleLabelPrefix + profile; node.Labels[label] != "true" {
			return fmt.Sprintf("node %s does not carry %s=true yet, so %s is held",
				node.Name, label, departing[0].Name)
		}
	}

	return ""
}

// nodeReady reports whether a Node's Ready condition is true.
func nodeReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
