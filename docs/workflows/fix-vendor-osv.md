# Fix vendored osv-scanner overrides

Source: [`fix-vendor-osv.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/fix-vendor-osv.yaml). Runs on a Dependabot pull request that touches `vendor/**`.

Dependabot re-runs `go mod vendor` for every gomod update rather than going through `make`. That deletes and recreates `vendor/`, which takes the generated `osv-scanner.toml` files with it, and [check-go-mod](check-go-mod.md) then fails a pull request nobody can fix without a manual commit. This job makes that commit instead - `make vendor-osv`, pushed back onto the Dependabot branch. See [osv vendor overrides](../agent/choice-osv-vendor-overrides.md).

The trigger is `pull_request_target`, which is the one event in this repository that runs with a writable token, so the shape of the job is dictated by that:

- It **never checks the pull request out**. It generates the overrides from the base checkout and commits them through the git data API, which needs the three file contents and nothing else. Checking the pull request out would put a freshly bumped dependency's tree on a runner holding a write token, which is the failure `pull_request_target` is known for.
- It is gated on `github.event.pull_request.user.login == 'dependabot[bot]'`. Dependabot's branches live in this repository so the app token can push to them; a fork cannot be pushed to at all, and no other actor re-vendors on its own.

OpenSSF Scorecard's Dangerous-Workflow check zeroes a `pull_request_target` workflow whose checkout takes an attacker-controlled `ref:`. This one takes none, which is the state to keep it in.
