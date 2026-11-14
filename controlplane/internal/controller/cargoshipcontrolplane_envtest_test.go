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

// The reconcile state machine, end to end against a real API server.
//
// What every test here asserts is a requeue and a condition, never a duration measured on the
// clock: the provider's whole answer to machines and API servers coming up on their own schedules
// is that nothing gives up, so what is worth pinning down is which state each pass lands in and
// what it does on the way through. The converge itself is a stub -- there is no fleet here, and
// the suite that drives a real one is a separate thing. See docs/dev/e2e-tests.md.
package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1 "github.com/colonel-byte/cluster-api-provider-cargoship/bootstrap/api/v1alpha1"
	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
)

func TestReconcileWaitsForTheOwningCluster(t *testing.T) {
	f := newFixture(t)

	f.cp.OwnerReferences = nil
	if err := f.c.Update(t.Context(), f.cp); err != nil {
		t.Fatalf("clearing the owner reference: %v", err)
	}

	result := f.reconcile(t)
	if result.RequeueAfter != waitForOwner {
		t.Errorf("requeued after %s, want %s", result.RequeueAfter, waitForOwner)
	}
	if len(f.machines(t)) != 0 {
		t.Error("created a machine before the owning Cluster was known")
	}
}

func TestReconcileWaitsForClusterInfrastructure(t *testing.T) {
	f := newFixture(t)

	f.cluster.Status.Initialization.InfrastructureProvisioned = ptr.To(false)
	if err := f.c.Status().Update(t.Context(), f.cluster); err != nil {
		t.Fatalf("un-provisioning the infrastructure: %v", err)
	}

	result := f.reconcile(t)
	if result.RequeueAfter != waitForInfrastructure {
		t.Errorf("requeued after %s, want %s", result.RequeueAfter, waitForInfrastructure)
	}
	if got := f.condition(t, controlplanev1.AvailableCondition); got.Reason != "WaitingForInfrastructure" {
		t.Errorf("Available reason = %q, want WaitingForInfrastructure", got.Reason)
	}
	if len(f.machines(t)) != 0 {
		t.Error("created a machine before the cluster infrastructure was provisioned")
	}
}

func TestReconcileCreatesMachinesOneAtATime(t *testing.T) {
	f := newFixture(t)
	f.setReplicas(t, 2)

	f.reconcile(t)
	machines := f.machines(t)
	if len(machines) != 1 {
		t.Fatalf("the first pass created %d machines, want 1: a scale is one machine per pass so each creation is observed before the next", len(machines))
	}

	m := machines[0]
	if m.Spec.ClusterName != f.cluster.Name {
		t.Errorf("machine cluster name = %q, want %q", m.Spec.ClusterName, f.cluster.Name)
	}
	if m.Spec.Version != f.cp.Spec.Version {
		t.Errorf("machine version = %q, want %q", m.Spec.Version, f.cp.Spec.Version)
	}
	if _, ok := m.Labels[clusterv1.MachineControlPlaneLabel]; !ok {
		t.Error("machine is missing the control-plane label, so the converge would build it as a worker")
	}
	if _, ok := m.Annotations[preTerminateAnnotation]; !ok {
		t.Error("machine is missing the pre-terminate hook, which cannot be claimed once it is being deleted")
	}
	if m.Spec.InfrastructureRef.Kind != "StubMachineTemplate" {
		t.Errorf("machine infrastructure ref = %q, want the template's", m.Spec.InfrastructureRef.Kind)
	}
	if len(m.OwnerReferences) != 1 || m.OwnerReferences[0].Name != f.cp.Name {
		t.Errorf("machine owner references = %v, want the control plane", m.OwnerReferences)
	}

	config := &bootstrapv1.CargoshipConfig{}
	key := types.NamespacedName{Namespace: f.ns, Name: m.Spec.Bootstrap.ConfigRef.Name}
	if err := f.c.Get(t.Context(), key, config); err != nil {
		t.Fatalf("reading the machine's bootstrap config %s: %v", key, err)
	}
	if config.Spec.User != f.cp.Spec.SSH.User {
		t.Errorf("bootstrap config user = %q, want %q", config.Spec.User, f.cp.Spec.SSH.User)
	}

	f.provision(t, &machines[0], "10.0.0.11")
	f.reconcile(t)
	if got := len(f.machines(t)); got != 2 {
		t.Fatalf("the second pass left %d machines, want 2", got)
	}
}

