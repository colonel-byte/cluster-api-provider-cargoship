# Why `make vendor` deletes three files `go mod vendor` just wrote

`go mod vendor` copies every file that sits in the directory of a package this module imports, which for three dependencies means a Python requirements file: a test harness under `github.com/theupdateframework/go-tuf`, a MkDocs toolchain under `github.com/txn2/txeh`, and a `codespell` pin under `go.opentelemetry.io/otel`. None of them is a dependency of this module, nothing here installs or executes them, and `make vendor` deletes them immediately after the vendoring step puts them back.

## What forced it

The [OpenSSF Scorecard](https://scorecard.dev/viewer/?uri=github.com/colonel-byte/cluster-api-provider-cargoship) Vulnerabilities check runs osv-scanner recursively over the whole checkout and scores it as `MaxResultScore - numVulnsFound`, floored at zero. There is no severity weighting and no reachability analysis, so every finding costs one point. The check read 3 against 7 findings, and every one of them came out of `vendor/`. A local run of the same scanner on the same day read 11, which is how the counts below were measured -- the number moves as advisories are published, and the scored 7 was the scan of 2026-11-15:

| Manifest                                                            | Ecosystem | Findings | What it pins                                                   |
| ------------------------------------------------------------------- | --------- | -------- | -------------------------------------------------------------- |
| `vendor/github.com/theupdateframework/go-tuf/requirements-test.txt` | PyPI      | 7        | The Python harness go-tuf runs its own conformance tests under |
| `vendor/github.com/txn2/txeh/requirements-docs.txt`                 | PyPI      | 3        | The MkDocs toolchain that builds txeh's documentation site     |
| `vendor/go.opentelemetry.io/otel/requirements.txt`                  | PyPI      | 0        | A `codespell` pin for otel's own spelling check                |

The otel file has no findings today and is pruned anyway, because a one-package requirements file is one advisory away from costing a point and the whole purpose of the arrangement is that an advisory upstream is not an event here.

`govulncheck`, which resolves what the binaries actually call, reports zero. The advisories are true statements about `requests` and `mkdocs-material`, and false statements about this provider: the check was not measuring this repository.

## A config at the root does nothing, and neither does un-vendoring

osv-scanner resolves configuration relative to the manifest it is scanning rather than to the root of the scan, and it only consults a single root config when the caller passes `ConfigPath` -- which Scorecard's `clients/osv.go` does not. Measured, not assumed: an `.osv-scanner.toml` at the repository root with an ecosystem-wide `[[PackageOverrides]]` left all 11 local findings in place, and so did a per-package one.

The configuration has to sit beside the manifest, which is what [cargoship does](https://github.com/colonel-byte/cargoship/blob/main/docs/agent/choice-osv-vendor-overrides.md) -- it has four such manifests, one of them an npm lockfile carrying 34 findings, so a file it cannot delete is worth generating a config next to. Here there are three files, all removable, and pruning them is a line of `find` against a mage package, a verify target and a `pull_request_target` workflow. Deleting the Go files osv-scanner should be reading is of course not on the table, and neither is dropping `vendor/`: the tree is vendored so the repository builds on a management node with no route to a module proxy, which is not negotiable for a scorecard point.

## The prune has to be the only way the tree is written

`rm -rf vendor && go mod vendor` puts all three files back, so a prune done by hand is undone by the next dependency bump, silently and in the direction of a lower score. `make vendor` is the prune, and `check-go-mod.yaml` runs that target rather than `go mod vendor` so the committed tree and the check agree.

The cost lands on Dependabot, which runs `go mod vendor` itself for every gomod update rather than going through make. Its branch arrives with the three files restored, and `check-go-mod` then fails on the diff the prune produces. That is a loud failure on the pull request that caused it, fixed by running `make vendor` on the branch -- which is the trade being made here: cargoship hit the same thing and answered it with a workflow that writes the files back through the git data API, and that machinery is worth more than three `find` matches are.

## What this does not do

It does not raise the check to 10. One finding survives, and it is not one an override would be right to silence: osv-scanner matches a commit range in a circl advisory against the vendored circl source. It read `CVE-2025-8556` (FourQ) locally and the Scorecard run an hour earlier read `CVE-2026-1229` (secp384r1); both are commit-range-only records against a module whose Go-database entry records the fix in v1.6.3, and `go.mod` carries v1.6.5. `vendor/github.com` is the source osv-scanner reports it against, so there is nothing to prune and nothing to bump -- the honest fix is a version range filed against the advisory. Expect the identifier to keep changing and the ceiling to sit at 9.

None of this makes anything safer. `govulncheck` reported zero before the prune and reports zero after; what changed is that a check which was describing go-tuf's test harness is now describing this provider. It is a measurement fix, not a security fix.
