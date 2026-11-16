# A cross-namespace secret reference is refused unless an operator opts in

`pullSecretRef`, `caBundleSecretRef` and `ssh.privateKeySecretRef` each take an optional `namespace`, and the controllers refuse any value other than the referring object's own namespace unless they were started with `--secret-namespaces` naming it. The flag's default is empty, which is the behaviour before the field existed: same namespace only.

## Why the default is to refuse rather than to allow

The controller reads secrets through a cluster-wide `ClusterRole` - it has to, because a management cluster puts each workload cluster in its own namespace and the provider is installed once. The reference is therefore not an access check, it is a request the controller performs with its own permission. Allowing any namespace by default would mean anybody who can create a `CargoshipControlPlane` in a namespace they own can have the controller read a credential they do not.

For the SSH key that is worse than reading the secret. The bootstrap provider derives the public half of whatever key the control plane names and publishes it into each machine's bootstrap data, so the borrower ends up with a fleet that the borrowed key opens, while the namespace that owns the key sees nothing happen at all. A field that is harmless on a pull secret is a privilege escalation on that one, and they are the same field.

Opening it up later is a flag value. Closing it down later is a breaking change for every cluster that had come to rely on it, which is the asymmetry the default follows from.

## No startup validation of the two things it interacts badly with

Both are documented in [`docs/guides/registry.md`](../guides/registry.md) and neither is enforced:

- **`clusterctl move` does not move the secret.** It moves what the cluster owns, and a secret outside the cluster's namespace is not that. A moved cluster arrives unable to reach its registry or its fleet.
- **`--namespace` on the manager makes the secret invisible.** That flag scopes the whole controller-runtime cache to one namespace, so a secret anywhere else reads as `NotFound`.

The second one is tempting to check at startup - the two flags are both right there - and it is deliberately not checked. `--secret-namespaces` is a list of what is *permitted*, not a list of what is used: an operator can reasonably set it on a manager that is also namespace-scoped, against the day the scope is widened, and refusing to start would be refusing a configuration that is merely unused. The failure it would prevent is a `NotFound` naming the secret, at the one moment the operator is looking at the reference they just wrote.

## The method rather than the call sites

`SecretReference.Key(ownerNamespace, allowed)` is where the decision lives, and the four resolution sites - the CA bundle, the pull secret, and the SSH key in each of the two providers - go through it. A cross-namespace reference that is not permitted is an error, never a quiet fall back to the owner's namespace: reading a different secret than the one named is a worse outcome than refusing, because it succeeds.

`contentFrom.secret` on a `CargoshipConfig`'s files is deliberately not included. It is a different reference type, written by whoever writes the config rather than by the control plane, and nothing has asked for it.
