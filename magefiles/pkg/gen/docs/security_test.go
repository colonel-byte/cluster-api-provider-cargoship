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

package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityPageRewritesLinks(t *testing.T) {
	t.Parallel()

	root := book(t)
	if err := Generate(root); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	page, err := os.ReadFile(filepath.Join(root, "docs", "security.md"))
	if err != nil {
		t.Fatalf("reading the generated page: %v", err)
	}
	got := string(page)

	// Into the book: relative to docs/, since that is the generated page's own directory.
	if !strings.Contains(got, "](agent/choice-one-converge.md)") {
		t.Errorf("a link into docs/ was not made relative to docs/:\n%s", got)
	}
	// Out of the book: a page in the book cannot reach a file outside it, so it leaves.
	if !strings.Contains(got, "](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/cosign.pub)") {
		t.Errorf("a link outside docs/ was not made absolute:\n%s", got)
	}
	if !strings.HasPrefix(got, generatedBanner) {
		t.Errorf("the generated page carries no banner:\n%s", got)
	}
}

func TestSecurityPageFailsOnADeadLink(t *testing.T) {
	t.Parallel()

	root := book(t)
	// The shape of the failure this generator exists to catch: a decision record the policy
	// cites, which has since been renamed. `mdbook build` publishes the 404 without a word.
	write(t, filepath.Join(root, ".github", "SECURITY.md"),
		"# Security Policy\n\nSee [a record](../docs/agent/choice-renamed-away.md).\n")

	err := Generate(root)
	if err == nil {
		t.Fatal("a dead link was published rather than reported")
	}
	if !strings.Contains(err.Error(), "choice-renamed-away.md") {
		t.Errorf("the failure does not name the dead target: %v", err)
	}
}

func TestRebaseFromGitHubDirLeavesWhatItShould(t *testing.T) {
	t.Parallel()

	root := book(t)

	tests := []struct {
		name   string
		target string
		want   string
		ok     bool
	}{
		{
			name:   "an absolute URL is not a path in this repository",
			target: "https://docs.sigstore.dev/cosign/",
			want:   "https://docs.sigstore.dev/cosign/",
			ok:     true,
		},
		{
			name:   "a bare fragment resolves against the page either way",
			target: "#verifying-a-release",
			want:   "#verifying-a-release",
			ok:     true,
		},
		{
			name:   "a fragment on an in-book page travels with the rebased path",
			target: "../docs/agent/choice-one-converge.md#the-lock",
			want:   "agent/choice-one-converge.md#the-lock",
			ok:     true,
		},
		{
			name:   "a sibling in .github/ is outside the book",
			target: "SECURITY.md",
			want:   repoBlob + ".github/SECURITY.md",
			ok:     true,
		},
		{
			name:   "a target that does not exist does not resolve",
			target: "../docs/agent/gone.md",
			want:   "../docs/agent/gone.md",
			ok:     false,
		},
		{
			name:   "a server-rooted path resolves to neither base",
			target: "/etc/passwd",
			want:   "/etc/passwd",
			ok:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := rebaseFromGitHubDir(root, tt.target)
			if got != tt.want || ok != tt.ok {
				t.Errorf("rebaseFromGitHubDir(%q) = (%q, %v), want (%q, %v)", tt.target, got, ok, tt.want, tt.ok)
			}
		})
	}
}
