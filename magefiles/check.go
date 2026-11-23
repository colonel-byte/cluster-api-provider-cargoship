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

// The checks that read the tree and report on it rather than writing to it. A generator belongs
// in generate.go instead; these are separate because `make verify` can run a check against a
// clean tree, and cannot run a generator without one.
package main

import (
	"github.com/colonel-byte/cluster-api-provider-cargoship/magefiles/pkg/util"
	"github.com/magefile/mage/mg"
)

// Check holds the targets that report on the tree without changing it.
type Check mg.Namespace

// Tables fails if a Markdown table anywhere in the tree is not padded the way the style guide in
// AGENTS.md requires, printing the expected form of every line that is not.
//
// A check rather than a formatter, which is the whole reason this is worth having: the padding is
// part of the content -- adding a wider row repads the column -- so a tool that rewrote it would
// silently reflow tables in a commit that was about something else. This prints what to write and
// leaves the writing to a person.
func (Check) Tables() error {
	return util.CheckRepoMarkdownTables()
}
