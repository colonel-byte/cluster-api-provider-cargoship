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

package converge

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

const testKeyName = "default/fleet"

// waitFor polls until cond is true, or fails the test. Used in place of a sleep so a slow machine
// does not turn a correct runner into a flake.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestRunnerStartIsNotBlocking is the property the reconcile depends on: starting a converge must
// return at once, however long the apply takes.
func TestRunnerStartIsNotBlocking(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	release := make(chan struct{})

	run := r.Start(context.Background(), testKeyName, "fp-1", func(_ context.Context) (*Run, error) {
		<-release
		return &Run{}, nil
	})
	if run.State != StateRunning {
		t.Fatalf("Start returned state %q, want %q", run.State, StateRunning)
	}

	// Observing it must not block either, and must still say running.
	if got := r.Observe(testKeyName); got == nil || got.State != StateRunning {
		t.Fatalf("Observe = %+v, want a running run", got)
	}

	close(release)
	if err := r.Wait(context.Background(), testKeyName); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got := r.Observe(testKeyName); got.State != StateSucceeded {
		t.Errorf("after finishing, state = %q, want %q", got.State, StateSucceeded)
	}
}

// TestRunnerStartsOnlyOnePerKey is the cost control: a reconcile loop that fires five times while
// an apply is in flight must not start five SSH walks over the same fleet.
func TestRunnerStartsOnlyOnePerKey(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	var starts atomic.Int32
	release := make(chan struct{})

	apply := func(_ context.Context) (*Run, error) {
		starts.Add(1)
		<-release
		return &Run{}, nil
	}

	for range 5 {
		r.Start(context.Background(), testKeyName, "fp-1", apply)
	}

	close(release)
	if err := r.Wait(context.Background(), testKeyName); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got := starts.Load(); got != 1 {
		t.Errorf("the apply ran %d times, want 1", got)
	}
}

// TestRunnerCancelsOnFingerprintChange covers a fleet that changes mid-converge: a machine
// finishing provisioning while an apply is walking the others. The in-flight run must be
// cancelled rather than left to finish converging to a host set that is no longer wanted.
func TestRunnerCancelsOnFingerprintChange(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	cancelled := make(chan struct{})

	r.Start(context.Background(), testKeyName, "fp-1", func(ctx context.Context) (*Run, error) {
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	})

	// The desired state moved. Start must cancel, and must still return without blocking.
	r.Start(context.Background(), testKeyName, "fp-2", func(_ context.Context) (*Run, error) {
		t.Error("the replacement apply started in the same reconcile; it should wait for the next one")
		return &Run{}, nil
	})

	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the in-flight converge was not cancelled when the fingerprint changed")
	}
}

// TestRunnerSurvivesTheReconcileContext is the subtle one. controller-runtime cancels the context
// it hands Reconcile as soon as Reconcile returns, which is immediately -- so a converge derived
// from it would be killed before it finished its first phase.
func TestRunnerSurvivesTheReconcileContext(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	reconcileCtx, cancelReconcile := context.WithCancel(context.Background())

	started := make(chan struct{})
	release := make(chan struct{})
	r.Start(reconcileCtx, testKeyName, "fp-1", func(ctx context.Context) (*Run, error) {
		close(started)
		select {
		case <-release:
			return &Run{}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	<-started
	cancelReconcile()

	// Give the cancellation every chance to propagate before declaring success.
	time.Sleep(20 * time.Millisecond)
	if got := r.Observe(testKeyName); got.State != StateRunning {
		t.Fatalf("the converge stopped when the reconcile returned: state = %q, err = %v", got.State, got.Err)
	}

	close(release)
	if err := r.Wait(context.Background(), testKeyName); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got := r.Observe(testKeyName); got.State != StateSucceeded {
		t.Errorf("state = %q, want %q", got.State, StateSucceeded)
	}
}

func TestRunnerRecordsFailure(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	want := errors.New("the leader refused the join")

	r.Start(context.Background(), testKeyName, "fp-1", func(_ context.Context) (*Run, error) {
		// A failed apply still returns cargoship's own result, and the runner must keep it: it
		// is what says which phases ran before the failure.
		return &Run{KubeConfig: nil}, want
	})
	if err := r.Wait(context.Background(), testKeyName); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	got := r.Observe(testKeyName)
	if got.State != StateFailed {
		t.Errorf("state = %q, want %q", got.State, StateFailed)
	}
	if !errors.Is(got.Err, want) {
		t.Errorf("err = %v, want %v", got.Err, want)
	}
}

// TestRunnerDoesNotRerunAFinishedConverge keeps a succeeded run from being re-run by the next
// reconcile that happens to pass the same fingerprint before status has been written.
func TestRunnerDoesNotRerunAFinishedConverge(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	var starts atomic.Int32
	apply := func(_ context.Context) (*Run, error) {
		starts.Add(1)
		return &Run{}, nil
	}

	r.Start(context.Background(), testKeyName, "fp-1", apply)
	if err := r.Wait(context.Background(), testKeyName); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	waitFor(t, "the first converge to finish", func() bool {
		return r.Observe(testKeyName).State == StateSucceeded
	})

	r.Start(context.Background(), testKeyName, "fp-1", apply)
	if got := starts.Load(); got != 1 {
		t.Errorf("the apply ran %d times for one fingerprint, want 1", got)
	}

	// Forget is how a caller asks for it again, and it must refuse to drop a running one.
	if err := r.Forget(testKeyName); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	r.Start(context.Background(), testKeyName, "fp-1", apply)
	if err := r.Wait(context.Background(), testKeyName); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got := starts.Load(); got != 2 {
		t.Errorf("after Forget the apply ran %d times, want 2", got)
	}
}

func TestRunnerForgetRefusesARunningConverge(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	release := make(chan struct{})
	r.Start(context.Background(), testKeyName, "fp-1", func(_ context.Context) (*Run, error) {
		<-release
		return &Run{}, nil
	})

	if err := r.Forget(testKeyName); err == nil {
		t.Error("Forget dropped a running converge, leaving an apply touching hosts with nothing watching it")
	}
	close(release)
}
