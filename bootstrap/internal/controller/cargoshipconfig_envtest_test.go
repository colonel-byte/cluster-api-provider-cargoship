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

// The bootstrap reconcile, end to end against a real API server.
//
// There is not much to assert, and that is the design rather than an unfinished suite: all this
// provider's bootstrap data does is authorize the controller's key and whatever files an operator
// asked for. What the tests are really here for is the write-once rule -- the data is user-data a
// running machine has already consumed, so a second render would be a change no machine ever sees.
package controller

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	bootstrapv1 "github.com/colonel-byte/cluster-api-provider-cargoship/bootstrap/api/v1alpha1"
	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
	"github.com/colonel-byte/cluster-api-provider-cargoship/internal/envtest"
	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/sshkey"
)

func TestMain(m *testing.M) {
	os.Exit(envtest.Run(m))
}

// fixture is a cluster with a control plane, one machine, and that machine's config.
type fixture struct {
	c      client.Client
	ns     string
	config *bootstrapv1.CargoshipConfig
	r      *CargoshipConfigReconciler
	// authorizedKey is the line the controller's key must appear as in the rendered data.
	authorizedKey string
}

// newFixture builds that, with the config owned by the machine -- which is the state the MachineSet
// or the control plane leaves behind, and the one the reconcile needs before it renders anything.
func newFixture(t *testing.T, spec bootstrapv1.CargoshipConfigSpec) *fixture {
	t.Helper()

	c := envtest.Client(t)
	ns := envtest.Namespace(t, c)
	ctx := t.Context()

	key := testKey(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "fleet-ssh", Namespace: ns},
		Type:       corev1.SecretTypeSSHAuth,
		Data:       map[string][]byte{corev1.SSHAuthPrivateKey: key},
	}
	if err := c.Create(ctx, secret); err != nil {
		t.Fatalf("creating the ssh secret: %v", err)
	}
	authorizedKey, err := sshkey.AuthorizedKey(secret)
	if err != nil {
		t.Fatalf("deriving the public key: %v", err)
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
			Distro: controlplanev1.DistroSource{OCIRef: "oci://registry.test/k3s:v1.34.1"},
			SSH: controlplanev1.SSHConfig{
				User:                "rocky",
				PrivateKeySecretRef: controlplanev1.LocalObjectReference{Name: "fleet-ssh"},
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

	config := &bootstrapv1.CargoshipConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "fleet-cp-0", Namespace: ns},
		Spec:       spec,
	}
	if err := c.Create(ctx, config); err != nil {
		t.Fatalf("creating the config: %v", err)
	}

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fleet-cp-0",
			Namespace: ns,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: cluster.Name},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					APIGroup: bootstrapv1.GroupVersion.Group,
					Kind:     "CargoshipConfig",
					Name:     config.Name,
				},
			},
			InfrastructureRef: cp.Spec.MachineTemplate.Spec.InfrastructureRef,
		},
	}
	if err := c.Create(ctx, machine); err != nil {
		t.Fatalf("creating the machine: %v", err)
	}

	return &fixture{c: c, ns: ns, config: config, authorizedKey: authorizedKey,
		r: &CargoshipConfigReconciler{Client: c}}
}

// own sets the machine as the config's owner, which Cluster API does in a separate write.
func (f *fixture) own(t *testing.T) {
	t.Helper()

	machine := &clusterv1.Machine{}
	key := types.NamespacedName{Namespace: f.ns, Name: "fleet-cp-0"}
	if err := f.c.Get(t.Context(), key, machine); err != nil {
		t.Fatalf("reading the machine: %v", err)
	}

	f.config.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Machine",
		Name:       machine.Name,
		UID:        machine.UID,
	}}
	if err := f.c.Update(t.Context(), f.config); err != nil {
		t.Fatalf("setting the owner reference: %v", err)
	}
}

// reconcile runs one pass and reloads the config.
func (f *fixture) reconcile(t *testing.T) ctrl.Result {
	t.Helper()

	result, err := f.r.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(f.config),
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if err := f.c.Get(t.Context(), client.ObjectKeyFromObject(f.config), f.config); err != nil {
		t.Fatalf("reloading the config: %v", err)
	}
	return result
}

// data is the rendered bootstrap data.
func (f *fixture) data(t *testing.T) string {
	t.Helper()

	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: f.ns, Name: f.config.Status.DataSecretName}
	if err := f.c.Get(t.Context(), key, secret); err != nil {
		t.Fatalf("reading the bootstrap secret %s: %v", key, err)
	}
	return string(secret.Data["value"])
}

