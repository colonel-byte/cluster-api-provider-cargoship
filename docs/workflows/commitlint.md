# PR Title Check

Source: [`commitlint.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/commitlint.yaml). Runs on a pull request being opened, reopened, edited, synchronized or milestoned, and on the merge queue.

Lints the **pull request title**, not the individual commits, because release-please squash-merges and the title becomes the final commit message - which is what release-please then reads to decide the next version and write the changelog. A title that is not a conventional commit produces a release that is wrong or missing. `wagoid/commitlint-github-action` lints commit messages instead, so it is not a drop-in replacement.

The config lives in [`.github/commitlint/`](https://github.com/colonel-byte/cluster-api-provider-cargoship/tree/main/.github/commitlint) and is installed with `npm ci` from a committed lockfile. Dependabot gets `.commitlintrc.dependabot.yaml`, the same config with `header-max-length` off: it cannot shorten a module path, and ten of the modules in `go.mod` overflow a 100-character title.

On the `merge_group` event every step is skipped and the job reports success. That is deliberate rather than a gap: there is no pull request under that event, the title was already linted on the pull request that got queued, and `title_check` is a required status check that the merge queue waits for a result from.

`milestoned` is in the trigger list as a workaround for release-please's own pull requests not firing pull request workflows; adding one to a milestone fires this.
