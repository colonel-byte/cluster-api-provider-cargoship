# The control plane owns the whole fleet, not just the control-plane machines

`CargoshipControlPlane`'s reconcile lists **every** `Machine` with the cluster's name label — control plane and `MachineDeployment` workers alike — and converges all of them in one `cargoship apply`. `KubeadmControlPlane` owns only control-plane machines, and the Cluster API control-plane contract is written on that assumption, so this is a deliberate departure from it.

## Why

A cargoship fleet is not a set of independent hosts. Three phases write state derived from the *whole* host list onto *every* host:

- `ModifyHosts` rewrites each host's `/etc/hosts` with every node in the inventory.
- `ConfigureFirewall` opens the cluster's ports in each host's own firewall, with rules that name the other hosts.
- Per-profile `Concurrency` batches hosts across the fleet — a `25%` profile means a quarter of the hosts sharing that profile, which is not a question one controller can answer about a subset.

Split across two controllers, each would compute those from half the fleet and overwrite the other half's answer. The failure is not a race that shows up under load; it is the steady state. A worker joining would rewrite `/etc/hosts` on the controllers without the other workers in it, and the next control-plane converge would put them back.

The second reason is cheaper to state: cargoship takes a cluster lock on every host for the duration of an apply (`pkg/phase/91_lock.go`). Two converges over overlapping hosts do not interleave — the second fails on the lock. So "two controllers, one fleet" is not slower than one controller; it does not work at all.

## What it costs

- **The contract's replica counts only describe the control plane.** `status.replicas`, `readyReplicas`, `availableReplicas` and `upToDateReplicas` count control-plane machines, as the contract requires, and the scale subresource's selector matches only them. A worker is in the converge and not in the counts, which is correct but reads oddly next to a `status.hosts` list that includes it.
- **A `MachineDeployment` scale-up waits on the control-plane controller.** Its machines get bootstrap data from the bootstrap provider as usual, but nothing installs anything on them until the control plane's next converge. The `Watches` on `Machine` in `SetupWithManager` is what keeps that prompt rather than waiting out the drift interval — without it a new worker would sit unconverged for up to ten minutes, which looks exactly like a broken provider.
- **RBAC is wider than KubeadmControlPlane's.** The control-plane controller needs `list` and `watch` on all `Machine` objects, not only the ones it owns.

## What would change it

Cargoship growing the ability to converge a declared subset of the inventory while reading the whole of it — so a worker-only run could rewrite that worker and leave the controllers' `/etc/hosts` to the control-plane run. `ResetOptions.TargetHosts` is the shape of that idea for `reset`; there is no equivalent for `apply`, and adding one means deciding what `ModifyHosts` does when it can see hosts it may not write to.

Until then, do not "fix" this by narrowing the machine list to the control plane. It will appear to work, because a single-node cluster has no other hosts to be wrong about.
