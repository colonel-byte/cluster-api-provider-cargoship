# dependabot validate

Source: [`dependabot-validate.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/dependabot-validate.yaml). Runs on a pull request that touches `.github/dependabot.yaml` or this workflow.

Validates `.github/dependabot.yaml` against its schema and posts the result as a sticky pull request comment.

It exists because a malformed Dependabot config fails nothing on the pull request that introduced it. Dependabot simply stops opening updates, and the first symptom is their absence - which is not something anybody notices on a schedule. Validating the file on the change that touches it is the only point at which the mistake is cheap.
