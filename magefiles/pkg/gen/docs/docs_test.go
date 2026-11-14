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

// write creates a file and the directories above it.
func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// book lays out a repository with one page in each chapter, plus the two the ordering and
// fallback assertions need.
func book(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	write(t, filepath.Join(root, "README.md"),
		"# provider\n\nSee [the guides](docs/guides/install.md) and the [license](LICENSE).\n")
	write(t, filepath.Join(root, "docs", "guides", "install.md"), "# Installing the provider\n")
	write(t, filepath.Join(root, "docs", "dev", "e2e-tests.md"), "# End-to-end tests\n")
	write(t, filepath.Join(root, "docs", "agent", "choice-one-converge.md"),
		"# The control plane owns the whole fleet\n")
	// Its heading sorts after the other record's and its file name sorts before: the order comes
	// from the path, so that two runs on the same tree cannot disagree.
	write(t, filepath.Join(root, "docs", "agent", "choice-a-record.md"), "# Zebra\n")
	// No heading at all, which changes nothing: the label is the file name either way.
	write(t, filepath.Join(root, "docs", "agent", "todo-untitled.md"), "Some prose, no heading.\n")
	return root
}

func TestGenerateSummary(t *testing.T) {
	t.Parallel()

	root := book(t)
	if err := Generate(root); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "docs", "SUMMARY.md"))
	if err != nil {
		t.Fatalf("reading the summary: %v", err)
	}
	got := string(raw)

	want := generatedBanner + "\n\n" +
		"# Summary\n\n" +
		"[Introduction](index.md)\n" +
		"\n# Guides\n\n" +
		"- [install](guides/install.md)\n" +
		"\n# Development\n\n" +
		"- [e2e-tests](dev/e2e-tests.md)\n" +
		"\n# Decision records\n\n" +
		"- [choice-a-record](agent/choice-a-record.md)\n" +
		"- [choice-one-converge](agent/choice-one-converge.md)\n" +
		"- [todo-untitled](agent/todo-untitled.md)\n"

	if got != want {
		t.Errorf("summary mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGenerateSummaryOmitsAnEmptyChapter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, filepath.Join(root, "README.md"), "# provider\n")
	write(t, filepath.Join(root, "docs", "guides", "install.md"), "# Installing the provider\n")

	if err := Generate(root); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "docs", "SUMMARY.md"))
	if err != nil {
		t.Fatalf("reading the summary: %v", err)
	}
	// An empty part heading is a sidebar section with nothing under it, which reads as a book
	// with a missing chapter rather than a book without one.
	if strings.Contains(string(raw), "# Development") {
		t.Errorf("wrote a heading for a chapter with no pages:\n%s", raw)
	}
}

func TestLandingPageRewritesLinks(t *testing.T) {
	t.Parallel()

	root := book(t)
	if err := Generate(root); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "docs", "index.md"))
	if err != nil {
		t.Fatalf("reading the landing page: %v", err)
	}
	got := string(raw)

	// The copy is read from docs/, so a `docs/` prefix would resolve one level too deep and the
	// link would be dead in the published book, silently.
	if !strings.Contains(got, "](guides/install.md)") {
		t.Errorf("the docs/ prefix was not stripped:\n%s", got)
	}
	// The license is not in the book at all, so a relative link to it cannot work.
	if !strings.Contains(got, licenseAbsolute) {
		t.Errorf("the license link was not made absolute:\n%s", got)
	}
}
