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
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bootstrapv1 "github.com/colonel-byte/cluster-api-provider-cargoship/bootstrap/api/v1alpha1"
	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
)

// preTerminateAnnotation is the full annotation key the controller claims on each machine.
var preTerminateAnnotation = fmt.Sprintf("%s/%s",
	clusterv1.PreTerminateDeleteHookAnnotationPrefix, controlplanev1.PreTerminateHookName)

// clusterMachines returns every Machine belonging to the cluster, in a stable order.
func (r *CargoshipControlPlaneReconciler) clusterMachines(
	ctx context.Context,
	cluster *clusterv1.Cluster,
) ([]*clusterv1.Machine, error) {
	list := &clusterv1.MachineList{}
	if err := r.List(ctx, list,
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels{clusterv1.ClusterNameLabel: cluster.Name},
	); err != nil {
		return nil, fmt.Errorf("unable to list the cluster's machines: %w", err)
	}

	out := make([]*clusterv1.Machine, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, &list.Items[i])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// filterControlPlane keeps the machines carrying the control-plane label.
func filterControlPlane(machines []*clusterv1.Machine) []*clusterv1.Machine {
	var out []*clusterv1.Machine
	for _, m := range machines {
		if _, ok := m.Labels[clusterv1.MachineControlPlaneLabel]; ok {
			out = append(out, m)
		}
	}
	return out
}

// machineSelector is the label selector the scale subresource reports.
func machineSelector(clusterName, controlPlaneName string) labels.Selector {
	return labels.SelectorFromValidatedSet(labels.Set{
		clusterv1.ClusterNameLabel:             clusterName,
		clusterv1.MachineControlPlaneLabel:     "",
		clusterv1.MachineControlPlaneNameLabel: controlPlaneName,
	})
}

// reconcileReplicas brings the control-plane machine count to spec.replicas.
//
// It changes the count by one per pass, and reports done=false when it did. Creating or deleting
// several at once would mean several machines joining etcd simultaneously, which is the way to
// lose a quorum during what should be a routine scale.
func (r *CargoshipControlPlaneReconciler) reconcileReplicas(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	cp *controlplanev1.CargoshipControlPlane,
	controlPlaneMachines []*clusterv1.Machine,
) (ctrl.Result, bool, error) {
	logger := log.FromContext(ctx)
	desired := int(ptr.Deref(cp.Spec.Replicas, 1))
	current := len(controlPlaneMachines)

	switch {
	case current < desired:
		// The first machine is the one the whole cluster waits on, so there is nothing to be
		// gained by holding it back; the ones after it are added one at a time anyway.
		if err := r.createMachine(ctx, cluster, cp); err != nil {
			return ctrl.Result{}, false, err
		}
		logger.Info("created a control-plane machine", "have", current+1, "want", desired)
		return ctrl.Result{RequeueAfter: waitForInfrastructure}, false, nil

	case current > desired:
		// Newest first, so a scale-down undoes the most recent scale-up rather than removing the
		// machine the rest of the fleet has been joining through.
		victim := newest(controlPlaneMachines)
		if !victim.DeletionTimestamp.IsZero() {
			// Already going. The pre-terminate hook is what is holding it, and the hook is
			// released once the surviving fleet has been converged -- which happens later in
			// this same reconcile, so there is nothing to do but let it proceed.
			return ctrl.Result{RequeueAfter: waitForInfrastructure}, true, nil
		}
		if err := r.Delete(ctx, victim); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, false, fmt.Errorf("unable to delete machine %s: %w", victim.Name, err)
		}
		// The API server now holds a deletion timestamp for it and this copy does not, and
		// everything downstream in this pass decides by that field -- above all the inventory
		// the survivors are converged from, which is the whole point of the window the
		// pre-terminate hook holds open. Stamped rather than re-read: a machine whose
		// infrastructure provider set no finalizer is already gone, and a re-read would fail.
		now := metav1.Now()
		victim.DeletionTimestamp = &now
		logger.Info("deleting a control-plane machine", "machine", victim.Name, "have", current, "want", desired)
		// Deliberately done=true: the converge below has to run with the departing host removed
		// from the inventory, so the survivors' /etc/hosts and firewall rules are rewritten
		// before the hook releases the instance.
		return ctrl.Result{}, true, nil
	}

	return ctrl.Result{}, true, nil
}

