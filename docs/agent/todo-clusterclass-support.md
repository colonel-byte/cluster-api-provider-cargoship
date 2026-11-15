# ClusterClass support: what landed, and what is left

The kind a `ClusterClass` needs now exists. `CargoshipControlPlaneTemplate` is in `controlplane/api/v1alpha1/cargoshipcontrolplanetemplate_types.go`, with a CRD, a scheme registration, the field-coverage test that keeps it in step with the live spec, and an envtest that a real `ClusterClass` referencing it validates. [`choice-clusterclass-template.md`](choice-clusterclass-template.md) records why its spec is a separate type, and [`docs/guides/clusterclass.md`](../guides/clusterclass.md) is the operator-facing half.

This is what the original plan got wrong, and what is still outstanding.

## Corrections to the original plan

- **`spec.controlPlane.ref` is the v1beta1 spelling.** The pinned `api/core/v1beta2` is `spec.controlPlane.templateRef`, of type `ClusterClassTemplateReference{kind, name, apiVersion}` - no namespace, so a template lives in its class's namespace.
- **The template spec is not the live spec**, which was the plan's central assumption. `version`, `replicas` and `machineTemplate.spec.infrastructureRef` are the topology controller's to fill in and are required or defaulted on the live type. Measured against `kubeadmcontrolplanetemplates` in `sigs.k8s.io/cluster-api@v1.14.4`, which drops exactly those three and requires nothing.
- **No RBAC was needed.** Nothing in this repository reads a template; the topology controller runs in the core manager with its own permissions.
- **The contract label needed nothing either.** The kustomize `labels` block in `controlplane/config/default/kustomization.yaml` applies `cluster.x-k8s.io/v1beta2: v1alpha1` to everything under `../crd`, so a new CRD inherits it. The explicit resource list in `controlplane/config/crd/kustomization.yaml` did need the new line.
- **envtest needed nothing.** `internal/envtest` loads the `crd/bases` directory rather than a file list, so a new CRD is picked up. `make envtest-crds` only generates the Cluster API CRDs, and `ClusterClass` is already among them.
- The plan cited line numbers for `spec.controlPlaneEndpoint` and the scale subresource. Both moved within a day. Cite symbols.

## What a live topology confirmed

Run against a k3d cluster with core Cluster API 1.14.4, `ClusterTopology=true` and the Docker provider standing in for infrastructure. A `Cluster` with a topology stamped a `CargoshipControlPlane` carrying:

| Stamped field                            | Value                                                | From                                  |
| ---------------------------------------- | ---------------------------------------------------- | ------------------------------------- |
| `spec.version`                           | `v1.34.1`                                            | `spec.topology.version`               |
| `spec.replicas`                          | `3`                                                  | `spec.topology.controlPlane.replicas` |
| `spec.machineTemplate.spec.infrastructureRef` | the generated `DockerMachineTemplate`           | the class's `machineInfrastructure`   |
| `spec.distro`, `spec.ssh`, `spec.apply`  | as written                                           | the template                          |
| the approval annotation                  | `approve-version-change: v1.34.1`                    | `spec.topology.controlPlane.metadata` |

So all three omitted fields are filled in, which is the whole argument for the template spec being its own type, and the annotation route for approving an upgrade works as documented. The existing reconciler then took the stamped object and reported `Available=False WaitingForInfrastructure`, which is correct: the Docker provider cannot provision a cluster in that setup, and no control-plane machine is created until the cluster infrastructure is.

It also settled the profile question measured rather than inferred. The generated `MachineDeployment` is `fleet-one-worker-td5v8`, and its `spec.template.metadata.labels` hold `topology.cluster.x-k8s.io/deployment-name: worker` with **no** `cluster.x-k8s.io/deployment-name` at all - so the label the converge used to read is absent, not merely unhelpful, and every class-based pool would have selected no profile.

## Still to do

- **One end to end against hosts that answer SSH.** Everything above stops at the infrastructure provider: nothing has converged a fleet from a class. The Docker provider cannot, for the reason [`docs/dev/e2e-tests.md`](../dev/e2e-tests.md) gives, so this wants the microvm fleet or real machines.
- **The patches are rendered but not applied.** `clusterctl generate cluster --from` substitutes the variables in [`examples/cluster-template-clusterclass-vsphere.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/examples/cluster-template-clusterclass-vsphere.yaml) and the output is valid, but no topology has run the endpoint or package-version patch, so the paths they write to are checked against the CRD schema and nothing else.
- **Whether the topology controller reads any control-plane contract field this provider does not set.** Nothing complained for a three-replica class with a machine infrastructure template, which covers the fields the contract lists; a class with `rollout` or a `healthCheck` is unexercised.

## Not this repository's to fix

- `ClusterClass` needs the `CLUSTER_TOPOLOGY` feature gate, which is off by default.
- Adopting a hand-wired cluster into a topology is a manual migration, and so is leaving one.
