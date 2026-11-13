# Summary

[Introduction](index.md)

# Guides

- [Installing the provider](guides/install.md)
- [Installing the provider with Rancher Turtles](guides/rancher-turtles.md)
- [Node removal and the pre-terminate hook](guides/node-removal.md)

# Development

- [End-to-end tests](dev/e2e-tests.md)

# Decision records

- [The control plane owns the whole fleet](agent/choice-one-converge.md)
- [The SSH key is never written to a file](agent/choice-in-memory-ssh-key.md)
- [A tagged distro reference is not re-resolved](agent/choice-distro-pinning.md)
- [There is no rollout](agent/choice-no-rollout.md)
- [Tool versions live in go.mod, except golangci-lint](agent/choice-tool-dependencies.md)
- [The Zarf package is built from the release artifact](agent/choice-zarf-package.md)
- [A Makefile, not mage](agent/choice-makefile-not-mage.md)
