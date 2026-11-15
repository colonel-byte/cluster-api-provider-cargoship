# There is no rollout, and a `spec.version` change needs an operator's approval

`CargoshipControlPlane` scales up and scales down. It does not replace machines, and it does not sequence an upgrade. A change to `spec.version` against a converged control plane is refused on the `Converged` condition unless `controlplane.cluster.x-k8s.io/approve-version-change` names the version being asked for.

## What this document used to claim, and what was actually true

It said a version change was "reported as unsupported rather than acted on", and the README said it was "rejected rather than half-applied". Neither was implemented. There are no webhooks in this provider, there was no CEL rule on `spec.version`, and no check in the reconcile - so what actually happened was:

- `spec.version` was copied onto any new machine and into `status.version`, and
- if the package moved too, the desired-state fingerprint changed and a converge ran - and `action.Apply` already registers `UpgradeController` and `UpgradeWorkers`, so cargoship upgraded every existing host **in place**, workers in whatever batches `spec.apply.workerConcurrent` allowed, with nothing in this controller sequencing or supervising it, while
- `status.upToDateReplicas` was the replica count unconditionally, so the resource claimed every machine was current throughout.

That is the half-applied upgrade this document exists to refuse, reachable by accident. The gate below is what makes the claim true.

## The gate

`versionChange` compares `status.version` - which only a successful converge writes, so it is what the hosts last had installed - against `spec.version`. When they differ, one of three things happens:

| State                                                     | Reason on `Converged`  | What happens                                                    |
| --------------------------------------------------------- | ---------------------- | --------------------------------------------------------------- |
| The package has not moved (fingerprint unchanged)         | `VersionChangeInert`   | Nothing. The message names `spec.distro` as the field to change |
| The package moved, and no annotation approves the version | `VersionChangeRefused` | Nothing. The message names the annotation and the value to set  |
| The package moved and the annotation names `spec.version` | -                      | The converge runs, and upgrades every host in place             |

The annotation's **value** is compared with `spec.version` rather than its presence being checked, so an approval left behind from one change cannot wave through the next.

`VersionChangeInert` exists because a version bumped on its own installs nothing: `converge.DesiredState` does not carry `spec.version` at all, so the fingerprint is unchanged and no converge would run. Approving it would not help. Reporting it as refused would have told an operator to set an annotation that changes nothing, and reporting it as converged would have left the resource claiming a version it is not running. This was found by a test written to assert the approved path, which never ran a second converge.

**The machines' own versions are deliberately not the gate.** A machine's `spec.version` is fixed when it is created and this provider never replaces a machine, so after an in-place upgrade every machine disagrees with the fleet forever - a gate reading them would refuse every converge from then on, and `upToDateReplicas` computed from them would never recover. `upToDateReplicas` is instead the whole-fleet answer the fingerprint already gives: the replica count when the last successful converge matches what is wanted now, and zero while a version change is pending or a converge is running.

## Why refusing is better than trying

`KubeadmControlPlane`'s rollout is the most intricate part of it: surge a new machine, wait for it to join, remove an old one, repeat, while keeping etcd quorum and handling a machine that fails to come up halfway through. Half of that implemented is worse than none of it, because the failure mode is a control plane stuck between two versions with no path forward that the controller knows how to take.

Cargoship does have the pieces - `pkg/phase/65_upgrade_common.go` and the engine-config-sync phases upgrade a host in place, draining and waiting for `Ready` through `waitNodeReady`. So the shape of an in-place rollout is available and is probably the right one for this provider, since replacing machines throws away the thing cargoship is good at. It is a separate piece of work with its own correctness argument, not an extension of this one.

## What an operator does meanwhile

Build a new cluster and move workloads, or scale the control plane up with machines created from a template carrying the new version and scale the old ones down - which works, because a scale-up creates a machine with `spec.version` from the custom resource and the converge installs whatever the package holds.

Or approve the in-place upgrade with the annotation, which is what the gate above is for: change `spec.distro` to a package installing the new version, set `spec.version` to match, annotate, and the next converge walks the hosts. That is not a rollout - nothing is surged, nothing is drained by this controller, and a failure partway leaves a fleet with some hosts upgraded - but it is cargoship's own upgrade path, supervised by the operator rather than by a controller.

`status.version` is still written from `spec.version` on a successful converge rather than observed per machine, because a converge either installed the package everywhere or failed. What changed is that it is now also the gate's input, so it has to stay honest: it is written **only** in the success branch, never on a pass that merely found the fleet up to date.
