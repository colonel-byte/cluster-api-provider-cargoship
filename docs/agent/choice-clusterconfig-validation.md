# `spec.clusterConfig` is guarded in Go, because CEL cannot be pointed at it

`spec.clusterConfig` carries a cargoship cluster configuration verbatim, as an opaque object: `*apiextensionsv1.JSON` with `+kubebuilder:pruning:PreserveUnknownFields`, which generates a schema with no `type` and nothing but `x-kubernetes-preserve-unknown-fields: true`. The reasoning for carrying it that way is in the field's own doc comment - the YAML an operator writes here is the YAML of a cargoship inventory, so it needs no translation and cannot drift from the schema cargoship validates against.

The obvious way to guard the parts of it that are easy to get wrong - a profile's `concurrency`, a firewall rule's `port` - is a CEL validation rule, the way `DistroSource` guards its own combinations. **It does not work, and it does not fail quietly.** The CEL type resolver in apiextensions skips subtrees it has no schema information for, so a rule naming `self.clusterConfig.profiles` does not merely never fire: the expression does not compile, and the API server rejects the custom resource definition that carries it. There is no CEL for this field short of giving it a real schema, which means mirroring cargoship's config types field by field and owning a copy that drifts on every cargoship release.

So the guard is Go, in `converge.validateClusterConfig`, run from `DecodeClusterConfig`.

## What it checks, and what it deliberately is not

Every check calls the cargoship function that would run later anyway:

| What                                         | cargoship's own check        | Where it would otherwise be found                                           |
| -------------------------------------------- | ---------------------------- | --------------------------------------------------------------------------- |
| `registries`                                 | `cluster.ValidateRegistries` | A registry entry that configures nothing stays a no-op nobody is told about |
| `profiles.*.inherit`                         | `cluster.ResolveProfiles`    | On load, after the package is pulled                                        |
| `profiles.*.concurrency`                     | `cluster.ParseConcurrency`   | Inside each phase as it sizes its batches, so part way through an apply     |
| `profiles.*.host.firewall`                   | `FirewallConfig.Validate`    | Phase 11, three phases after SSH has reached every host in the fleet        |
| `profiles.*.host.ports`, `...policy.*.ports` | `FirewallRule.Validate`      | Nowhere - it reaches the firewall backend and renders a broken rule         |

Two things follow from "calls cargoship's own":

- **No check here is a second implementation of one.** That is the whole point: a port parser written in this repository would be one more thing to keep in step with the one that decides whether an apply succeeds. The exposed ports are checked by building an equivalent `cluster.FirewallRule` and validating it, because cargoship's port parser is unexported and `FirewallRule.Validate` is the only exported caller of it. A port with no protocol is handed `tcp`, which is what every firewall backend renders it as.
- **The profiles are folded before they are checked.** `ResolveProfiles` is called and its result is what gets validated, so a profile inheriting a bad concurrency is refused like one spelling it out. The folded map is discarded: cargoship folds them again when it loads the cluster, so nothing here changes what the converge is given.

This is validation, not admission. `kubectl apply` still succeeds, because nothing in this repository runs a webhook - there is no cert-manager wiring, no service, no kustomize patch and no release-manifest entry for one, and adding that stack to reject a handful of fields would be a far larger thing than the fields are worth. The failure is reported on the `Converged` condition with the reason `ClusterConfigInvalid`, and it is reported *as* a refused converge rather than as an incomplete inventory: the machines are fine, the document is wrong, and only an edit to it changes that. `converge.ErrClusterConfig` is what lets the reconcile tell those two apart.

## The schema guards that are possible

The fields this provider does own in its own schema are guarded there, where the API server can reject them on `kubectl apply`:

- `spec.apply.workerConcurrent` matches a count or a percentage. Nothing in this repository parses it - cargoship does, as it sizes each phase's batches - so without the pattern a typo lands part way through an apply with earlier phases already applied to hosts.
- `spec.version` requires the leading `v` and a `major.minor.patch`, with anything after it allowed, because a distro package's build suffix is part of the version it installs: `v1.31.2+k3s1`.
- `spec.distro.registry` may not contain `://`. It is a host with an optional path prefix; a scheme there builds a reference like `oci://https://registry/...` and keys the pull credentials under a host of `https:`.
