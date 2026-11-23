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

package controller

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/pkg/phase"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
	"github.com/colonel-byte/cluster-api-provider-cargoship/internal/envtest"
	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/converge"
)

func TestMain(m *testing.M) {
	os.Exit(envtest.Run(m))
}

// stubProbe answers the SSH reachability check without a host.
type stubProbe struct {
	reachable bool
	err       error
}

func (p stubProbe) Reachable(context.Context, converge.Host) (bool, error) {
	return p.reachable, p.err
}

// stubNodes stands in for the workload cluster's Nodes, which there is no workload cluster to
// read: envtest runs an API server for the management cluster only. One Node per machine that has
// a node reference, which is the state the pre-terminate gate wants to see, with the two things it
// looks at settable.
type stubNodes struct {
	ready    bool
	labelled bool
	machines func() []clusterv1.Machine
}

func (s *stubNodes) list(context.Context, *clusterv1.Cluster) ([]corev1.Node, error) {
	status := corev1.ConditionFalse
	if s.ready {
		status = corev1.ConditionTrue
	}

	var out []corev1.Node
	for _, m := range s.machines() {
		if m.Status.NodeRef.Name == "" {
			continue
		}
		labels := map[string]string{}
		if s.labelled {
			labels[nodeRoleLabelPrefix+converge.Profile(&m)] = "true"
		}
		out = append(out, corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   m.Status.NodeRef.Name,
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
		})
	}
	return out, nil
}

// stubConverge stands in for cargoship's apply. It records the host set each converge was asked
// for, which is what the pre-terminate and scale assertions are actually about: a converge that
// ran is not the same as a converge that ran against the right fleet.
type stubConverge struct {
	mu       sync.Mutex
	runs     [][]string
	lastOpts converge.Options
	err      error
}

// apply is the ConvergeFunc the reconciler is given.
func (s *stubConverge) apply(_ context.Context, inv *converge.Inventory, opts converge.Options) (*converge.Run, error) {
	s.mu.Lock()
	addresses := make([]string, 0, len(inv.Hosts))
	for _, h := range inv.Hosts {
		addresses = append(addresses, h.Address)
	}
	s.runs = append(s.runs, addresses)
	s.lastOpts = opts
	err := s.err
	s.mu.Unlock()

	if err != nil {
		return nil, err
	}

	// A result with one host-reporting phase, so the status assertions have something to read.
	hosts := make([]phase.HostChange, 0, len(addresses))
	for _, a := range addresses {
		hosts = append(hosts, phase.HostChange{Host: a, Changed: true})
	}
	return &converge.Run{
		Result: phase.RunResult{Ran: []phase.PhaseResult{{
			Title:         "Apply",
			Declared:      true,
			Changed:       true,
			HostsDeclared: true,
			Hosts:         hosts,
		}}},
		KubeConfig: []byte("apiVersion: v1\nkind: Config\n"),
	}, nil
}

// count is how many converges have been started.
func (s *stubConverge) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.runs)
}

// options is what the most recent converge was asked to do.
func (s *stubConverge) options(t *testing.T) converge.Options {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.runs) == 0 {
		t.Fatal("no converge has been started")
	}
	return s.lastOpts
}

// last is the host set of the most recent converge.
func (s *stubConverge) last(t *testing.T) []string {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.runs) == 0 {
		t.Fatal("no converge has been started")
	}
	return s.runs[len(s.runs)-1]
}

// fixture is one cluster, its control plane, and a reconciler wired to stubs.
type fixture struct {
	c        client.Client
	ns       string
	cluster  *clusterv1.Cluster
	cp       *controlplanev1.CargoshipControlPlane
	r        *CargoshipControlPlaneReconciler
	converge *stubConverge
	nodes    *stubNodes
}

