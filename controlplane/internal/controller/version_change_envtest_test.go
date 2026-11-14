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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
)

// A spec.version change against a converged control plane is the half-applied upgrade
// choice-no-rollout.md exists to refuse, and until this it was refused only in the documentation:
// nothing in the API or the reconcile stopped it, and a converge would have walked every host
// through cargoship's upgrade phases unsupervised.
func TestReconcileRefusesAnUnapprovedVersionChange(t *testing.T) {
	f := newFixture(t)
	f.grow(t, 1)

	f.reconcile(t)
	f.waitForConverge(t)
	f.reconcile(t)
	if f.converge.count() != 1 {
		t.Fatalf("%d converges to reach the desired state, want 1", f.converge.count())
	}

	// A version and the package that installs it, which is what an upgrade is.
	f.setVersionAndPackage(t, "v1.35.4", "oci://registry.test/k3s:1.35.4")

	result := f.reconcile(t)
	if f.converge.count() != 1 {
		t.Errorf("an unapproved version change started converge %d, want none", f.converge.count())
	}
	got := f.condition(t, controlplanev1.ConvergedCondition)
	if got.Status != metav1.ConditionFalse || got.Reason != "VersionChangeRefused" {
		t.Fatalf("Converged = %s/%s, want False/VersionChangeRefused", got.Status, got.Reason)
	}
	// The message is the whole interface here: it is where an operator finds out what to do.
	for _, want := range []string{"v1.34.1", "v1.35.4", controlplanev1.ApproveVersionChangeAnnotation} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("Converged message = %q, want it to name %q", got.Message, want)
		}
	}
	// A refusal is a decision waiting on a person, so it requeues at the drift interval rather
	// than backing off like a failure.
	if result.RequeueAfter != defaultDriftInterval {
		t.Errorf("requeued after %s, want the drift interval %s", result.RequeueAfter, defaultDriftInterval)
	}
	// And the counts stop claiming the fleet is current while it is not.
	if ptr.Deref(f.cp.Status.UpToDateReplicas, -1) != 0 {
		t.Errorf("status.upToDateReplicas = %v, want 0 while a version change is unapproved",
			ptr.Deref(f.cp.Status.UpToDateReplicas, -1))
	}

	// Approved for the version actually being asked for, the converge runs.
	f.approveVersionChange(t, "v1.35.4")
	f.reconcile(t)
	if f.converge.count() != 2 {
		t.Fatalf("%d converges after approving the change, want 2", f.converge.count())
	}
	f.waitForConverge(t)
	f.reconcile(t)
	if got := f.condition(t, controlplanev1.ConvergedCondition); got.Status != metav1.ConditionTrue {
		t.Errorf("Converged = %s/%s after the approved change, want True", got.Status, got.Reason)
	}
	if f.cp.Status.Version != "v1.35.4" {
		t.Errorf("status.version = %q, want v1.35.4", f.cp.Status.Version)
	}
	if ptr.Deref(f.cp.Status.UpToDateReplicas, -1) != 1 {
		t.Errorf("status.upToDateReplicas = %v, want 1 once the fleet matches",
			ptr.Deref(f.cp.Status.UpToDateReplicas, -1))
	}
}

// A version bumped on its own installs nothing: the fleet runs whatever the package holds, and
// the desired-state fingerprint does not carry spec.version at all. Approving such a change would
// not make it take effect, so it gets an answer that says what is actually missing.
func TestReconcileReportsAVersionChangeWithNoPackageBehindIt(t *testing.T) {
	f := newFixture(t)
	f.grow(t, 1)

	f.reconcile(t)
	f.waitForConverge(t)
	f.reconcile(t)

	f.setVersion(t, "v1.35.4")
	result := f.reconcile(t)

	if f.converge.count() != 1 {
		t.Errorf("a version change with no new package started converge %d, want none", f.converge.count())
	}
	got := f.condition(t, controlplanev1.ConvergedCondition)
	if got.Status != metav1.ConditionFalse || got.Reason != "VersionChangeInert" {
		t.Fatalf("Converged = %s/%s, want False/VersionChangeInert", got.Status, got.Reason)
	}
	if !strings.Contains(got.Message, "spec.distro") {
		t.Errorf("Converged message = %q, want it to name the field to change", got.Message)
	}
	if result.RequeueAfter != defaultDriftInterval {
		t.Errorf("requeued after %s, want the drift interval %s", result.RequeueAfter, defaultDriftInterval)
	}

	// Approving it changes nothing, because there is nothing to approve: the answer is still that
	// the package has to move.
	f.approveVersionChange(t, "v1.35.4")
	f.reconcile(t)
	if f.converge.count() != 1 {
		t.Errorf("approving an inert change started converge %d, want none", f.converge.count())
	}
	if got := f.condition(t, controlplanev1.ConvergedCondition); got.Reason != "VersionChangeInert" {
		t.Errorf("Converged reason = %q, want VersionChangeInert", got.Reason)
	}
}

// An annotation approving the version a cluster already runs must not wave through the next
// change, which is why the value is compared rather than its presence checked.
func TestReconcileRefusesTheNextChangeAfterAnApprovedOne(t *testing.T) {
	f := newFixture(t)
	f.grow(t, 1)

	f.reconcile(t)
	f.waitForConverge(t)
	f.reconcile(t)

	f.setVersionAndPackage(t, "v1.35.4", "oci://registry.test/k3s:1.35.4")
	f.approveVersionChange(t, "v1.35.4")
	f.reconcile(t)
	f.waitForConverge(t)
	f.reconcile(t)
	if f.converge.count() != 2 {
		t.Fatalf("%d converges after the approved change, want 2", f.converge.count())
	}

	// The annotation is still there, naming the version now installed.
	f.setVersionAndPackage(t, "v1.36.1", "oci://registry.test/k3s:1.36.1")
	f.reconcile(t)

	if f.converge.count() != 2 {
		t.Errorf("a stale approval started converge %d, want none", f.converge.count())
	}
	if got := f.condition(t, controlplanev1.ConvergedCondition); got.Reason != "VersionChangeRefused" {
		t.Errorf("Converged reason = %q, want VersionChangeRefused", got.Reason)
	}
}
