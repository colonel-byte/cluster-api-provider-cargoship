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

## Still to do

- **A `cluster-template.yaml` flavor.** `clusterctl generate cluster --flavor` reads a template from the release assets, and a class-based cluster is the flavor worth shipping: an operator gets the class, the templates and a `Cluster` with a topology from one command. It belongs in `.goreleaser.yaml`'s `extra_files` beside the three files clusterctl already fetches.
- **One `clusterctl generate cluster` end to end**, against a class composed with a real infrastructure provider, before any of this is documented as working. The guide's YAML is checked against the CRD schemas and the vendored types, not against a cluster that came up.
- **Variables and patches are documented but unexercised.** The control-plane-endpoint patch in the guide is the shape a class wants; nothing has run it. Leave the class itself without variables until a second cluster shape needs them - a class with one consumer is indirection with nothing on the other side of it.
- **Whether the topology controller reads any control-plane contract field this provider does not set.** The table in the original plan was read off this provider's types. `vendor/sigs.k8s.io/cluster-api/` holds only `api` and `util`, so the contract package is not vendored to read - the module cache has the full tree, and `clusterctl generate cluster` against a live topology is the real check.

## Not this repository's to fix

- `ClusterClass` needs the `CLUSTER_TOPOLOGY` feature gate, which is off by default.
- Adopting a hand-wired cluster into a topology is a manual migration, and so is leaving one.
