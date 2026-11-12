# Node removal and the pre-terminate hook

A machine Cluster API is destroying needs no `cargoship reset` — the instance is going, so uninstalling software from it is work for nothing. But two things do need to happen, and neither is automatic:

1. **The surviving hosts' `/etc/hosts` and firewall rules still name the departing machine.** Cargoship's `ModifyHosts` and `ConfigureFirewall` phases write per-host state derived from the whole inventory, so they have to run again with the departing host absent.
2. **A departing control-plane member has to leave etcd** before its instance disappears, or the cluster loses a voter without being told.

Both are handled with a Cluster API **pre-terminate hook**.

## What the hook does

The controller puts `pre-terminate.delete.hook.machine.cluster.x-k8s.io/cargoship` on every machine it creates, at creation time. `ensurePreTerminateHooks` adds it to any machine missing it — one adopted from elsewhere, or created by an older version of the controller.

When a machine is deleted, Cluster API:

1. Cordons and drains the node.
2. Deletes the `Node` object.
3. **Stops**, and waits for every `pre-terminate.delete.hook...` annotation to be removed before letting the infrastructure provider destroy the instance.

The controller uses that window. Its next reconcile builds the inventory from the machines that remain — the departing one has no `Node` and is being deleted, so it is simply not in the list — converges them, and only then removes the annotation.

The annotation has to be claimed **at creation**, not at deletion. By the time a machine is being deleted it is already draining, and Cluster API ignores an annotation added after deletion began.

## Why this also keeps a safety check working

`phase.DetectRemovedHosts` stops an apply when the cluster holds a `Node` that no host in the inventory accounts for. That check is worth keeping — a genuinely stray node is worth stopping on — and the provider runs with `AllowUnmanagedNodes: false` so it is live.

The reason it does not fire during an ordinary scale-down is step 2 above: Cluster API deletes the `Node` object *before* the hook is consulted. By the time the controller's converge runs, there is nothing unaccounted for.

**If `DetectRemovedHosts` starts failing during a normal scale-down, the hook is not working.** Do not reach for an option to silence the check — find out why the annotation is missing or why it was released early.

## Deleting the whole control plane

`reconcileDelete` releases every hook and deletes every machine without converging anything. There are no survivors whose `/etc/hosts` needs correcting, and any converge still walking the fleet is cancelled so it does not hold SSH sessions open against hosts being destroyed.

## The one thing to verify

Whether k3s and RKE2 remove the etcd member on `Node` deletion. Both document that they do, through their own etcd controllers, and the ordering above gives them the chance to: the `Node` is deleted while the instance is still running and the etcd process is still up.

If either turns out not to, the hook is the right place to add an explicit member removal — which is a reason to have the hook even if the answer is yes.
