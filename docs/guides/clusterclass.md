# Composing a ClusterClass

A `ClusterClass` is one object per cluster *shape*, and a `Cluster` with a `spec.topology` block per cluster. Without one an operator wires four objects together by hand and keeps the cross-references straight themselves - a `CargoshipControlPlane`, a `CargoshipConfigTemplate`, an infrastructure cluster and one machine template per pool, each naming the others. With one, the shape is written once.

This provider supplies two of the four kinds a class needs: `CargoshipControlPlaneTemplate` for the control plane and `CargoshipConfigTemplate` for the workers. The infrastructure halves come from an infrastructure provider - the examples here name CAPV's `VSphereClusterTemplate` and `VSphereMachineTemplate`.

**`ClusterClass` needs the `CLUSTER_TOPOLOGY` feature gate on the management cluster, and it is off by default.** `clusterctl init` takes it as `CLUSTER_TOPOLOGY=true` in the environment, or set it on the core controller's deployment.

## The control-plane template

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1alpha1
kind: CargoshipControlPlaneTemplate
metadata:
  name: fleet
spec:
  template:
    spec:
      distro:
        fromZarfState: true
        registry: zarf-docker-registry.zarf.svc.cluster.local:5000
        name: k3s
        packageVersion: 1.34.1
      ssh:
        user: rocky
        privateKeySecretRef:
          # One secret for every cluster built from the class, so this is the reference most likely
          # to want a `namespace:`. See [Pulling the distro package](registry.md#a-secret-in-another-namespace).
          name: fleet-ssh
      clusterConfig:
        values:
          cni: cilium
      apply:
        modifyHosts: true
        modifyFirewall: true
        labelNodes: true
```

Three fields a live `CargoshipControlPlane` requires are deliberately absent, because the class and the topology supply them:

| Not in the template                      | Comes from                                                         |
| ---------------------------------------- | ------------------------------------------------------------------ |
| `version`                                | `Cluster.spec.topology.version`                                    |
| `replicas`                               | `Cluster.spec.topology.controlPlane.replicas`                      |
| `machineTemplate.spec.infrastructureRef` | `ClusterClass.spec.controlPlane.machineInfrastructure.templateRef` |

Everything else a `CargoshipControlPlane` takes is here, and [the decision record](../agent/choice-clusterclass-template.md) covers why the two specs are separate types and what keeps them in step.

## The class

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: ClusterClass
metadata:
  name: cargoship
spec:
  infrastructure:
    templateRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: VSphereClusterTemplate
      name: fleet
  controlPlane:
    templateRef:
      apiVersion: controlplane.cluster.x-k8s.io/v1alpha1
      kind: CargoshipControlPlaneTemplate
      name: fleet
    machineInfrastructure:
      templateRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
        kind: VSphereMachineTemplate
        name: fleet-cp
  workers:
    machineDeployments:
      - class: gpu
        bootstrap:
          templateRef:
            apiVersion: bootstrap.cluster.x-k8s.io/v1alpha1
            kind: CargoshipConfigTemplate
            name: fleet-worker
        infrastructure:
          templateRef:
            apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
            kind: VSphereMachineTemplate
            name: fleet-gpu
```

A `templateRef` carries a kind, a name and an apiVersion and **no namespace**, so every template lives in the class's namespace.

**The machine-deployment class name is the cargoship profile.** `class: gpu` selects `clusterConfig.profiles.gpu` - its node labels, taints, firewall rules and concurrency - which is the same join a hand-wired `MachineDeployment` makes by being *named* `gpu`. A class reaches it by a different route: Cluster API generates the deployment's object name per cluster and puts the class name in `topology.cluster.x-k8s.io/deployment-name`, which it propagates to each machine, and the converge reads that label first. A profile the configuration does not define is not an error, so a pool needs an entry only where it should differ from the defaults.

## The cluster

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: fleet-one
spec:
  topology:
    classRef:
      name: cargoship
    version: v1.34.1
    controlPlane:
      replicas: 3
    workers:
      machineDeployments:
        - class: gpu
          name: gpu
          replicas: 2
