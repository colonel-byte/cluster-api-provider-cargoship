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

**A worker pool's cargoship profile does not yet come from the class.** For a hand-wired `MachineDeployment` the profile is the deployment's own name, so one called `gpu` selects `clusterConfig.profiles.gpu` - its labels, taints, firewall rules and concurrency. A topology does not name its deployments that way: Cluster API generates the name and tracks the class in a label instead, so a pool declared `class: gpu` currently selects no profile and its hosts take the defaults, silently, because a profile the configuration does not define is not an error. Until the converge reads that label, keep per-pool settings out of a class.

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
              value:
                variable: controlPlaneEndpointIP
```

The class is also what guarantees the same value reaches both this field and wherever the vip's own address is configured, which is the half of it that is easy to get wrong by hand.

## What a class does not do

- **Adopting an existing hand-wired cluster into a topology is a manual migration**, and a cluster created from a class cannot be un-managed without one either.
- **The components still name `ghcr.io` images.** Mirroring the two controller images and overriding the registry is the same job it is without a class - see [the Rancher Turtles guide](rancher-turtles.md).