// createMachine creates one control-plane Machine and the CargoshipConfig behind it.
func (r *CargoshipControlPlaneReconciler) createMachine(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	cp *controlplanev1.CargoshipControlPlane,
) error {
	name := fmt.Sprintf("%s-", cp.Name)

	config := &bootstrapv1.CargoshipConfig{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: name,
			Namespace:    cp.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{*ownerRef(cp)},
		},
		Spec: bootstrapv1.CargoshipConfigSpec{
			User: cp.Spec.SSH.User,
		},
	}
	if err := r.Create(ctx, config); err != nil {
		return fmt.Errorf("unable to create the bootstrap config: %w", err)
	}

	machineLabels := map[string]string{
		clusterv1.ClusterNameLabel:             cluster.Name,
		clusterv1.MachineControlPlaneLabel:     "",
		clusterv1.MachineControlPlaneNameLabel: cp.Name,
	}
	for k, v := range cp.Spec.MachineTemplate.ObjectMeta.Labels {
		machineLabels[k] = v
	}

	annotations := map[string]string{
		// Claimed at creation, not at deletion: by the time a machine is being deleted it is
		// already draining, and there is no window left to ask for one.
		preTerminateAnnotation: "",
	}
	for k, v := range cp.Spec.MachineTemplate.ObjectMeta.Annotations {
		annotations[k] = v
	}

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName:    name,
			Namespace:       cp.Namespace,
			Labels:          machineLabels,
			Annotations:     annotations,
			OwnerReferences: []metav1.OwnerReference{*ownerRef(cp)},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     cp.Spec.Version,
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					APIGroup: bootstrapv1.GroupVersion.Group,
					Kind:     "CargoshipConfig",
					Name:     config.Name,
				},
			},
			InfrastructureRef: cp.Spec.MachineTemplate.Spec.InfrastructureRef,
			FailureDomain:     r.failureDomain(cp),
		},
	}
	if err := r.Create(ctx, machine); err != nil {
		return fmt.Errorf("unable to create the machine: %w", err)
	}
	return nil
}

// failureDomain picks the next failure domain to place a machine in, or "" for the provider's
// choice.
//
// Round-robin over the listed domains would need to know how many machines are already in each,
// which is a question this provider does not answer yet; the first one is returned so a cluster
// that named exactly one gets it, and a cluster that named several gets a note to say the rest are
// ignored.
func (r *CargoshipControlPlaneReconciler) failureDomain(cp *controlplanev1.CargoshipControlPlane) string {
	if len(cp.Spec.MachineTemplate.Spec.FailureDomains) == 0 {
		return ""
	}
	return cp.Spec.MachineTemplate.Spec.FailureDomains[0]
}

// ensurePreTerminateHooks puts the hook annotation on any machine missing it.
//
// Machines created by this controller get it at creation. This covers the ones that did not: a
// control plane adopted from somewhere else, or a machine created by a version of this controller
// that did not set it.
func (r *CargoshipControlPlaneReconciler) ensurePreTerminateHooks(ctx context.Context, machines []*clusterv1.Machine) error {
	for _, m := range machines {
		if _, ok := m.Annotations[preTerminateAnnotation]; ok {
			continue
		}
		// A machine already being deleted is past the point where claiming the hook does
		// anything, and Cluster API ignores one added after deletion began.
		if !m.DeletionTimestamp.IsZero() {
			continue
		}

		patched := m.DeepCopy()
		if patched.Annotations == nil {
			patched.Annotations = map[string]string{}
		}
		patched.Annotations[preTerminateAnnotation] = ""
		if err := r.Patch(ctx, patched, client.MergeFrom(m)); err != nil {
			return fmt.Errorf("unable to claim the pre-terminate hook on %s: %w", m.Name, err)
		}
	}
	return nil
}

// releasePreTerminateHooks removes the hook from machines that are being deleted.
//
// This is the other half of the removal story. Cluster API has, by this point, drained the node
// and deleted the Node object, and is waiting on this annotation before letting the infrastructure
// provider destroy the instance. The converge that ran before this call rewrote the surviving
// hosts' /etc/hosts and firewall rules from an inventory the departing host is no longer in, so
// releasing it now is safe -- and releasing it earlier would leave the survivors pointing at a
// machine that no longer exists.
func (r *CargoshipControlPlaneReconciler) releasePreTerminateHooks(ctx context.Context, machines []*clusterv1.Machine) error {
	logger := log.FromContext(ctx)

	for _, m := range machines {
		if m.DeletionTimestamp.IsZero() {
			continue
		}
		if _, ok := m.Annotations[preTerminateAnnotation]; !ok {
			continue
		}

		patched := m.DeepCopy()
		delete(patched.Annotations, preTerminateAnnotation)
		if err := r.Patch(ctx, patched, client.MergeFrom(m)); err != nil {
			return fmt.Errorf("unable to release the pre-terminate hook on %s: %w", m.Name, err)
		}
		logger.Info("released the pre-terminate hook", "machine", m.Name)
	}
	return nil
}