func TestReconcileWaitsForAnAddress(t *testing.T) {
	f := newFixture(t)

	f.reconcile(t)
	if len(f.machines(t)) != 1 {
		t.Fatal("the first pass created no machine")
	}

	// The machine exists and the infrastructure provider has not given it an address yet, which
	// is the ordinary state of a cluster being created and must not be an error.
	result := f.reconcile(t)
	if result.RequeueAfter != waitForInfrastructure {
		t.Errorf("requeued after %s, want %s", result.RequeueAfter, waitForInfrastructure)
	}
	if got := f.condition(t, controlplanev1.MachinesReadyCondition); got.Reason != "WaitingForAddress" {
		t.Errorf("MachinesReady reason = %q, want WaitingForAddress", got.Reason)
	}
	if f.converge.count() != 0 {
		t.Error("started a converge against a fleet with no addresses")
	}
}

func TestReconcileWaitsForSSH(t *testing.T) {
	f := newFixture(t)
	f.r.Probe = stubProbe{reachable: false}
	f.grow(t, 1)

	result := f.reconcile(t)
	if result.RequeueAfter != waitForInfrastructure {
		t.Errorf("requeued after %s, want %s", result.RequeueAfter, waitForInfrastructure)
	}
	if got := f.condition(t, controlplanev1.ConvergedCondition); got.Reason != "WaitingForSSH" {
		t.Errorf("Converged reason = %q, want WaitingForSSH", got.Reason)
	}
	// The dial is there so a machine that is still booting is waited for by the reconcile rather
	// than inside a cargoship phase, where it would hold the runner slot for fifteen minutes.
	if f.converge.count() != 0 {
		t.Error("started a converge against a machine that is not accepting ssh")
	}
}

func TestReconcileConvergesAndPublishesTheKubeconfig(t *testing.T) {
	f := newFixture(t)
	f.grow(t, 1)

	result := f.reconcile(t)
	if result.RequeueAfter != waitForInfrastructure {
		t.Errorf("requeued after %s while converging, want %s", result.RequeueAfter, waitForInfrastructure)
	}
	if got := f.condition(t, controlplanev1.ConvergedCondition); got.Reason != "Converging" {
		t.Fatalf("Converged reason = %q, want Converging", got.Reason)
	}

	f.waitForConverge(t)
	result = f.reconcile(t)

	if got := f.condition(t, controlplanev1.ConvergedCondition); got.Status != metav1.ConditionTrue {
		t.Errorf("Converged = %s/%s, want True", got.Status, got.Reason)
	}
	if got := f.condition(t, controlplanev1.AvailableCondition); got.Status != metav1.ConditionTrue {
		t.Errorf("Available = %s/%s, want True", got.Status, got.Reason)
	}
	// The contract field that releases worker machines to be created.
	if !ptr.Deref(f.cp.Status.Initialization.ControlPlaneInitialized, false) {
		t.Error("status.initialization.controlPlaneInitialized is not set, so no worker would ever be created")
	}
	if f.cp.Status.LastAppliedHash == "" {
		t.Error("status.lastAppliedHash is empty, so the next pass would converge the fleet again")
	}
	if f.cp.Status.Version != f.cp.Spec.Version {
		t.Errorf("status.version = %q, want %q", f.cp.Status.Version, f.cp.Spec.Version)
	}
	if len(f.cp.Status.Hosts) != 1 || f.cp.Status.Hosts[0].Address != "10.0.0.11" {
		t.Fatalf("status.hosts = %v, want one entry for 10.0.0.11", f.cp.Status.Hosts)
	}
	if f.cp.Status.Hosts[0].Role != "controller" {
		t.Errorf("status.hosts[0].role = %q, want controller", f.cp.Status.Hosts[0].Role)
	}
	if result.RequeueAfter != defaultDriftInterval {
		t.Errorf("requeued after %s once converged, want the drift interval %s", result.RequeueAfter, defaultDriftInterval)
	}

	// The name is fixed by convention: everything from clusterctl to Rancher Turtles looks for
	// <cluster>-kubeconfig.
	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: f.ns, Name: f.cluster.Name + "-kubeconfig"}
	if err := f.c.Get(t.Context(), key, secret); err != nil {
		t.Fatalf("reading the kubeconfig secret %s: %v", key, err)
	}
	if len(secret.Data["value"]) == 0 {
		t.Error("the kubeconfig secret has no value key")
	}
	if secret.Labels[clusterv1.ClusterNameLabel] != f.cluster.Name {
		t.Errorf("kubeconfig secret cluster label = %q, want %q", secret.Labels[clusterv1.ClusterNameLabel], f.cluster.Name)
	}
}