// newFixture creates a cluster whose infrastructure is provisioned and a one-replica control plane
// owned by it, which is the state every test here starts from unless it undoes part of it.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	c := envtest.Client(t)
	ns := envtest.Namespace(t, c)
	ctx := t.Context()

	if err := c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "fleet-ssh", Namespace: ns},
		Type:       corev1.SecretTypeSSHAuth,
		Data:       map[string][]byte{corev1.SSHAuthPrivateKey: testKey(t)},
	}); err != nil {
		t.Fatalf("creating the ssh secret: %v", err)
	}

	// The template every machine's infrastructure is cloned from. A real object rather than a
	// dangling reference, because createMachine reads it now.
	tmpl := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "infrastructure.cluster.x-k8s.io/v1beta1",
		"kind":       "StubMachineTemplate",
		"metadata": map[string]any{
			"name":      "stub",
			"namespace": ns,
		},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{"size": "small"},
			},
		},
	}}
	if err := c.Create(ctx, tmpl); err != nil {
		t.Fatalf("creating the machine template: %v", err)
	}

	cp := &controlplanev1.CargoshipControlPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "fleet", Namespace: ns},
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
	if err := c.Create(ctx, cp); err != nil {
		t.Fatalf("creating the control plane: %v", err)
	}

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "fleet", Namespace: ns},
		Spec: clusterv1.ClusterSpec{
			ControlPlaneRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: controlplanev1.GroupVersion.Group,
				Kind:     "CargoshipControlPlane",
				Name:     cp.Name,
			},
		},
	}
	if err := c.Create(ctx, cluster); err != nil {
		t.Fatalf("creating the cluster: %v", err)
	}
	cluster.Status.Initialization.InfrastructureProvisioned = ptr.To(true)
	if err := c.Status().Update(ctx, cluster); err != nil {
		t.Fatalf("marking the infrastructure provisioned: %v", err)
	}

	// Cluster API's own Cluster controller writes this reference, and nothing here would:
	// GetOwnerCluster is how the reconcile finds its cluster, so without it every pass stops at
	// "waiting for the owning Cluster".
	cp.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       cluster.Name,
		UID:        cluster.UID,
	}}
	if err := c.Update(ctx, cp); err != nil {
		t.Fatalf("setting the owner reference: %v", err)
	}

	stub := &stubConverge{}
	f := &fixture{
		c:        c,
		ns:       ns,
		cluster:  cluster,
		cp:       cp,
		converge: stub,
		nodes: &stubNodes{
			ready:    true,
			labelled: true,
		},
	}
	f.nodes.machines = func() []clusterv1.Machine { return f.machines(t) }
	f.r = &CargoshipControlPlaneReconciler{
		Client:   c,
		Runner:   converge.NewRunner(),
		Probe:    stubProbe{reachable: true},
		Converge: stub.apply,
		Nodes:    f.nodes.list,
	}
	return f
}

// reconcile runs one pass and reloads the control plane, so a caller reads the status the pass
// wrote rather than the copy it started from.
func (f *fixture) reconcile(t *testing.T) ctrl.Result {
	t.Helper()

	result, err := f.r.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: f.ns, Name: f.cp.Name},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	f.refresh(t)
	return result
}

// refresh reloads the control plane from the API server.
func (f *fixture) refresh(t *testing.T) {
	t.Helper()

	if err := f.c.Get(t.Context(), client.ObjectKeyFromObject(f.cp), f.cp); err != nil {
		t.Fatalf("reloading the control plane: %v", err)
	}
}

// waitForConverge blocks until the in-flight converge has finished, so the pass after it observes
// a terminal state rather than racing the goroutine.
func (f *fixture) waitForConverge(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := f.r.Runner.Wait(ctx, f.ns+"/"+f.cp.Name); err != nil {
		t.Fatalf("waiting for the converge: %v", err)
	}
}

// machines lists the cluster's machines, in the order the controller sees them.
func (f *fixture) machines(t *testing.T) []clusterv1.Machine {
	t.Helper()

	list := &clusterv1.MachineList{}
	if err := f.c.List(t.Context(), list,
		client.InNamespace(f.ns),
		client.MatchingLabels{clusterv1.ClusterNameLabel: f.cluster.Name},
	); err != nil {
		t.Fatalf("listing machines: %v", err)
	}
	sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Name < list.Items[j].Name })
	return list.Items
}

// provision does to a machine what an infrastructure provider and Cluster API would: gives it an
// address and a node reference. A finalizer goes on at the same time so that a later deletion
// leaves the object in place with a deletion timestamp, which is the state the pre-terminate hook
// assertions need to see.
func (f *fixture) provision(t *testing.T, m *clusterv1.Machine, address string) {
	t.Helper()

	f.provisionAddress(t, m, address)
	m.Status.NodeRef = clusterv1.MachineNodeReference{Name: m.Name}
	m.Status.Conditions = []metav1.Condition{{
		Type:               clusterv1.MachineAvailableCondition,
		Status:             metav1.ConditionTrue,
		Reason:             "Available",
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: m.Generation,
	}}
	if err := f.c.Status().Update(t.Context(), m); err != nil {
		t.Fatalf("setting the status of %s: %v", m.Name, err)
	}
}

