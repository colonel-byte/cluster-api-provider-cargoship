// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build mage
// +build mage

// The generators this repository runs, as mage targets.
//
// Why both tools: the three target names Cluster API's tooling depends on -- `make generate`,
// `make manifests`, `make release-manifests` -- are an interface to the ecosystem and stay in the
// Makefile, which calls these. Everything with real logic in it lives here, in Go, where it can be
// tested and read. See docs/agent/choice-makefile-not-mage.md.
//
// There is deliberately no mage target for anything the Makefile already does in one command.
// `controller-gen` with flags is a Makefile recipe; a generator that has to read the docs tree and
// decide things is not.
package main

import (
	"github.com/colonel-byte/cluster-api-provider-cargoship/magefiles/pkg/gen/docs"
	"github.com/magefile/mage/mg"
)

// Generate holds the generator targets.
type Generate mg.Namespace

// Document writes the book's landing page and its table of contents.
func (Generate) Document() error {
	// The repository root: mage runs from it, because the Makefile does.
	return docs.Generate(".")
}
