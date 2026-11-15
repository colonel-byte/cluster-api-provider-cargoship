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

package osv_test

import (
	"strings"
	"testing"

	"github.com/colonel-byte/cluster-api-provider-cargoship/magefiles/pkg/gen/osv"
)

// The override paths are relative to the repository root, because that is where mage and make
// run from.
const repoRoot = "../../../.."

func TestForeignManifest(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{
			name:     "an npm lockfile",
			filename: "package-lock.json",
			want:     true,
		},
		{
			name:     "a yarn lockfile",
			filename: "yarn.lock",
			want:     true,
		},
		{
			name:     "a plain requirements file",
			filename: "requirements.txt",
			want:     true,
		},
		{
			name:     "a suffixed requirements file",
			filename: "requirements-test.txt",
			want:     true,
		},
		{
			name:     "a maven pom",
			filename: "pom.xml",
			want:     true,
		},
		{
			name:     "a Go manifest osv-scanner should read",
			filename: "go.mod",
			want:     false,
		},
		{
			name:     "a Go source file",
			filename: "main.go",
			want:     false,
		},
		{
			name:     "an unrelated text file",
			filename: "test.txt",
			want:     false,
		},
	}

	for _, tt := range tests {
		if got := osv.ForeignManifest(tt.filename); got != tt.want {
			t.Errorf("%s: ForeignManifest(%q) = %v, want %v", tt.name, tt.filename, got, tt.want)
		}
	}
}

func TestOverrideContentNamesTheEcosystemAndItsLanguage(t *testing.T) {
	content := osv.OverrideContent(osv.Override{
		Dir:       "vendor/example.com/foo",
		Manifest:  "package-lock.json",
		Ecosystem: "npm",
		What:      "the dev dependencies of something else entirely",
	})

	for _, want := range []string{
		`ecosystem = "npm"`,
		"ignore = true",
		"JavaScript tooling vendored alongside a Go module",
		"package-lock.json here is the dev dependencies of something else entirely",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("OverrideContent() does not contain %q:\n%s", want, content)
		}
	}
}

// The overrides are checked in, so the entries have to describe the tree that is there. Both
// halves of VerifyVendor run against the real vendor/, which is the only tree that matters.
func TestVerifyVendorPassesAgainstTheCheckedInTree(t *testing.T) {
	t.Chdir(repoRoot)

	if err := osv.VerifyVendor(); err != nil {
		t.Errorf("VerifyVendor() against the repository root: %v", err)
	}
}