// provisionAddress is provision without the node reference: the machine has booted and has an
// address, but its Node never got a matching spec.providerID, so Cluster API has nothing to point
// nodeRef at. The state a fleet with no cloud provider sits in indefinitely.
func (f *fixture) provisionAddress(t *testing.T, m *clusterv1.Machine, address string) {
	t.Helper()

	m.Finalizers = append(m.Finalizers, "test.cargoship/keep")
	if err := f.c.Update(t.Context(), m); err != nil {
		t.Fatalf("adding the test finalizer to %s: %v", m.Name, err)
	}

	m.Status.Addresses = []clusterv1.MachineAddress{{Type: clusterv1.MachineInternalIP, Address: address}}
	if err := f.c.Status().Update(t.Context(), m); err != nil {
		t.Fatalf("setting the status of %s: %v", m.Name, err)
	}
}

// grow brings the control plane up to replicas machines, provisioning each one as it appears. It
// is the state most tests want to start their own assertions from.
func (f *fixture) grow(t *testing.T, replicas int) {
	t.Helper()

	for i := 0; i < replicas; i++ {
		f.reconcile(t)
		machines := f.machines(t)
		if len(machines) != i+1 {
			t.Fatalf("pass %d created %d machines, want %d", i, len(machines), i+1)
		}
		// Whichever machine has no address yet, rather than the last one in name order: the
		// controller names machines with generateName, so the one this pass created carries a
		// random suffix and sorts anywhere. Indexing by the pass number provisioned an already
		// addressed machine half the time, which left the other with no address and a fleet one
		// host short -- a test that passed or failed on the roll of a suffix.
		fresh := -1
		for j := range machines {
			if len(machines[j].Status.Addresses) == 0 {
				fresh = j
				break
			}
		}
		if fresh < 0 {
			t.Fatalf("pass %d created no machine without an address", i)
		}
		f.provision(t, &machines[fresh], fmt.Sprintf("10.0.0.%d", 11+i))
	}
}

// condition returns one condition from the control plane's status, or fails.
func (f *fixture) condition(t *testing.T, conditionType string) metav1.Condition {
	t.Helper()

	for _, c := range f.cp.Status.Conditions {
		if c.Type == conditionType {
			return c
		}
	}
	t.Fatalf("no %s condition on the control plane; has %v", conditionType, f.cp.Status.Conditions)
	return metav1.Condition{}
}

// setReplicas changes spec.replicas, as an operator scaling the control plane would.
func (f *fixture) setReplicas(t *testing.T, replicas int32) {
	t.Helper()

	f.cp.Spec.Replicas = ptr.To(replicas)
	if err := f.c.Update(t.Context(), f.cp); err != nil {
		t.Fatalf("scaling to %d: %v", replicas, err)
	}
}

// testKey returns a throwaway PEM-encoded ed25519 private key. Generated rather than checked in,
// for the same reason pkg/converge's tests generate theirs.
func testKey(t *testing.T) []byte {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshalling the key: %v", err)
	}
	return pem.EncodeToMemory(block)
}

// errConvergeFailed is what the stub returns when a test asks for a failed apply.
var errConvergeFailed = errors.New("a phase failed on 10.0.0.11")

// setVersion changes spec.version the way an operator would, and reloads what the API server
// stored so the next reconcile reads it.
func (f *fixture) setVersion(t *testing.T, version string) {
	t.Helper()

	f.refresh(t)
	f.cp.Spec.Version = version
	if err := f.c.Update(t.Context(), f.cp); err != nil {
		t.Fatalf("setting spec.version to %s: %v", version, err)
	}
}

// setVersionAndPackage changes both halves of an upgrade: the version asked for, and the package
// that installs it. Only the second reaches the desired-state fingerprint, which is why a test
// that moves one without the other is testing something else.
func (f *fixture) setVersionAndPackage(t *testing.T, version, ociRef string) {
	t.Helper()

	f.refresh(t)
	f.cp.Spec.Version = version
	f.cp.Spec.Distro.OCIRef = ociRef
	if err := f.c.Update(t.Context(), f.cp); err != nil {
		t.Fatalf("setting spec.version to %s and the package to %s: %v", version, ociRef, err)
	}
}

// approveVersionChange sets the annotation that lets a converge follow a version change.
func (f *fixture) approveVersionChange(t *testing.T, version string) {
	t.Helper()

	f.refresh(t)
	if f.cp.Annotations == nil {
		f.cp.Annotations = map[string]string{}
	}
	f.cp.Annotations[controlplanev1.ApproveVersionChangeAnnotation] = version
	if err := f.c.Update(t.Context(), f.cp); err != nil {
		t.Fatalf("approving the change to %s: %v", version, err)
	}
}
