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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// securitySource is the hand-written security policy, and its path is fixed by GitHub: that is
// where the repository's Security tab reads the policy from, so the file cannot move into docs/
// and be copied out. The copy goes the other way instead.
const securitySource = ".github/SECURITY.md"

// repoBlob is the prefix for a link to a file in the repository rather than in the book. A page
// in the book can only reach another page in the book, so anything the policy links outside
// docs/ -- the license key, the AI disclosure, a workflow -- becomes a link back to GitHub.
const repoBlob = "https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/"

// inlineLink matches the target of a Markdown inline link or image, with its optional title.
// Reference-style links and autolinks are not matched, and the policy uses neither.
var inlineLink = regexp.MustCompile(`(!?\]\()([^)\s]+)((?:\s+"[^"]*")?\))`)

// securityPage copies .github/SECURITY.md to docs/security.md, rewriting its links.
//
// A copy rather than a symlink, for the same reason the landing page is one, plus a second: the
// two readers resolve a relative link against different directories. GitHub resolves one against
// .github/, where the file lives; mdBook resolves it against docs/, because book.toml sets
// src = "docs" and the page is served from the site root. No single relative path satisfies both,
// so one of them has to be generated.
//
// A link whose target does not exist fails the run rather than being published. That is the point
// of doing this in Go at all: `mdbook build` does not fail on a dead relative link, so a policy
// that cites a decision record which has since been renamed would publish a 404 and say nothing.
func securityPage(root string) error {
	content, err := os.ReadFile(filepath.Join(root, securitySource))
	if err != nil {
		return fmt.Errorf("unable to read %s: %w", securitySource, err)
	}

	var dead []string
	page := inlineLink.ReplaceAllStringFunc(string(content), func(match string) string {
		parts := inlineLink.FindStringSubmatch(match)
		open, target, tail := parts[1], parts[2], parts[3]

		rebased, ok := rebaseFromGitHubDir(root, target)
		if !ok {
			dead = append(dead, target)
			return match
		}
		return open + rebased + tail
	})

	if len(dead) > 0 {
		sort.Strings(dead)
		return fmt.Errorf("%s links to %s, which does not exist: fix the link, or the target moved and nothing updated it",
			securitySource, strings.Join(dead, ", "))
	}

	out := filepath.Join(root, "docs", "security.md")
	fmt.Println(filepath.ToSlash(filepath.Join("docs", "security.md")))
	return os.WriteFile(out, []byte(generatedBanner+"\n\n"+page), 0o644)
}

// rebaseFromGitHubDir rewrites one of the policy's link targets for the generated copy, and
// reports whether it resolves. The target is relative to .github/, which is where the source is.
//
// A target that is not a path in this repository -- an absolute URL, a bare fragment -- is left
// exactly as it is, because its meaning does not depend on which directory the page sits in.
func rebaseFromGitHubDir(root, target string) (string, bool) {
	switch {
	case strings.HasPrefix(target, "#"):
		// A fragment resolves against the page itself either way.
		return target, true
	case strings.Contains(target, "://"), strings.HasPrefix(target, "//"), strings.HasPrefix(target, "mailto:"):
		return target, true
	case target == "", strings.HasPrefix(target, "/"):
		return target, false
	}

	// The fragment travels with the link but is not part of the path being checked.
	path, fragment := target, ""
	if i := strings.Index(path, "#"); i >= 0 {
		path, fragment = path[:i], path[i:]
	}

	repoPath := filepath.Join(".github", path)
	if _, err := os.Stat(filepath.Join(root, repoPath)); err != nil {
		return target, false
	}

	// Inside the book: make it relative to docs/, since that is the page's own directory.
	// mdBook rewrites a .md target to .html itself, so the extension is left alone.
	if rel, inside := strings.CutPrefix(filepath.ToSlash(repoPath), "docs/"); inside {
		return rel + fragment, true
	}

	// Outside the book, so it has to leave the book. The fragment is dropped on purpose for a
	// non-Markdown target, but kept: GitHub honours one on a rendered Markdown file, and
	// ignores it harmlessly otherwise.
	return repoBlob + filepath.ToSlash(repoPath) + fragment, true
}