func TestReconcileDoesNotConvergeAgainForTheSameState(t *testing.T) {
	f := newFixture(t)
	f.grow(t, 1)

	f.reconcile(t)
	f.waitForConverge(t)
	f.reconcile(t)
	if f.converge.count() != 1 {
		t.Fatalf("%d converges to reach the desired state, want 1", f.converge.count())
	}

	// A resync finds the fingerprint unchanged and goes back to sleep. This is a cost control
	// rather than a correctness gate -- a converge is idempotent -- but a multi-minute SSH walk
	// per resync is not something to discover in production.
	result := f.reconcile(t)
	if f.converge.count() != 1 {
		t.Errorf("a resync started converge %d, want none", f.converge.count())
	}
	if result.RequeueAfter != defaultDriftInterval {
		t.Errorf("requeued after %s, want the drift interval %s", result.RequeueAfter, defaultDriftInterval)
	}
}

func TestReconcileRetriesAfterAFailedConverge(t *testing.T) {
	f := newFixture(t)
	f.converge.err = errConvergeFailed
	f.grow(t, 1)

	f.reconcile(t)
	f.waitForConverge(t)
	result := f.reconcile(t)

	if got := f.condition(t, controlplanev1.ConvergedCondition); got.Reason != "ConvergeFailed" {
		t.Errorf("Converged reason = %q, want ConvergeFailed", got.Reason)
	}
	if f.cp.Status.LastAppliedHash != "" {
		t.Error("recorded a fingerprint for a converge that failed")
	}
	if result.RequeueAfter != waitForInfrastructure {
		t.Errorf("requeued after %s, want %s", result.RequeueAfter, waitForInfrastructure)
	}

	// The failed run is forgotten rather than reported forever, so the pass after it tries again.
	f.converge.mu.Lock()
	f.converge.err = nil
	f.converge.mu.Unlock()

	f.reconcile(t)
	f.waitForConverge(t)
	f.reconcile(t)
	if f.converge.count() != 2 {
		t.Errorf("%d converges in total, want a second attempt after the failure", f.converge.count())
	}
	if got := f.condition(t, controlplanev1.ConvergedCondition); got.Status != metav1.ConditionTrue {
		t.Errorf("Converged = %s/%s after the retry, want True", got.Status, got.Reason)
	}
}

func TestReconcileConvergesTheSurvivorsBeforeReleasingThePreTerminateHook(t *testing.T) {
	f := newFixture(t)
	f.setReplicas(t, 2)
	f.grow(t, 2)

	f.reconcile(t)
	f.waitForConverge(t)
	f.reconcile(t)
	if len(f.converge.last(t)) != 2 {
		t.Fatalf("the first converge saw %v, want both hosts", f.converge.last(t))
	}

	f.setReplicas(t, 1)
	f.reconcile(t)

	departing, surviving := partitionByDeletion(t, f.machines(t))
	if _, ok := departing.Annotations[preTerminateAnnotation]; !ok {
		t.Fatal("the pre-terminate hook was released while the survivors' converge was still in flight")
	}

	// The window the hook holds open is for exactly this: the survivors are converged from a host
	// list the departing machine is not in, so their /etc/hosts and firewall rules stop naming it
	// before the instance is destroyed.
	hosts := f.converge.last(t)
	if len(hosts) != 1 || hosts[0] != addressOf(t, surviving) {
		t.Fatalf("the scale-down converge saw %v, want only the surviving host %s", hosts, addressOf(t, surviving))
	}

	f.waitForConverge(t)
	f.reconcile(t)

	if err := f.c.Get(t.Context(), client.ObjectKeyFromObject(&departing), &departing); err != nil {
		t.Fatalf("reloading the departing machine: %v", err)
	}
	if _, ok := departing.Annotations[preTerminateAnnotation]; ok {
		t.Error("the pre-terminate hook is still held after the survivors converged, so the instance is never destroyed")
	}
}

