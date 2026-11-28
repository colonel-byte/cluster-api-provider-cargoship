# Why vendor/ carries three generated osv-scanner.toml files

`go mod vendor` copies every file that sits in the directory of a package this module imports, which for three dependencies means a Python requirements file: a test harness under `github.com/theupdateframework/go-tuf`, a MkDocs toolchain under `github.com/txn2/txeh`, and a `codespell` pin under `go.opentelemetry.io/otel`. Beside each of them now sits a generated `osv-scanner.toml` telling a scanner to skip that ecosystem. This records why those three files exist, why they are generated rather than written by hand, and what they deliberately do not cover.

## What forced it

The [OpenSSF Scorecard](https://scorecard.dev/viewer/?uri=github.com/colonel-byte/cluster-api-provider-cargoship) Vulnerabilities check runs osv-scanner recursively over the whole checkout and scores it in `checks/evaluation/vulnerabilities.go` as `MaxResultScore - numVulnsFound`, floored at zero. There is no severity weighting and no reachability analysis: every finding is one point. The check read 3 against 7 findings, and every one of them came out of `vendor/`. A local run of the same scanner on the same day read 11, which is where the counts below come from -- the number moves as advisories are published, so re-measure rather than quoting it:

| Manifest                                                            | Ecosystem | Findings | What it pins                                                   |
| ------------------------------------------------------------------- | --------- | -------- | -------------------------------------------------------------- |
| `vendor/github.com/theupdateframework/go-tuf/requirements-test.txt` | PyPI      | 7        | The Python harness go-tuf runs its own conformance tests under |
| `vendor/github.com/txn2/txeh/requirements-docs.txt`                 | PyPI      | 3        | The MkDocs toolchain that builds txeh's documentation site     |
| `vendor/go.opentelemetry.io/otel/requirements.txt`                  | PyPI      | 0        | A `codespell` pin for otel's own spelling check                |

None of it is a dependency of this module. Nothing in those files is compiled, executed, installed or shipped; they are in the tree because they happen to sit in the directory of a module this provider imports Go packages from, and `go mod vendor` copies directories. `govulncheck`, which resolves what the binaries actually call, reports zero -- that is the scan in [`scan-vuln.yaml`](../../.github/workflows/scan-vuln.yaml), and it is what actually covers the Go code.

So the advisories are true statements about `requests` and `mkdocs-material`, and false statements about this provider. The check was not measuring this repository.

The otel row is zero findings today and has an override anyway. A `requirements.txt` with one package in it is one advisory away from costing a point, and the whole purpose of the arrangement is that a publication upstream is not an event here.

## The config has to sit beside the manifest

osv-scanner resolves configuration relative to the file it is scanning, not to the root of the scan, and it only consults a single root config when the caller passes `ConfigPath` -- which Scorecard's `clients/osv.go` does not. An `.osv-scanner.toml` at the repository root therefore changes nothing at all, which is what anyone would try first. Measured, not assumed, with osv-scanner v2 over this tree:

| Arrangement                                               | Findings |
| --------------------------------------------------------- | -------- |
| No config                                                 | 11       |
| One `.osv-scanner.toml` at the repository root            | 11       |
| The same file with a per-package override instead         | 11       |
| One `osv-scanner.toml` beside each of the three manifests | 1        |

Deleting the three files works too, and is what this landed as first: they are not build inputs, so `go mod vendor` followed by one `find -delete` left the same single finding. It was replaced because the prune has no answer for a bot that re-vendors, which is the section below, and because the override is the arrangement [cargoship already runs](https://github.com/colonel-byte/cargoship/blob/main/docs/agent/choice-osv-vendor-overrides.md) -- including for an npm lockfile under `x/telemetry` that is not in this module's tree and could not be pruned without a second mechanism. One pattern across the two repositories is worth more than one `find` match.

Dropping `vendor/` entirely would have removed the problem at the root and was never a candidate. The tree is vendored so the repository builds on a management node with no route to a module proxy; that is the reason `vendor/` exists at all, and it is not negotiable for a scorecard point.

## Ecosystem-wide overrides, not lists of advisory IDs

`[[IgnoredVulns]]` suppresses named advisories and would work. It is not what is wanted: the claim being made here is not "these particular advisories are harmless", it is "this ecosystem is not part of the build", which is a property of the manifest and stays true as the advisory list changes. An ID list would need a commit every time a new CVE landed against a lockfile nobody here reads, and each of those commits would look like a security decision while being pure bookkeeping.

`[[PackageOverrides]]` with only `ecosystem` and `ignore = true` says the intended thing once. Each generated file carries a `reason` and a comment explaining itself to whoever finds it while reading vendored third-party source, which is the only way anyone comes across it.

## Generated by mage, not written by hand

`magefiles/pkg/gen/osv` holds the three entries and the text they render to. `make vendor` runs `go mod vendor` and then writes them, so the normal way of updating dependencies leaves them in place without anybody remembering; `make vendor-osv` writes them without re-vendoring, which is what the workflow below needs. Committing them as ordinary files was the alternative, and it fails the first time somebody runs `go mod vendor` and commits the result.

`make verify-vendor` is the guard, wired into [`check-go-mod.yaml`](../../.github/workflows/check-go-mod.yaml) beside the `git diff --exit-code vendor/` that already catches a deleted or edited override. It fails if a declared override is missing or has drifted from the generator, if the manifest an entry names has gone away -- which turns a stale entry into a build error rather than a file quietly doing nothing -- and, the half that matters more, if `vendor/` holds a non-Go dependency manifest that no entry covers. Without that last check, a dependency bump that happened to pull in a module carrying a `Gemfile.lock` would drop the score on some later Scorecard run with nothing in the diff to point at. It fails on the commit that caused it instead.

## Renovate deletes them, so a workflow puts them back

Generating the overrides from `make vendor` assumes the normal way of updating a dependency goes through make. It does not: Renovate re-runs `go mod vendor` itself for every gomod update, and the recreated tree arrives without the three files. In cargoship that meant a hand-written `ci: readd the osv scanner artifacts` commit on two Dependabot branches before the workflow existed. Renovate's `postUpgradeTasks` in `.github/renovate.json5` runs `make vendor-osv` on its own branches, so the workflow below is a backstop for the runs where that did not happen.

[`fix-vendor-osv.yaml`](../../.github/workflows/fix-vendor-osv.yaml) makes that commit instead, ported from cargoship. It is the one workflow here that runs on `pull_request_target`, because it needs a token that can push to the pull request branch and a bot's `pull_request` runs are handed a read-only one. That trigger is also why the job never fetches the pull request at all: it checks out the base branch, runs `make vendor-osv` against that tree, and writes the results onto the pull request branch through the git data API -- a blob per changed override, a tree on top of the branch's own tree, a commit, and a fast-forward update of the ref. Putting a freshly bumped dependency's tree on a runner that holds a write token is the specific failure `pull_request_target` is known for, and the API route means it never lands there to begin with. Generating from base is sound because the contents come from `Overrides`, and a dependency bump never touches that file.

Scorecard's Dangerous-Workflow check is why the base checkout names no `ref`: its `validateUntrustedCodeCheckout` flags any `actions/checkout` in a `pull_request_target` workflow whose `ref` mentions `github.event.pull_request`, including one naming `base.sha`, which is a trusted commit. On `pull_request_target` the default checkout is already the base branch tip, so naming it would buy nothing and claim something the job does not do.

The commit is built with explicit author and committer fields rather than `git commit`, so the `[bot]` identity and the `Signed-off-by` trailer are carried the same way they would be. The ref update is a plain PATCH with no force: the parent is whatever `git/ref/heads/<branch>` returned moments earlier, so a bot push that lands mid-run makes the update fail instead of overwriting the newer commit. It uses the release-please GitHub App rather than `GITHUB_TOKEN`, which is not a preference -- a push authenticated with `GITHUB_TOKEN` raises no `pull_request` event, so `validate-go-mod` would stay red on the superseded commit and the pull request would still not be mergeable.

Two cases are deliberately left to fail. A bump that drops a module entirely leaves no directory to write into, and a bump that pulls in a module carrying an uncovered lockfile trips the second half of `make verify-vendor`. Both need `Overrides` edited by a person, so the workflow skips them and lets the check report.

## What this does not do

It does not raise the check to 10. One finding survives, and it is not one an override would be right to silence: osv-scanner matches a commit-range-only circl advisory against the vendored circl source, reading `CVE-2025-8556` (FourQ) locally and `CVE-2026-1229` (secp384r1) on the Scorecard run an hour earlier. Both record the fix in v1.6.3 and `go.mod` carries v1.6.5, so the match is a defect in the advisory rather than in the dependency; `vendor/github.com` is the source it is reported against, so there is nothing to bump and nothing a per-manifest override could cover. Expect the identifier to keep changing and the ceiling to sit at 9 -- a score that oscillates by one for that reason is not a signal, and an `[[IgnoredVulns]]` entry for a Go advisory would be the wrong trade. The honest fix is a version range filed against the advisory.

None of this makes anything safer. `govulncheck` reported zero before the overrides existed and reports zero after; what changed is that a check which was describing go-tuf's test harness is now describing this provider. It is a measurement fix, not a security fix, and the maintenance cost is real: it lands on whoever next adds a dependency that carries a lockfile, which is what `make verify-vendor` exists to turn into a clear failure rather than a mystery.
