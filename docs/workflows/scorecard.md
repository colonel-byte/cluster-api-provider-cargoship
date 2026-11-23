# Scorecards supply-chain security

Source: [`scorecard.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/scorecard.yaml). Runs on pushes to `main`, weekly on a schedule, and on a branch protection rule change.

Recomputes the repository's [OpenSSF Scorecard](https://securityscorecards.dev/viewer/?uri=github.com/colonel-byte/cluster-api-provider-cargoship), uploads the SARIF to the code scanning dashboard, and keeps it as an artifact for five days. `publish_results: true` is what makes the README badge resolve; with it off the workflow still runs and the dashboard still fills in, but the badge reads `unknown` forever.

The published score only refreshes after a merge, so a workflow change can move it with nothing failing on the pull request. Score the working tree instead - `scorecard --local .` - and take the baseline from a clean export rather than by stashing. The checks a provider repository actually trips, and what to do about each, are in [`.github/AGENTS.md`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/AGENTS.md).

`vendor/` being committed costs points on a couple of checks, knowingly; see [osv vendor overrides](../agent/choice-osv-vendor-overrides.md).