func TestReconcileDeleteRemovesTheMachinesAndTheFinalizer(t *testing.T) {
	f := newFixture(t)
	f.grow(t, 1)
	f.reconcile(t)
	f.waitForConverge(t)
	f.reconcile(t)

	if err := f.c.Delete(t.Context(), f.cp); err != nil {
		t.Fatalf("deleting the control plane: %v", err)
	}

	result := f.reconcile(t)
	if result.RequeueAfter != waitForInfrastructure {
		t.Errorf("requeued after %s while machines remain, want %s", result.RequeueAfter, waitForInfrastructure)
	}

	machines := f.machines(t)
	if len(machines) != 1 || machines[0].DeletionTimestamp.IsZero() {
		t.Fatalf("machines = %v, want the one machine to be deleting", machines)
	}

	// The hook is released on the pass after the deletion rather than the one that made it: the
	// release skips a machine that is not yet deleting, and the machine list this pass read was
	// fetched before it deleted anything.
	f.reconcile(t)
	machines = f.machines(t)
	// No converge on the way out: the whole fleet is going, so there are no survivors whose
	// /etc/hosts needs correcting.
	if f.converge.count() != 1 {
		t.Errorf("%d converges, want no converge during teardown", f.converge.count())
	}
	if _, ok := machines[0].Annotations[preTerminateAnnotation]; ok {
		t.Error("the pre-terminate hook is still held, so the infrastructure provider never destroys the instance")
	}

	// What Cluster API and the infrastructure provider would now do.
	machines[0].Finalizers = nil
	if err := f.c.Update(t.Context(), &machines[0]); err != nil {
		t.Fatalf("releasing the test finalizer: %v", err)
	}

	if _, err := f.r.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: f.ns, Name: f.cp.Name},
	}); err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("the final pass: %v", err)
	}

	err := f.c.Get(t.Context(), client.ObjectKeyFromObject(f.cp), f.cp)
	if !apierrors.IsNotFound(err) {
		t.Errorf("the control plane is still there (%v), so its finalizer was not removed", err)
	}
}

// partitionByDeletion splits a machine list into the one being deleted and the one that is not,
// and fails unless there is exactly one of each.
//
// The victim is chosen by creation timestamp, and machines created in the same second are
// separated by name, so which of two machines a scale-down picks is not something a test gets to
// assume.
func partitionByDeletion(t *testing.T, machines []clusterv1.Machine) (departing, surviving clusterv1.Machine) {
	t.Helper()

	var deleting, remaining []clusterv1.Machine
	for _, m := range machines {
		if m.DeletionTimestamp.IsZero() {
			remaining = append(remaining, m)
			continue
		}
		deleting = append(deleting, m)
	}
	if len(deleting) != 1 || len(remaining) != 1 {
		t.Fatalf("%d machines deleting and %d remaining, want one of each", len(deleting), len(remaining))
	}
	return deleting[0], remaining[0]
}

// addressOf is a machine's internal address.
func addressOf(t *testing.T, m clusterv1.Machine) string {
	t.Helper()

	for _, a := range m.Status.Addresses {
		if a.Type == clusterv1.MachineInternalIP {
			return a.Address
		}
	}
	t.Fatalf("machine %s has no internal address", m.Name)
	return ""
}
