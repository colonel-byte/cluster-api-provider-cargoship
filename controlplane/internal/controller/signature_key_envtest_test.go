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
	"bytes"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
)

// testPublicKey is the shape of a Cosign public key, which is all this has to be: nothing here
// verifies anything, the apply that would is stubbed out.
var testPublicKey = []byte("-----BEGIN PUBLIC KEY-----\naaaa\n-----END PUBLIC KEY-----\n")

// nameSignatureKey points spec.distro at a secret, as an operator turning verification on would.
func nameSignatureKey(t *testing.T, f *fixture, name string) {
	t.Helper()

	f.cp.Spec.Distro.SignatureKeySecretRef = &controlplanev1.SecretReference{Name: name}
	if err := f.c.Update(t.Context(), f.cp); err != nil {
		t.Fatalf("naming the signature key secret: %v", err)
	}
}

// TestReconcileConvergesWhenVerificationIsTurnedOn is the whole reason the key is a fingerprint
// input. A fleet that already matches its last converge would otherwise skip the apply, so the
// key would be read, reported as fine, and never used to verify anything.
func TestReconcileConvergesWhenVerificationIsTurnedOn(t *testing.T) {
	f := newFixture(t)
	f.grow(t, 1)

	f.reconcile(t)
	f.waitForConverge(t)
	f.reconcile(t)
	if f.converge.count() != 1 {
		t.Fatalf("%d converges to reach the desired state, want 1", f.converge.count())
	}
	if key := f.converge.options(t).SignatureKey; len(key) != 0 {
		t.Errorf("converged with a signature key %q before one was named", key)
	}

	if err := f.c.Create(t.Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cosign-pubkey", Namespace: f.ns},
		Data:       map[string][]byte{signatureKeyField: testPublicKey},
	}); err != nil {
		t.Fatalf("creating the signature key secret: %v", err)
	}
	nameSignatureKey(t, f, "cosign-pubkey")

	f.reconcile(t)
	f.waitForConverge(t)
	if f.converge.count() != 2 {
		t.Fatalf("%d converges after naming a key, want 2", f.converge.count())
	}
	if key := f.converge.options(t).SignatureKey; !bytes.Equal(key, testPublicKey) {
		t.Errorf("converged with signature key %q, want the one in the secret", key)
	}
}

// TestReconcileRefusesAnUnreadableSignatureKey covers the reason the secret is read in the
// reconcile rather than inside the converge: a missing one is a condition on the resource now,
// not a failed apply several minutes in.
func TestReconcileRefusesAnUnreadableSignatureKey(t *testing.T) {
	tests := []struct {
		name   string
		secret *corev1.Secret
		want   string
	}{
		{
			name:   "the secret does not exist",
			secret: nil,
			want:   "unable to read the signature key secret",
		},
		{
			name: "the secret has no cosign.pub",
			secret: &corev1.Secret{
				Data: map[string][]byte{"tls.crt": testPublicKey},
			},
			want: `has no "cosign.pub" field`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.grow(t, 1)

			if tt.secret != nil {
				tt.secret.Name = "cosign-pubkey"
				tt.secret.Namespace = f.ns
				if err := f.c.Create(t.Context(), tt.secret); err != nil {
					t.Fatalf("creating the signature key secret: %v", err)
				}
			}
			nameSignatureKey(t, f, "cosign-pubkey")

			// Not f.reconcile, which fails the test on the error this one is about.
			_, err := f.r.Reconcile(t.Context(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: f.ns, Name: f.cp.Name},
			})
			if err == nil {
				t.Fatal("Reconcile() succeeded, want the unusable signature key as an error")
			}
			f.refresh(t)

			if f.converge.count() != 0 {
				t.Error("converged with a signature key the controller could not read")
			}
			got := f.condition(t, controlplanev1.ConvergedCondition)
			if got.Reason != "SignatureKeyUnusable" {
				t.Errorf("Converged reason = %q, want SignatureKeyUnusable", got.Reason)
			}
			if !strings.Contains(got.Message, tt.want) {
				t.Errorf("Converged message = %q, want it to mention %q", got.Message, tt.want)
			}
		})
	}
}
