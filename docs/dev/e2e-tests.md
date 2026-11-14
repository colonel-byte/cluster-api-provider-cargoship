# End-to-end tests

The unit tests cover everything that can be covered without an API server: inventory construction from `Machine` objects, the desired-state fingerprint, the converge runner's concurrency, and cloud-init rendering. They need no cluster and no network.

```sh
make test
```

## The reconcile state machine

```sh
make test-envtest
```

This runs both controllers against a real API server - `envtest`, the kubebuilder control plane - with the cargoship apply stubbed out. It is where the provider's own logic is covered: machines created one per pass, the pre-terminate hook claimed at creation and released only after the survivors have converged, the kubeconfig secret published under the name clusterctl looks for, the bootstrap data rendered once and never again, and every state the reconcile waits in.

**What it asserts is which state a pass lands in, never a duration measured on the clock.** This provider's answer to machines and API servers coming up on their own schedules is that nothing anywhere gives up, so there is no timing to assert; what is worth pinning down is that "no address yet", "ssh not answering", "converging" and "converged" each requeue and say why. A test that waited on wall-clock time here would be testing the one thing the design does not do.

Two things it needs, and both are why it is `make test-envtest` rather than `go test`:

- **The control-plane binaries** - a real `kube-apiserver` and `etcd` - which `setup-envtest` fetches. This is the one thing in this repository that reaches the network for a test.
- **Cluster API's own core CRDs**, for the Clusters and Machines it holds. `make envtest-crds` generates them from the vendored types into `out/`, and `test-envtest` passes the directory in as `CAPI_CRD_DIR`. Generating beats copying them out of the `cluster-api` module - whose `config/crd` directory `go mod vendor` drops - because it needs neither a network nor a populated module cache, and because the schemas the tests run against are then the ones the vendored types actually describe. A bare `go test -tags=envtest` fails saying the variable is unset rather than failing inside the API server.

The harness is `internal/envtest`, behind the same build tag, so `make test` stays runnable with nothing installed. CI runs this as its own job in the unit-tests workflow, for the same reason it is a separate target here: the suite is behind a build tag, so the `go test ./...` job proves nothing about it.

The stub is injected through the reconciler's `Converge` field, which `SetupWithManager` fills in with `converge.Apply` when it is nil. That is the only seam added for testing, and it exists because the alternative is a fleet.

## A converge against real hosts

There is no suite for this yet, and the backend to build it on is not the obvious one: [todo-e2e-fleet-backends](../agent/todo-e2e-fleet-backends.md) is the plan and the reasoning.

The short version. **CAPD cannot be used** - its machines are containers running a kubelet and no sshd, and cargoship's first phase is `Connect` - which is worth stating because every other provider's e2e documentation points at CAPD. **Bootloose containers can**: they run sshd, answer on port 22, and attach to the `kind` network the controller pod is already on. **The microVM fleet cannot**, from a pod: its nodes are reachable only as per-node forwarded ports on the host's loopback, and neither a single inventory-wide SSH port nor a `MachineAddress` can express that.

Do not reach for `virsh` to inspect anything a run leaves behind. `virsh -c qemu:///system` goes through polkit and interrupts whoever is at the keyboard to answer a question the run's own log already answers.

## What has no coverage yet

- A converge against real hosts, per the plan above.
- The bootstrap data actually being consumed by a booting machine. Neither container nor microVM fleets read it, so the first thing that proves it is a real infrastructure provider.