```

`version` has to agree with what the package installs. Nothing checks that - the package is the authority on what lands on a host - so a topology version that disagrees makes `status.version` wrong rather than failing.

## Upgrading a class-based cluster

This is where a cargoship cluster and a Cluster API operator's expectations part company, and it is worth reading before anyone drives this from Rancher Turtles.

Editing `version:` in a topology normally rolls the control plane: a new machine is surged, it joins, an old one goes, and so on. **Nothing is replaced here.** Cargoship converges hosts in place, so the same edit would walk every existing host through cargoship's upgrade phases - which is refused until an operator approves it. Request and approve in one edit:

```yaml
spec:
  topology:
    version: v1.35.4
    controlPlane:
      metadata:
        annotations:
          controlplane.cluster.x-k8s.io/approve-version-change: v1.35.4
```

The topology's `controlPlane.metadata` is applied to the control-plane object, which is what carries the annotation through. The value has to name the version being asked for, so it is not a standing permission and the next upgrade needs its own.

Two further things:

- The package has to move as well. `spec.version` is not part of the desired-state fingerprint, so bumping the topology's version alone converges nothing and reports `VersionChangeInert` - point `distro.packageVersion` at a package that installs the new version in the same change. With a class, that is a variable and a patch, or an edit to the template.
- `spec.topology.controlPlane.rollout.after` does nothing. There is no rollout to schedule.

See [`choice-no-rollout.md`](../agent/choice-no-rollout.md) for what the gate is protecting against.

## Variables and patches

A class can expose per-cluster values. The one most clusters need is the control-plane endpoint: a kube-vip or load-balancer address has to be chosen before the first node boots, and neither Cluster API nor an IPAM pool allocates it, because nothing creates an `IPAddressClaim` for an endpoint that is not a machine.

```yaml
spec:
  variables:
    - name: controlPlaneEndpointIP
      required: true
      schema:
        openAPIV3Schema:
          type: string
  patches:
    - name: control-plane-endpoint
      definitions:
        - selector:
            apiVersion: controlplane.cluster.x-k8s.io/v1alpha1
            kind: CargoshipControlPlaneTemplate
            matchResources:
              controlPlane: true
          jsonPatches:
            - op: add
              path: /spec/template/spec/controlPlaneEndpoint/host
              valueFrom:
                variable: controlPlaneEndpointIP
```

`valueFrom.variable`, not `value` - a `value` holding a `variable` key is a literal object and the patch would write it as one.

The class is also what guarantees the same value reaches both this field and wherever the vip's own address is configured, which is the half of it that is easy to get wrong by hand.

## Rendering one with clusterctl

[`examples/cluster-template-clusterclass-vsphere.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/examples/cluster-template-clusterclass-vsphere.yaml) is all of the above as one renderable document: the class, both cargoship templates, a `Cluster` with a topology, and a vSphere infrastructure half to adapt. Every release publishes it.

**`--flavor` cannot reach it.** clusterctl reads a flavoured template from the *infrastructure* provider's repository - `ProviderRepositorySourceOptions.InfrastructureProvider` is "InfrastructureProvider to read the workload cluster template from" - so a control-plane provider cannot ship one. `--from` takes a URL or a path instead:

```sh
export CLUSTER_CLASS_NAME=cargoship NAMESPACE=default
export CARGOSHIP_REGISTRY=zarf-docker-registry.zarf.svc.cluster.local:5000
export CARGOSHIP_PACKAGE=k3s CARGOSHIP_PACKAGE_VERSION=1.34.1
export CARGOSHIP_SSH_USER=rocky CARGOSHIP_SSH_SECRET=fleet-ssh
export CONTROL_PLANE_ENDPOINT_IP=10.0.0.10
# ...and the VSPHERE_* set the infrastructure half names.

clusterctl generate cluster fleet-one \
  --kubernetes-version v1.34.1 \
  --control-plane-machine-count 3 \
  --worker-machine-count 2 \
  --from https://github.com/colonel-byte/cluster-api-provider-cargoship/releases/latest/download/cluster-template-clusterclass-vsphere.yaml \
  > fleet-one.yaml
```

clusterctl refuses to render while any `${VARIABLE}` is unset and lists the ones it is missing, which is the fastest way to read the file. `--kubernetes-version` has to name a version the package installs; nothing checks that.

## What a class does not do

- **Adopting an existing hand-wired cluster into a topology is a manual migration**, and a cluster created from a class cannot be un-managed without one either.
- **The components still name `ghcr.io` images.** Mirroring the two controller images and overriding the registry is the same job it is without a class - see [the Rancher Turtles guide](rancher-turtles.md).
