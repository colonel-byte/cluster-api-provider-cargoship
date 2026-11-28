# Renovate

Source: [`renovate.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/renovate.yaml). Runs hourly on a cron, on a manual dispatch with a log level and a dry-run toggle, and when the Renovate config itself lands on `main`.

This repository runs Renovate self-hosted rather than through the hosted app: the job mints an installation token from the same GitHub App release-please uses and runs `renovatebot/github-action` against this repository only, reading [`renovate.json5`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/renovate.json5). The cron is the whole schedule - Renovate only looks at the repository when the workflow fires.

Three things in the workflow are load-bearing:

- The **package cache** under `/tmp/renovate/cache` is what makes an hourly cadence affordable; an uncached run re-resolves every module in the vendored tree. The repository cache stays disabled, because restoring stale pull request state makes Renovate abort with "Repository has changed during renovation".
- `RENOVATE_ALLOWED_COMMANDS` is the global half of the config's `postUpgradeTasks`: Renovate refuses to read allowed commands from a repository file, so the `make vendor-osv` the tasks run must be allowlisted here. A command that stops matching is skipped with a log warning, and the failure mode is a red pull request, not an error.
- Pull requests are authored by the app's own bot, not `renovate[bot]`, which is why the workflows that special-case bot authors ([commitlint](commitlint.md), [fix-vendor-osv](fix-vendor-osv.md)) match the `[bot]` suffix rather than a name.
