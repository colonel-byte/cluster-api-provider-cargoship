# Node removal and the pre-terminate hook

A machine Cluster API is destroying needs no `cargoship reset` - the instance is going, so uninstalling software from it is work for nothing. But two things do need to happen, and neither is automatic:

1. **The surviving hosts' `/etc/hosts` and firewall rules still name the departing machine.** Cargoship's `ModifyHosts` and `ConfigureFirewall` phases write per-host state derived from the whole inventory, so they have to run again with the departing host absent.
2. **A departing control-plane member has to leave etcd** before its instance disappears, or the cluster loses a voter without being told.

Both are handled with a Cluster API **pre-terminate hook**.

## What the hook does

The controller puts `pre-terminate.delete.hook.machine.cluster.x-k8s.io/cargoship` on **every machine in the cluster**, workers included: the converge is one apply over the whole fleet, so a departing worker leaves the survivors' `/etc/hosts` and firewall rules naming a host that is going, exactly as a departing control-plane member does.

A control-plane machine gets it at creation, which is the earliest moment there is. A worker is created by a MachineDeployment rather than by this controller, so there is no creation to hook into: `ensurePreTerminateHooks` claims it on the first reconcile that sees the machine, and does the same for a control plane adopted from elsewhere or a machine created by an older version of the controller.

When a machine is deleted, Cluster API:

1. Cordons and drains the node.
2. Deletes the `Node` object.
3. **Stops**, and waits for every `pre-terminate.delete.hook...` annotation to be removed before letting the infrastructure provider destroy the instance.

The controller uses that window. Its next reconcile builds the inventory from the machines that remain - the departing one has no `Node` and is being deleted, so it is simply not in the list - converges them, and only then removes the annotation.

## What the hook waits for

A converge that succeeded says the survivors no longer name the departing host. It does not say the machine replacing it has arrived, so the hook is held past the converge until every surviving machine:

1. Has a node reference (`status.nodeRef.name`), which is Cluster API's record that its `Node` turned up.
2. Has a live `Node` on the workload cluster that exists and is `Ready`.
3. Carries `node-role.kubernetes.io/<profile>: "true"` on that `Node` - only where `spec.apply.labelNodes` is on, since nothing writes the label otherwise, and only for a machine that selects a profile at all.

Cargoship's `LabelNodes` phase skips a host whose `Node` it could not find, and logs a warning rather than failing, so a fleet can converge cleanly with a replacement's role label missing. Releasing the hook then destroys the departing instance while nothing yet carries the label, and a workload whose affinity selects it - an infrastructure-only pod, say - goes unschedulable with nowhere to land.

While the hook is held the `MachinesReady` condition reads `WaitingForReplacementNodes` and names the machine being waited on. There is no deadline on it, as there is nowhere in this provider: a replacement that takes eleven minutes to join holds the hook for eleven minutes. The reconcile observes the converge that already succeeded rather than starting another, and the Machine watch wakes it when the node reference appears.

Tearing the whole cluster down is not gated, in either order it happens in. Cluster API deletes a cluster's workers before its control plane, so a worker's hook is released by the ordinary reconcile as soon as the `Cluster` has a deletion timestamp, and the control plane's own hooks by `reconcileDelete` once it has one of its own. Neither converges anything first: there are no survivors to wait for, and a cluster being deleted because it is broken would never satisfy a gate that waited for one.

The annotation has to be claimed **before** a machine is deleted, not at deletion. By the time a machine is being deleted it is already draining, and Cluster API ignores an annotation added after deletion began.

## Why this also keeps a safety check working

`phase.DetectRemovedHosts` stops an apply when the cluster holds a `Node` that no host in the inventory accounts for. That check is worth keeping - a genuinely stray node is worth stopping on - and the provider runs with `AllowUnmanagedNodes: false` so it is live.

The reason it does not fire during an ordinary scale-down is step 2 above: Cluster API deletes the `Node` object *before* the hook is consulted. By the time the controller's converge runs, there is nothing unaccounted for.

**If `DetectRemovedHosts` starts failing during a normal scale-down, the hook is not working.** Do not reach for an option to silence the check - find out why the annotation is missing or why it was released early.

## Deleting the whole control plane

`reconcileDelete` releases every hook and deletes every control-plane machine without converging anything. There are no survivors whose `/etc/hosts` needs correcting, and any converge still walking the fleet is cancelled so it does not hold SSH sessions open against hosts being destroyed.

It releases the hook from every machine rather than only the ones being deleted, workers among them: the controller is about to drop its finalizer and stop watching, and a hook left behind would be held by nobody - the next deletion of that machine would wait on an annotation with no controller left to remove it. The reconcile takes the same way out as soon as the `Cluster` is deleted, for the same reason and one more: it is the only pass that runs while the workers are going.

## The one thing to verify

Whether k3s and RKE2 remove the etcd member on `Node` deletion. Both document that they do, through their own etcd controllers, and the ordering above gives them the chance to: the `Node` is deleted while the instance is still running and the etcd process is still up.

If either turns out not to, the hook is the right place to add an explicit member removal - which is a reason to have the hook even if the answer is yes.