// reconcileDelete tears the control plane down.
//
// Machines are deleted and their hooks released without a converge: the whole fleet is going, so
// there are no survivors whose /etc/hosts needs correcting, and `cargoship reset` would be
// uninstalling software from instances about to be destroyed.
func (r *CargoshipControlPlaneReconciler) reconcileDelete(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	cp *controlplanev1.CargoshipControlPlane,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Any converge still walking the fleet is pointless now, and would keep SSH sessions open
	// against hosts being destroyed.
	r.Runner.Cancel(client.ObjectKeyFromObject(cp).String())

	machines, err := r.clusterMachines(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	controlPlaneMachines := filterControlPlane(machines)

	if err := r.releasePreTerminateHooks(ctx, controlPlaneMachines); err != nil {
		return ctrl.Result{}, err
	}

	remaining := 0
	for _, m := range controlPlaneMachines {
		remaining++
		if !m.DeletionTimestamp.IsZero() {
			continue
		}
		if err := r.Delete(ctx, m); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("unable to delete machine %s: %w", m.Name, err)
		}
	}

	if remaining > 0 {
		logger.V(1).Info("waiting for the control-plane machines to go", "remaining", remaining)
		return ctrl.Result{RequeueAfter: waitForInfrastructure}, nil
	}

	controllerutil.RemoveFinalizer(cp, controlplanev1.ControlPlaneFinalizer)
	return ctrl.Result{}, nil
}

// controlPlanesForMachine maps any Machine to the CargoshipControlPlane of its cluster.
//
// Worker machines are included on purpose: they are part of the converge, and nothing else would
// wake the control plane when one appears, gets an address, or starts being deleted.
func (r *CargoshipControlPlaneReconciler) controlPlanesForMachine(ctx context.Context, obj client.Object) []ctrl.Request {
	machine, ok := obj.(*clusterv1.Machine)
	if !ok {
		return nil
	}

	cluster := &clusterv1.Cluster{}
	key := types.NamespacedName{
		Namespace: machine.Namespace,
		Name:      machine.Spec.ClusterName,
	}
	if err := r.Get(ctx, key, cluster); err != nil {
		return nil
	}
	if cluster.Spec.ControlPlaneRef.Kind != "CargoshipControlPlane" {
		return nil
	}

	return []ctrl.Request{
		{
			NamespacedName: types.NamespacedName{
				Namespace: cluster.Namespace,
				Name:      cluster.Spec.ControlPlaneRef.Name,
			},
		},
	}
}

// newest returns the most recently created machine, by creation timestamp then name.
func newest(machines []*clusterv1.Machine) *clusterv1.Machine {
	out := machines[0]
	for _, m := range machines[1:] {
		if m.CreationTimestamp.After(out.CreationTimestamp.Time) {
			out = m
			continue
		}
		// Machines created in the same second are common, and a tie broken arbitrarily would
		// make a scale-down pick a different victim on each pass.
		if m.CreationTimestamp.Equal(&out.CreationTimestamp) && m.Name > out.Name {
			out = m
		}
	}
	return out
}

// allJoined reports whether every machine has a node reference, which is Cluster API's way of
// saying the node turned up in the cluster.
func allJoined(machines []*clusterv1.Machine) bool {
	for _, m := range machines {
		if !m.DeletionTimestamp.IsZero() {
			continue
		}
		if m.Status.NodeRef.Name == "" {
			return false
		}
	}
	return true
}

// isAvailable reports whether a machine is available, by its own condition.
func isAvailable(m *clusterv1.Machine) bool {
	for _, c := range m.Status.Conditions {
		if c.Type == clusterv1.MachineAvailableCondition {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}

// ownerRef is the controller owner reference for a control plane.
func ownerRef(cp *controlplanev1.CargoshipControlPlane) *metav1.OwnerReference {
	return &metav1.OwnerReference{
		APIVersion:         controlplanev1.GroupVersion.String(),
		Kind:               "CargoshipControlPlane",
		Name:               cp.Name,
		UID:                cp.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
}

// setCondition records one condition on a control plane.
func setCondition(
	cp *controlplanev1.CargoshipControlPlane,
	conditionType string,
	status metav1.ConditionStatus,
	reason, message string,
) {
	apimeta.SetStatusCondition(&cp.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cp.Generation,
	})
}
