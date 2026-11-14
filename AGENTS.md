# Working in this repository

`cluster-api-provider-cargoship` is a Cluster API bootstrap and control-plane provider pair that stands up Kubernetes with [cargoship](https://github.com/colonel-byte/cargoship) instead of kubeadm. It consumes cargoship as a library - the phases a reconcile runs are the phases the CLI runs - which is the same arrangement [`colonel-byte/terraform-provider-cargoship`](https://github.com/colonel-byte/terraform-provider-cargoship) has. See [`README.md`](README.md) for the pitch and [`docs/agent/`](docs/agent/) for the decisions behind the odd-looking parts.

It runs **in** a management cluster and opens SSH connections out to the fleet. That is unlike every other Cluster API provider, where the machine bootstraps itself from user-data, and it is the fact most of this repository's design follows from.

## The rule the whole design rests on: no timers

Every infrastructure provider takes its own unpredictable time to produce a machine with an address, and the Kubernetes API takes its own unpredictable time after that. Any design with a timeout in it is wrong for at least one provider.

So there are none. Cargoship's `apply` is a converge rather than an install - each phase decides per host whether it has anything to do - which means every reconcile can rebuild the host list from *whatever machines currently have an address*, converge those, and let the next reconcile pick up the ones that arrived late. Everything the controller waits for is a requeue: no address yet, SSH not answering, the API not answering. **Do not add a deadline, a retry budget, or a "give up after" anywhere in a reconcile.** A machine that takes eleven minutes to boot is not an error.

The one exception is the cargoship apply itself, which is a single blocking call that cannot be suspended. `pkg/converge.Runner` exists so the reconcile starts it and observes it rather than calling it; read the type comment there before changing anything about it.

## Layout

| Path                        | What lives there                                                                                                                                   |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `bootstrap/api/v1alpha1/`   | `CargoshipConfig`, `CargoshipConfigTemplate`, in `bootstrap.cluster.x-k8s.io`                                                                       |
| `bootstrap/internal/`       | The config reconciler and the cloud-init renderer                                                                                                  |
| `bootstrap/cmd/manager/`    | The bootstrap provider's binary                                                                                                                    |
| `bootstrap/config/`         | Kustomize overlays; `crd/bases` and `rbac/role.yaml` are generated                                                                                  |
| `controlplane/`             | The same four, for `CargoshipControlPlane` in `controlplane.cluster.x-k8s.io`                                                                       |
| `pkg/converge/`             | The only package that imports cargoship: Machines -> `cluster.Cluster` -> `action.Apply`, plus the desired-state fingerprint and the apply runner   |
| `pkg/sshkey/`               | Reads the controller's SSH key out of a secret, and derives the public half from it                                                                 |
| `pkg/manager/`              | The flags, scheme and manager setup both binaries share                                                                                             |
| `containers/manager/`       | One Dockerfile for both managers, selected by `MANAGER`                                                                                             |
| `docs/`                     | The mdBook source. `docs/index.md` is generated from `README.md`                                                                                    |
| `docs/agent/choice-*.md`    | Records of non-obvious decisions and the tradeoffs behind them - read before reversing one                                                          |
| `hack/`                     | `boilerplate.go.txt`, the licence header controller-gen stamps                                                                                      |

## Building and testing

A `Makefile`, not mage - which is what `colonel-byte/cargoship` uses, and the deviation is deliberate. Every Cluster API provider is driven by `make generate`, `make manifests` and `make release-manifests`; the provider implementers' documentation and clusterctl's release conventions assume those names, and `.goreleaser.yaml` calls the third one. See [`docs/agent/choice-makefile-not-mage.md`](docs/agent/choice-makefile-not-mage.md).

```sh
make generate         # CRDs, RBAC, deepcopy functions, and docs/index.md
make test             # unit tests, with the race detector
make verify           # fail if a generator would change a checked-in file
make release-manifests  # the three files clusterctl downloads
make help             # everything else
```

The repository vendors its dependencies and every CI job builds with `-mod=vendor`, so all of the above works with no network access. `make test-envtest` is wired up and has **no tests behind it yet**; see [`docs/dev/e2e-tests.md`](docs/dev/e2e-tests.md) for what that leaves uncovered and why the Docker infrastructure provider cannot be used to cover it.

## Three constraints that look reversible and are not

Each of these has a `docs/agent/` page. The short version, because each is a thing somebody will otherwise undo in good faith:

- **The control plane lists worker machines too**, and converges the whole fleet in one apply. Narrowing it to control-plane machines will appear to work, because a single-node cluster has no other hosts to be wrong about. Three cargoship phases write per-host state derived from the whole inventory, and cargoship takes a cluster lock on every host for the duration of an apply. See [`choice-one-converge.md`](docs/agent/choice-one-converge.md).
- **`golangci-lint` is not a `go.mod` tool dependency.** Adding it drags `k8s.io/*` to a patch release wanting `structured-merge-diff/v7` while cluster-api pins `v6`, and the whole module stops compiling. The first symptom is golangci-lint reporting a `typecheck` error against a file that is fine. See [`choice-tool-dependencies.md`](docs/agent/choice-tool-dependencies.md).
- **The SSH key never reaches a filesystem**, and nothing had to be added for that: rig's `protocol/ssh.Config.AuthMethods` is excluded from YAML and JSON, so it appears in no schema and no example. Do not materialize the key to a volume or add an ssh-agent sidecar. See [`choice-in-memory-ssh-key.md`](docs/agent/choice-in-memory-ssh-key.md).

### `docs/agent/choice-*.md` are constraints, not history

When a decision looks reversible from the code alone, check `docs/agent/` first. These are usually the record of a tradeoff made on purpose, not an oversight waiting to be cleaned up. A document whose entire subject was removed is marked superseded rather than deleted - the reasoning in one usually still explains something that is still here.

## The contract label is load-bearing and silent

`cluster.x-k8s.io/<contract>: <crd version>` on each CRD is what clusterctl and the core Cluster API controllers read to decide which version of this provider's API to speak. **A CRD without it is ignored rather than rejected**: the provider installs, the controllers start, and nothing ever reconciles. It is set in the kustomize `labels` block of each `config/default/kustomization.yaml`, and `install-smoke.yaml` asserts it on every pull request, because nothing else would.

The same goes for `metadata.yaml`, which maps a release series to a contract version. Without it `clusterctl init` refuses to install at all; wrong, it installs a provider that cannot talk to the core controllers. `release-guards.yaml` checks it against the vendored cluster-api on every pull request.

**Never write a contract field path from memory.** They changed between v1beta1 and v1beta2 - `status.initialized` became `status.initialization.controlPlaneInitialized`, `updatedReplicas` became `upToDateReplicas`, `unavailableReplicas` became `availableReplicas`, and the bootstrap half's `status.ready` became `status.initialization.dataSecretCreated`. Read `vendor/sigs.k8s.io/cluster-api/internal/contract/` and quote it.

## Keeping `.gitattributes` in sync with the generators

Everything a generator writes is marked `linguist-generated` in [`.gitattributes`](.gitattributes), which collapses it in a GitHub diff and keeps it out of the language statistics. A generated file that is not marked shows up as hand-written work in every review, and a regenerated thousand-line CRD buries the twenty lines that were actually written.

So a change to what a generator produces is a change to `.gitattributes` as well: add a new output path, widen a pattern rather than adding a sibling where directory depth is the only difference, and delete an entry whose generator went away. Patterns are grouped by the target that writes them, with the target named in a comment.

Hand-written files inside a generated tree stay unmarked, and there are several: only `role.yaml` is generated in each `config/rbac/`, and the other five files beside it are written by hand. Mark the pattern the generator writes, not the directory it writes into.

Check a specific file rather than reasoning about the patterns:

```sh
git check-attr linguist-generated -- controlplane/config/rbac/role.yaml
```

## Keeping `README.md` current

[`README.md`](README.md) is the front page and the pitch, and it is also the source for the book's landing page - `make docs` copies it to `docs/index.md` - so a stale README is stale in two places.

Treat it as part of the change: a new field or behaviour belongs where a reader would look for it, a removed one is deleted everywhere including the example that exercised it, and prose and snippet are fixed together. Commands shown in `README.md` are copied and pasted; they should work as written. After editing it, run `make docs` so `docs/index.md` matches.

The `Not yet supported` section is load-bearing. It is where an operator finds out that a `spec.version` change is refused, that a republished tag is not noticed, and that encrypted registry credentials do not work. When one of those lands, delete its bullet in the same change.

## Say so when you notice user-facing drift, even out of scope

Documentation and user-facing strings drift silently - nothing fails, no test goes red. When work in one corner turns up a stale user-facing surface somewhere else - a field's doc comment that becomes the generated CRD description, a README snippet, a `docs/guides/` command, a condition reason nothing sets any more - name it in the change description, with the file and what is wrong. Do not quietly widen the change to fix it, and do not stay silent because it is out of scope: the operator deciding whether it is worth a follow-up is the point.

A field's doc comment here is upstream of a generated CRD, which is upstream of what `kubectl explain` prints. Chase the wording back to the Go comment that produced it before reporting where it lives.

## A doc's claims are checkable, so check them

A dead reference reads exactly like a live one. `mdbook build` does not fail on a dead relative link, `go vet` never opens a Markdown file, and no test asserts that a path, symbol, flag, or field named in prose still exists. The cost is not spread evenly: a wrong count in a decision record costs a reader a minute, while a command in `docs/guides/` that cannot run costs an operator an afternoon, because they will assume they typed it wrong before they assume the guide is wrong.

| Failure mode                                   | How to check it                                                                                                                 |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| A path, file, or directory that moved or went  | `ls`/`find` the path as written                                                                                                 |
| A Go symbol that was renamed or unexported     | `grep -rn --include='*.go' '<name>' .`, or `go doc`                                                                             |
| A CRD field named in prose                     | `grep -n '<field>' */api/v1alpha1/*_types.go`, and the generated CRD under `*/config/crd/bases/`                                 |
| A make target that does not exist              | `make help`                                                                                                                     |
| A cargoship symbol consumed as a library       | `grep -rn '<name>' vendor/github.com/colonel-byte/cargoship/`                                                                    |
| A contract field path                          | `grep -rn '<path>' vendor/sigs.k8s.io/cluster-api/internal/contract/`                                                            |
| A quoted error or log string                   | `grep -rn '<fragment>'` for the literal. Quoting these from memory is how a guide comes to print an error no code produces       |
| A relative link between pages                  | `ls` the target. Nothing in the build catches a dead one                                                                        |
| A count, version, or pin given as current fact | Read the thing being counted. A number measured once and never re-measured is the default state of every count                   |

## Committing changes

Run the hooks over the whole tree before every commit and fix anything they flag. Prefer [`prek`](https://github.com/j178/prek) - `prek run --all-files` - if it is on `PATH`; it reads the same [`.pre-commit-config.yaml`](.pre-commit-config.yaml) and runs the same hooks, just faster. Fall back to `pre-commit run --all-files` when it isn't. Several hooks rewrite files (`end-of-file-fixer`, `trailing-whitespace`, `keep-sorted`, `addlicense`, `make generate`) - re-stage after the run. Don't skip hooks with `--no-verify` or `SKIP=`.

[`.prekignore`](.prekignore) excludes `vendor/`. Without it prek discovers the `.pre-commit-config.yaml` files vendored dependencies carry in their own repositories and runs their hooks too.

## Go test formatting

Do not condense struct literals in table-driven test cases onto single lines (such as `{name: "...", input: "...", want: "..."}`). Format each field on its own line within the struct literal to keep test cases legible and easy to review, diff, and edit.

```go
// Do not condense:
{name: "internal is preferred by default", addrs: addrs, want: "10.0.0.11"},

// Prefer multi-line:
{
	name:  "internal is preferred by default",
	addrs: addrs,
	want:  "10.0.0.11",
},
```

## Writing style

Use `-` (hyphen), not `-` (em dash), wherever a hyphen reads fine.

### Markdown line formatting

Markdown files should NOT have hard-wrapped or truncated lines. Write full paragraphs and list items on single, continuous lines without manual line breaks, letting editors and renderers handle wrapping.

Pad table cells so the columns line up, so that the source reads as a table and a reviewer can see the columns without rendering the file. Pad every cell to the width of the widest cell in its column, keep one space of padding inside each pipe, and run the dashes of the delimiter row out to that same width. Adding a wider row repads the whole table, which is expected - the alignment is part of the content.

## Commit messages

Keep commit messages to a short title only - no body paragraph explaining the change. End with a `Co-Authored-By` trailer.

```
feat(controlplane): release the pre-terminate hook after the survivors converge

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
```

## Pull requests

Before creating or updating a pull request description, read [`.github/pull_request_template.md`](.github/pull_request_template.md) and follow the format rules in [`.github/AGENTS.md`](.github/AGENTS.md#writing-a-pull-request-description).

### Naming the branches in a stacked PR

Every branch in a stack is named `stack/<n>-<2-3 word header>`:

```
stack/1-go-module
stack/2-provider-api
stack/3-converge-engine
```

**`<n>` counts within the stack, from 1.** It is the layer's position, bottom to top, so a seven-layer stack runs `stack/1-...` to `stack/7-...` and the next stack starts again at `stack/1-...`. A stack is self-contained; the number says where a branch sits in its own chain and nothing about any other work. Do not carry on from the highest number seen in the repository's history - that couples unrelated stacks to each other and means a branch name cannot be read without knowing the whole branch history.

**`<2-3 word header>`** is the layer's concern in kebab case, not a sentence. Two or three words, matching the one-sentence description the layer should already have - a layer whose concern will not fit in three words is usually two layers. Do not restate the stack's topic in every branch; the chain supplies that.

Names are used verbatim by `gh stack add`, so there is nothing to correct afterwards. Renaming a branch once its PR is open is not worth it: `gh stack` tracks the chain by branch, so the fix is to delete the stack and re-open it, which discards any review already left on it.

## Tracking work in issues

Before starting a task, check for a related issue - someone may already have filed it, or filed something that overlaps enough to change the approach:

```sh
gh issue list --repo colonel-byte/cluster-api-provider-cargoship --search "<keywords>" --state all
```

Check [`colonel-byte/cargoship`](https://github.com/colonel-byte/cargoship) too, since anything needing a change to the library surface is tracked there. If there is no issue covering the work, ask the person whether they would like one submitted before continuing. Don't open one unprompted, and don't treat a missing issue as a reason to stop - they may well say no.

## Quirks worth knowing

### `go.mod` may carry a `replace` for cargoship, and `vendor/` hides it

While a needed cargoship change is unreleased, `go.mod` points at a local checkout and `vendor/` holds a snapshot of it. CI goes green because it is building the snapshot, so nothing fails - and a release built that way cannot be built by anyone else. `release-guards.yaml` fails any pull request carrying a local-path `replace`, which is the only thing standing between that and a broken tag.

### `go list -m -f '{{.Dir}}'` returns nothing in vendor mode

Which makes a check that compares against it pass by comparing against an empty string. Read `vendor/<module>/` directly instead. The contract check in `release-guards.yaml` was written the wrong way round first and silently passed.

### The e2e suite cannot use the Docker infrastructure provider

CAPD machines are containers running a kubelet; they run no sshd, and cargoship's first phase is `Connect`. Every other provider's e2e documentation points at CAPD, so this is worth knowing before spending an afternoon on it. The fleet comes from cargoship's `test/microvm`, which is exported rather than internal for exactly this reason. See [`docs/dev/e2e-tests.md`](docs/dev/e2e-tests.md).