func TestConfigWaitsForTheOwningMachine(t *testing.T) {
	f := newFixture(t, bootstrapv1.CargoshipConfigSpec{})

	result := f.reconcile(t)
	if result.RequeueAfter != waitForOwner {
		t.Errorf("requeued after %s, want %s", result.RequeueAfter, waitForOwner)
	}
	if f.config.Status.DataSecretName != "" {
		t.Error("rendered bootstrap data before the owning Machine was known")
	}
}

func TestConfigWritesTheBootstrapSecret(t *testing.T) {
	f := newFixture(t, bootstrapv1.CargoshipConfigSpec{
		Files: []bootstrapv1.File{{
			Path:        "/etc/motd",
			Permissions: "0644",
			Content:     "converged by cargoship\n",
		}},
	})
	f.own(t)

	f.reconcile(t)

	if f.config.Status.DataSecretName != f.config.Name {
		t.Fatalf("status.dataSecretName = %q, want %q", f.config.Status.DataSecretName, f.config.Name)
	}
	if !ptr.Deref(f.config.Status.Initialization.DataSecretCreated, false) {
		t.Error("status.initialization.dataSecretCreated is not set, so the Machine controller keeps waiting")
	}
	if !controllerutil.ContainsFinalizer(f.config, bootstrapv1.ConfigFinalizer) {
		t.Error("no finalizer, so the secret's owner reference is the only thing keeping it")
	}

	data := f.data(t)
	// The key is derived from the control plane's own secret rather than carried separately: a
	// mismatched pair boots a fleet that reports ready and refuses every connection.
	if !strings.Contains(data, f.authorizedKey) {
		t.Errorf("the rendered data does not authorize the controller's key:\n%s", data)
	}
	if !strings.Contains(data, "/etc/motd") {
		t.Errorf("the rendered data is missing the requested file:\n%s", data)
	}
	if !strings.Contains(data, "rocky") {
		t.Errorf("the rendered data does not name the control plane's ssh user:\n%s", data)
	}
}

func TestConfigDoesNotRenderTwice(t *testing.T) {
	f := newFixture(t, bootstrapv1.CargoshipConfigSpec{})
	f.own(t)
	f.reconcile(t)

	// What a second render would overwrite. On most infrastructure providers this is user-data a
	// running machine consumed at boot, so rewriting it changes nothing on the machine while
	// making every reconcile look like a change.
	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: f.ns, Name: f.config.Status.DataSecretName}
	if err := f.c.Get(t.Context(), key, secret); err != nil {
		t.Fatalf("reading the bootstrap secret: %v", err)
	}
	secret.Data["value"] = []byte("#cloud-config\n# edited\n")
	if err := f.c.Update(t.Context(), secret); err != nil {
		t.Fatalf("editing the bootstrap secret: %v", err)
	}

	f.config.Spec.SSHAuthorizedKeys = []string{"ssh-ed25519 AAAAsomethingelse operator@laptop"}
	if err := f.c.Update(t.Context(), f.config); err != nil {
		t.Fatalf("changing the config: %v", err)
	}

	f.reconcile(t)
	if got := f.data(t); got != "#cloud-config\n# edited\n" {
		t.Errorf("the bootstrap data was re-rendered after the machine had consumed it:\n%s", got)
	}
}

func TestConfigResolvesContentFromASecret(t *testing.T) {
	f := newFixture(t, bootstrapv1.CargoshipConfigSpec{
		Files: []bootstrapv1.File{{
			Path: "/etc/pki/ca-trust/source/anchors/registry.crt",
			ContentFrom: &bootstrapv1.FileSource{
				Secret: bootstrapv1.SecretKeySelector{Name: "registry-ca", Key: "ca.crt"},
			},
		}},
	})
	f.own(t)

	if err := f.c.Create(t.Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "registry-ca", Namespace: f.ns},
		Data:       map[string][]byte{"ca.crt": []byte("-----BEGIN CERTIFICATE-----\n")},
	}); err != nil {
		t.Fatalf("creating the referenced secret: %v", err)
	}

	f.reconcile(t)

	if got := f.data(t); !strings.Contains(got, "BEGIN CERTIFICATE") {
		t.Errorf("the referenced secret's content is not in the rendered data:\n%s", got)
	}
}

// testKey returns a throwaway PEM-encoded ed25519 private key.
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
