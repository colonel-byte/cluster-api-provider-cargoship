# `CargoshipControlPlaneTemplate`'s spec is not `CargoshipControlPlaneSpec`

A `ClusterClass` names a control plane through `spec.controlPlane.templateRef`, which has to be a *template* kind, so `CargoshipControlPlaneTemplate` exists. The obvious way to write it is the way [`CargoshipConfigTemplate`](../../bootstrap/api/v1alpha1/cargoshipconfigtemplate_types.go) is written - wrap the live spec and be done in twenty-five lines - and that is wrong here. This records why, because it will look like gratuitous duplication to the next person.

## Three fields belong to the topology controller

| Field                                    | On the live type | Who fills it in when a class stamps one                            |
| ---------------------------------------- | ---------------- | ------------------------------------------------------------------ |
| `version`                                | `+required`      | `Cluster.spec.topology.version`                                    |
| `replicas`                               | defaulted to 1   | `Cluster.spec.topology.controlPlane.replicas`                      |
| `machineTemplate.spec.infrastructureRef` | `+required`      | `ClusterClass.spec.controlPlane.machineInfrastructure.templateRef` |

Wrapping `CargoshipControlPlaneSpec` carries all three into the template, where two are required. So an operator authoring a class would have to write a version and an infrastructure reference that the topology controller then overwrites, and the API server would refuse the template until they did. The bootstrap half has no such fields, which is why its template *can* wrap its spec and is not the precedent to follow.

## Measured against Cluster API's own template, not inferred

`KubeadmControlPlaneTemplate` takes exactly this shape. From the CRDs in the `sigs.k8s.io/cluster-api@v1.14.4` module, `v1beta2`:

| CRD                            | `spec` required              | `machineTemplate.spec` holds                                               |
| ------------------------------ | ---------------------------- | -------------------------------------------------------------------------- |
| `kubeadmcontrolplanes`         | `machineTemplate`, `version` | `deletion`, **`infrastructureRef`** (required), `readinessGates`, `taints` |
| `kubeadmcontrolplanetemplates` | nothing                      | `deletion`, `taints`                                                       |

The template drops `version`, drops `replicas`, drops `infrastructureRef`, and requires nothing. `CargoshipControlPlaneTemplateResourceSpec` and `CargoshipControlPlaneTemplateMachineTemplateSpec` are the same subtraction applied to this provider's fields.

## Nothing converts between them, so a test holds them together

There is no conversion function and there should not be one: the topology controller copies `spec.template.spec` onto the new object as it is, matching by field name. That makes the duplication cheap and the drift silent - a field added to the live spec and not to the template is a field a class cannot set, and nothing fails. The resource validates, the cluster stamps, and the setting is absent.

`TestTemplateSpecCoversTheLiveSpec` compares the two field sets in both directions and fails unless a field is in both or declared in `topologyOwned` with what sets it. Adding a field to `CargoshipControlPlaneSpec` therefore fails a test until it is either carried in the template or written down as somebody else's to fill in. That is the whole mechanism keeping these two in step.

## No controller, and no RBAC

A template is never reconciled. Cluster API's topology controller reads it and stamps a `CargoshipControlPlane`, which the existing reconciler handles with no change at all - the stamped object is an ordinary one. Nothing in this repository reads a template, so no RBAC is added for it either: the topology controller runs in the core manager with its own permissions, and the contract label the kustomize `labels` block already applies to everything under `config/crd` is what makes the kind visible to it.

## What a topology version bump means here, which is not what it means elsewhere

ClusterClass's headline behaviour is that an operator edits `version:` in the topology and the control plane rolls. This provider has no rollout - see [`choice-no-rollout.md`](choice-no-rollout.md) - and since that document's gate landed, a topology version bump does **not** quietly re-converge the fleet either: the stamped object's `spec.version` moves away from `status.version`, and the reconcile stops with `VersionChangeRefused` until an annotation approves it. Under a managed topology that reads as a cluster that will not move.

The approval can be delivered through the topology rather than by hand, which is the composition worth knowing: `Cluster.spec.topology.controlPlane.metadata` is applied to the control plane object, so

```yaml
spec:
  topology:
    version: v1.35.4
    controlPlane:
      metadata:
        annotations:
          controlplane.cluster.x-k8s.io/approve-version-change: v1.35.4
```

requests the upgrade and approves it in the same edit - and the annotation's value has to be restated for the next one, so it does not become a standing permission.

Two things a topology asks for that this provider cannot do, neither of which fails loudly:

- `spec.topology.controlPlane.rollout.after` asks for a rollout after a timestamp. Nothing replaces machines here, so nothing happens.
- A topology that changes the package without changing the version converges the fleet with no approval needed, because the gate reads the version rather than the package. That is the same behaviour a hand-wired resource has.
