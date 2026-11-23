# Install smoke test

Source: [`install-smoke.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/install-smoke.yaml). Runs on every pull request and in the merge queue, with a 20 minute timeout.

The only check in the repository that exercises the Cluster API contract rather than the Go code, and the slowest at around four minutes. It builds both controller images through `make docker-build`, creates a kind cluster, loads the images, renders the component manifests, lays them out as a local provider repository, and runs `clusterctl init` against them with `--infrastructure docker`.

Everything it catches is silent:

| Failure                                               | What it looks like without this check                                     |
| ----------------------------------------------------- | ------------------------------------------------------------------------- |
| A CRD missing its `cluster.x-k8s.io/<contract>` label | The provider installs, the controllers start, and nothing ever reconciles |
| A `metadata.yaml` that does not name the contract     | `clusterctl init` refuses outright                                        |
| RBAC one verb short                                   | A failure at the first reconcile, in a log nobody is reading yet          |

So the assertions are the point: all four CRDs reach `Established` and each carries `cluster.x-k8s.io/v1beta2: v1alpha1`, both deployments go `Available`, and the control-plane manager's log is checked for the two startup failures that are fatal but still report `Available` briefly - an unusable cache path, and never reaching its start line. On failure it dumps pods, deployments and logs.

The provider version is read out of the rendered `metadata.yaml` rather than written into the workflow, because `clusterctl` refuses a version outside the release series `metadata.yaml` declares and release-please bumps that series on every release.

**What it does not cover:** a converge. No machine is ever created, and CAPD's machines are containers running a kubelet with no sshd, which is cargoship's first phase. See [e2e-tests](../dev/e2e-tests.md).
