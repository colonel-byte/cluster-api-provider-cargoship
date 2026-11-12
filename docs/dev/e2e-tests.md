# End-to-end tests

The unit tests cover everything that can be covered without a fleet: inventory construction from `Machine` objects, the desired-state fingerprint, the converge runner's concurrency, and cloud-init rendering. They need no cluster and no network.

```sh
make test
```

An end-to-end test needs something a converge can actually SSH into, and that is the constraint that shapes the whole suite.

## Why not the Docker infrastructure provider

The obvious choice for a provider's e2e suite is CAPD — Cluster API's Docker provider — because it needs nothing but Docker. **It cannot be used here.** CAPD machines are containers running a kubelet; they run no sshd. Cargoship's first phase is `Connect`, and there is nothing to connect to.

This is worth stating because every other provider's e2e documentation points at CAPD, and discovering why it does not work here costs an afternoon.

## What is used instead

`colonel-byte/cargoship`'s microVM fleet, which is importable for exactly this reason — `test/microvm` and the root `test` package are exported rather than internal so another module's suite can drive them, which `terraform-provider-cargoship` already does.

The suite:

1. Brings up a Rocky VM fleet with `test/microvm`.
2. Creates `Machine` objects with those static addresses, plus a fake infrastructure provider that marks them ready and does nothing else.
3. Drives a real converge and asserts the kubeconfig secret appears and the nodes join.

`CARGOSHIP_E2E_EXAMPLE_ROOT` points it at a cargoship checkout so it reads the generated distro definitions rather than carrying a copy.

The microVM backend is also the only one where cargoship's SELinux, fapolicyd and firewalld phases run as the paths they are rather than as gates that correctly do nothing — so it is a better test of a converge than a container fleet would be even if containers could run sshd.

## Reading a run

**Check which steps skipped, not only which failed.** A skip on this suite usually means a capability the backend does not have, and a run with fifteen of them is not a pass. The same rule as cargoship's own suite, for the same reason.

The fleet brings itself up and tears itself down, and a whole run needs no elevation. Do not reach for `virsh` to inspect it: `virsh -c qemu:///system` goes through polkit and interrupts whoever is at the keyboard to answer a question the run's own log already answers. A fleet left behind by a killed run comes down with `mage dev:vmDown -fleet=<name>` from the cargoship checkout.

## What has no coverage yet

- The reconcile state machine end to end. It wants envtest with a fake infrastructure provider, asserting the **requeues** rather than any timing: no address yet, SSH not answering, converge in flight, converged. `make test-envtest` is wired for it and there are no tests behind it.
- The pre-terminate hook's ordering. The thing worth asserting is that the survivors' converge happens before the annotation is released, and that is a sequence, not a state.
