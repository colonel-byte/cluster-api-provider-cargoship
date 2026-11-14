# Copyright 2026 colonel-byte
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Make for the targets the ecosystem names, mage for the generators behind them.
#
# Every Cluster API provider is driven by `make generate`, `make manifests` and
# `make release-manifests`; the contributing guide, the provider implementers' documentation and
# clusterctl's own release tooling all assume those targets exist, so they stay here. A generator
# with real logic in it lives in magefiles/ instead, in Go, where it can be tested -- and this
# Makefile calls it. See docs/agent/choice-makefile-not-mage.md.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help

# Tool versions are pinned in go.mod as tool dependencies, so Dependabot owns them and
# `go tool` resolves them without a network fetch at build time. See
# docs/agent/choice-tool-dependencies.md.
CONTROLLER_GEN := go tool controller-gen
KUSTOMIZE := go tool kustomize
ENVTEST := go tool setup-envtest
# mage is pinned the same way, so `make docs` needs nothing installed. It compiles magefiles/
# itself, which is why the generators there can be ordinary Go packages with tests.
MAGE := go tool mage

# golangci-lint is deliberately NOT a tool dependency. Adding it to go.mod drags k8s.io/* up to a
# patch release that wants structured-merge-diff/v7 while cluster-api pins v6, and the module
# stops building. It is run through the pre-commit hooks instead, which is also how
# colonel-byte/cargoship runs it. See docs/agent/choice-tool-dependencies.md.

ENVTEST_K8S_VERSION ?= 1.34.x
RELEASE_DIR ?= out
TAG ?= dev
REGISTRY ?= ghcr.io/colonel-byte

BOOTSTRAP_IMG := $(REGISTRY)/cluster-api-cargoship-bootstrap-controller:$(TAG)
CONTROLPLANE_IMG := $(REGISTRY)/cluster-api-cargoship-controlplane-controller:$(TAG)

##@ General

.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Generate

.PHONY: generate
generate: generate-deepcopy manifests docs ## Run every generator.

.PHONY: generate-deepcopy
generate-deepcopy: ## Regenerate zz_generated.deepcopy.go.
	$(CONTROLLER_GEN) object:headerFile=hack/boilerplate.go.txt \
		paths=./bootstrap/api/... paths=./controlplane/api/...

.PHONY: docs
docs: ## Write the book's landing page and its table of contents.
	# Both pages are generated: index.md is README.md with its links rewritten, and SUMMARY.md is
	# built from whatever pages are on disk, because a page that is added and not listed there is
	# simply missing from the book and mdBook does not say so. The reasoning for each is on the
	# functions in magefiles/pkg/gen/docs.
	$(MAGE) generate:document

.PHONY: manifests
manifests: ## Regenerate the CRDs and RBAC roles.
	$(CONTROLLER_GEN) crd rbac:roleName=manager-role paths=./bootstrap/... \
		output:crd:artifacts:config=bootstrap/config/crd/bases \
		output:rbac:artifacts:config=bootstrap/config/rbac
	$(CONTROLLER_GEN) crd rbac:roleName=manager-role paths=./controlplane/... \
		output:crd:artifacts:config=controlplane/config/crd/bases \
		output:rbac:artifacts:config=controlplane/config/rbac

##@ Test

.PHONY: test
test: ## Run the unit tests.
	go test -race -coverprofile=cover.out ./...

.PHONY: test-envtest
test-envtest: ## Run the tests that need a real API server.
	KUBEBUILDER_ASSETS="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" \
		go test -race -tags=envtest ./...

.PHONY: lint
lint: ## Run go vet. golangci-lint comes from the pre-commit hooks, not from go.mod.
	go vet ./...

# Scoped to the paths the generators write, not the whole tree. A bare `git diff` here would
# fail on any uncommitted edit and report it as "generated files are out of date", which sends
# the reader to the wrong place.
GENERATED_PATHS := \
	'**/zz_generated.deepcopy.go' \
	bootstrap/config \
	controlplane/config \
	docs/index.md \
	docs/SUMMARY.md

