# Unit tests

Source: [`unit-tests.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/unit-tests.yaml). Runs on every pull request and in the merge queue, with in-progress runs cancelled per ref.

Two jobs, kept apart on purpose.

`unit` runs `go test -mod=vendor -race ./...` with `GOPROXY=off`, so an accidental network fetch fails rather than quietly succeeding. The race detector is not decoration: the only concurrency in this repository is the converge runner, and what it has to get right - a goroutine outliving the reconcile that started it, and being cancelled when the desired state changes - is exactly what the detector catches.

`envtest` runs `make test-envtest`, which drives both controllers against a real kube-apiserver and etcd with the cargoship apply stubbed out. It is a second job rather than more steps on the first because it downloads the kubebuilder control plane, and that is the only thing in this repository's CI that reaches the network for a test. Keeping it separate means the offline guarantee above still means something, and a failure here is immediately distinguishable from a unit failure.

Both jobs exit successfully on a tree with no Go packages, which is the state of the bottom of a stack that vendors its dependencies before any code is written.

**What it does not cover:** a converge against real hosts. The envtest suite stubs the apply out, so no SSH connection is made and no cargoship phase runs. See [e2e-tests](../dev/e2e-tests.md).
