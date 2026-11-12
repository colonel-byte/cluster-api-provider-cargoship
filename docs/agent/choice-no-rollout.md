# There is no rollout, and `spec.version` changes are refused rather than half-applied

`CargoshipControlPlane` scales up and scales down. It does not replace machines, and it does not upgrade them in place. A change to `spec.version` against a converged control plane is reported as unsupported rather than acted on.

## Why refusing is better than trying

`KubeadmControlPlane`'s rollout is the most intricate part of it: surge a new machine, wait for it to join, remove an old one, repeat, while keeping etcd quorum and handling a machine that fails to come up halfway through. Half of that implemented is worse than none of it, because the failure mode is a control plane stuck between two versions with no path forward that the controller knows how to take.

Cargoship does have the pieces — `pkg/phase/65_upgrade_common.go` and the engine-config-sync phases upgrade a host in place, draining and waiting for `Ready` through `waitNodeReady`. So the shape of an in-place rollout is available and is probably the right one for this provider, since replacing machines throws away the thing cargoship is good at. It is a separate piece of work with its own correctness argument, not an extension of this one.

## What an operator does meanwhile

Build a new cluster and move workloads, or scale the control plane up with machines created from a template carrying the new version and scale the old ones down — which works, because a scale-up creates a machine with `spec.version` from the custom resource and the converge installs whatever the package holds.

The second route is why `status.version` is set from `spec.version` rather than observed per machine: with no rollout, every machine that exists was created at the current version. **When rollout lands that stops being true**, and `updateStatusCounts` has to compare each machine's version against the desired one to fill in `upToDateReplicas` honestly. The comment on that function says so; the counts are correct today and will quietly lie the moment a second version can exist.