.PHONY: verify
verify: generate ## Fail if a generator would change a checked-in file.
	@if ! git diff --exit-code --quiet -- $(GENERATED_PATHS); then \
		echo "generated files are out of date; run 'make generate' and commit the result"; \
		git diff --stat -- $(GENERATED_PATHS); \
		exit 1; \
	fi

##@ Build

.PHONY: build
build: ## Build both manager binaries.
	go build -o bin/bootstrap-manager ./bootstrap/cmd/manager
	go build -o bin/controlplane-manager ./controlplane/cmd/manager

.PHONY: docker-build
docker-build: ## Build both controller images.
	# The binaries go to dist/<os>/<arch>/ and the build context is dist/, because the Dockerfile
	# copies from ${TARGETPLATFORM}/${MANAGER} -- which is how goreleaser lays its docker context
	# out, so this builds the same image the release does. Getting it wrong fails with
	# `"/linux/amd64/<manager>": not found`, which names the path and not the mistake.
	mkdir -p dist/linux/amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/linux/amd64/bootstrap-manager ./bootstrap/cmd/manager
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/linux/amd64/controlplane-manager ./controlplane/cmd/manager
	docker build -f containers/manager/Dockerfile --build-arg MANAGER=bootstrap-manager -t $(BOOTSTRAP_IMG) dist
	docker build -f containers/manager/Dockerfile --build-arg MANAGER=controlplane-manager -t $(CONTROLPLANE_IMG) dist

##@ Release

.PHONY: release-manifests
release-manifests: manifests ## Render the component manifests clusterctl installs.
	mkdir -p $(RELEASE_DIR)
	cd bootstrap/config/manager && $(KUSTOMIZE) edit set image controller=$(BOOTSTRAP_IMG)
	cd controlplane/config/manager && $(KUSTOMIZE) edit set image controller=$(CONTROLPLANE_IMG)
	$(KUSTOMIZE) build bootstrap/config/default > $(RELEASE_DIR)/bootstrap-components.yaml
	$(KUSTOMIZE) build controlplane/config/default > $(RELEASE_DIR)/control-plane-components.yaml
	cp metadata.yaml $(RELEASE_DIR)/metadata.yaml

.PHONY: oci-artifact
oci-artifact: release-manifests ## Pack the component manifests into an OCI artifact for the Cluster API Operator.
	# The third way the operator can install a provider, after a release page and ConfigMaps, and
	# the one an air gap with a registry wants. A layout directory rather than a push: the registry
	# this is destined for is usually on the other side of the air gap from the machine that builds
	# it, so filling it is `oras cp --from-oci-layout` on the inside. See
	# docs/guides/rancher-turtles.md.
	$(MAGE) generate:ociArtifact $(RELEASE_DIR) $(TAG)
	# Tarred from inside the layout rather than from above it: `--from-oci-layout` on a tar looks
	# for oci-layout and index.json at the root of the archive, and a one-directory prefix makes it
	# report the layout as invalid.
	tar -C $(RELEASE_DIR)/oci -cf $(RELEASE_DIR)/cargoship-capi-providers-$(TAG).oci.tar .

# The version the Zarf package pins, read back out of zarf.yaml so that `make zarf-package`
# builds the component manifests at the tag the package's images already name. Overriding TAG
# here would produce a package whose manifests and images disagree, which is why the target
# does not take one.
ZARF_VERSION := $(shell sed -n 's/^  version: \([^ ]*\).*/\1/p' zarf.yaml)

.PHONY: zarf-package
zarf-package: ## Build the air-gapped Zarf package. Needs the release's images to be published.
	$(MAKE) release-manifests TAG=$(ZARF_VERSION)
	zarf package create . --confirm --output $(RELEASE_DIR)

.PHONY: clean
clean: ## Remove build output.
	rm -rf bin dist $(RELEASE_DIR) cover.out
