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
	"fmt"
	"sync"

	"github.com/colonel-byte/cargoship/pkg/phase"
)

// State is where a converge has got to.
type State string

const (
	// StateRunning means the apply is in flight.
	StateRunning State = "Running"
	// StateSucceeded means the apply returned without error.
	StateSucceeded State = "Succeeded"
	// StateFailed means the apply returned an error, or was cancelled.
	StateFailed State = "Failed"
)

// Run is one converge, started by a reconcile and observed by the ones that follow.
type Run struct {
	// Fingerprint is the desired state this run is converging to.
	Fingerprint string
	// State is where it has got to.
	State State
	// Err is why it failed, when it did.
	Err error
	// Result is cargoship's own account of the run: which phases ran, which hosts each named,
	// and what changed. It is what the controller writes status from.
	Result phase.RunResult
	// KubeConfig is the admin credentials the apply built, when it was asked for them.
	KubeConfig []byte
}

// ApplyFunc is the work one converge does. It is a function so that this package does not have to
// know how a package is loaded or which action options a caller wants -- the controller supplies
// that, and this file only ever concerns itself with running it once at a time.
//
// It must be safe to cancel through the context: the Runner cancels an in-flight converge when the
// desired state changes underneath it.
type ApplyFunc func(ctx context.Context) (*Run, error)

// Runner holds at most one in-flight converge per key, and nothing else.
//
// The reason it exists is the one thing about this provider that cannot be made to look like a
// normal reconcile. A cargoship apply is a single blocking call that walks every host over SSH,
// takes minutes, and runs its own retry loops inside each phase; it cannot be suspended and
// resumed, and rewriting it so it could would mean rewriting cargoship. So the reconcile never
// calls it directly. It starts a converge, returns, and the reconciles that follow observe the
// one that is running.
//
// Everything else the controller waits for -- a machine getting an address, SSH beginning to
// answer, the API server beginning to answer -- is an ordinary requeue, and none of it has a
// timer.
type Runner struct {
	mu   sync.Mutex
	runs map[string]*entry
}

type entry struct {
	run    *Run
	cancel context.CancelFunc
	done   chan struct{}
}

// NewRunner returns an empty Runner.
func NewRunner() *Runner {
	return &Runner{runs: map[string]*entry{}}
}

// Observe returns the current converge for key, or nil if there is none.
//
// The returned Run is a copy, so a caller can read it without racing the goroutine that is
// updating it.
func (r *Runner) Observe(key string) *Run {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.runs[key]
	if !ok {
		return nil
	}
	snapshot := *e.run
	return &snapshot
}

// Start begins a converge for key, unless one is already in flight.
//
// A converge already running for the same fingerprint is left alone and reported as it stands. One
// running for a different fingerprint is cancelled, and this call reports StateRunning for the
// cancelled run rather than starting the new one immediately -- the next reconcile starts it, once
// the old goroutine has actually stopped. Waiting for the cancellation here would block the
// reconcile, which is the one thing this type exists to avoid.
func (r *Runner) Start(ctx context.Context, key, fingerprint string, apply ApplyFunc) *Run {
	r.mu.Lock()
	defer r.mu.Unlock()

	if e, ok := r.runs[key]; ok {
		if e.run.State == StateRunning {
			if e.run.Fingerprint != fingerprint {
				e.cancel()
			}
			snapshot := *e.run
			return &snapshot
		}
		// A finished run for the fingerprint being asked for is the answer, not a reason to run
		// again. A finished run for any other fingerprint is stale and replaced below.
		if e.run.Fingerprint == fingerprint {
			snapshot := *e.run
			return &snapshot
		}
	}

	// Deliberately not derived from the reconcile's context: that one is cancelled when Reconcile
	// returns, which is immediately. The converge outlives the reconcile that started it, and is
	// stopped either by Cancel or by the manager shutting down.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	e := &entry{
		run:    &Run{Fingerprint: fingerprint, State: StateRunning},
		cancel: cancel,
		done:   make(chan struct{}),
	}
	r.runs[key] = e

	go func() {
		defer close(e.done)
		defer cancel()

		result, err := apply(runCtx)

		r.mu.Lock()
		defer r.mu.Unlock()

		// Only publish if this entry is still the current one. A Cancel-and-replace that
		// happened while the apply was returning must not have its result overwritten by the
		// run it replaced.
		if r.runs[key] != e {
			return
		}
		if result != nil {
			e.run.Result = result.Result
			e.run.KubeConfig = result.KubeConfig
		}
		if err != nil {
			e.run.State = StateFailed
			e.run.Err = err
			return
		}
		e.run.State = StateSucceeded
	}()

	snapshot := *e.run
	return &snapshot
}

// Cancel stops the converge for key, if there is one, and forgets it.
//
// It does not wait. A cancelled apply unwinds through whichever phase it was in, which can take as
// long as one SSH command, and the caller is a reconcile that should not be holding anything open.
func (r *Runner) Cancel(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.runs[key]
	if !ok {
		return
	}
	e.cancel()
	delete(r.runs, key)
}

// Forget drops a finished converge for key, so the next Start runs again.
//
// It refuses to drop a running one, because that would leak the goroutine's eventual result into
// nothing while the apply carried on touching hosts. Cancel is the way to stop one.
func (r *Runner) Forget(key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.runs[key]
	if !ok {
		return nil
	}
	if e.run.State == StateRunning {
		return fmt.Errorf("converge for %s is still running", key)
	}
	delete(r.runs, key)
	return nil
}

// Wait blocks until the converge for key has finished, for tests and for a graceful shutdown.
func (r *Runner) Wait(ctx context.Context, key string) error {
	r.mu.Lock()
	e, ok := r.runs[key]
	r.mu.Unlock()

	if !ok {
		return nil
	}
	select {
	case <-e.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
