// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cluster-api-provider-cargoship/magefiles/pkg/util"
)

func TestCheckMarkdownTables(t *testing.T) {
	tests := []struct {
		name  string
		doc   string
		want  []int    // 1-indexed lines expected to be reported
		wants []string // the padded form of each reported line, in the same order
	}{
		{
			name: "padded table passes",
			doc: strings.Join([]string{
				"| Path   | What it holds |",
				"| ------ | ------------- |",
				"| `pkg/` | Packages      |",
			}, "\n"),
			want: nil,
		},
		{
			name: "unpadded cells and delimiter are reported",
			doc: strings.Join([]string{
				"| Path | What it holds |",
				"| --- | --- |",
				"| `pkg/` | Packages |",
			}, "\n"),
			want: []int{1, 2, 3},
			wants: []string{
				"| Path   | What it holds |",
				"| ------ | ------------- |",
				"| `pkg/` | Packages      |",
			},
		},
		{
			name: "over-padded column is reported",
			doc: strings.Join([]string{
				"| Path     | What it holds |",
				"| -------- | ------------- |",
				"| `pkg/`   | Packages      |",
			}, "\n"),
			want: []int{1, 2, 3},
			wants: []string{
				"| Path   | What it holds |",
				"| ------ | ------------- |",
				"| `pkg/` | Packages      |",
			},
		},
		{
			name: "table inside a fenced block is skipped",
			doc: strings.Join([]string{
				"Write this:",
				"",
				"```markdown",
				"| Path | What it holds |",
				"| --- | --- |",
				"```",
			}, "\n"),
			want: nil,
		},
		{
			name: "escaped pipe counts as cell content",
			doc: strings.Join([]string{
				"| Flag  | Meaning    |",
				"| ----- | ---------- |",
				"| `a\\|b` | either one |",
			}, "\n"),
			want: []int{1, 2},
			wants: []string{
				"| Flag   | Meaning    |",
				"| ------ | ---------- |",
			},
		},
		{
			name: "alignment colons are not a false positive",
			doc: strings.Join([]string{
				"| Path | Count |",
				"| :--- | ----: |",
				"| `a`  | 1     |",
			}, "\n"),
			want: nil,
		},
		{
			name: "pipe lines without a delimiter row are not a table",
			doc: strings.Join([]string{
				"| not | a table |",
				"| still | not |",
			}, "\n"),
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "doc.md")
			if err := os.WriteFile(path, []byte(tt.doc+"\n"), 0o600); err != nil {
				t.Fatalf("writing %s: %v", path, err)
			}

			findings, err := util.CheckMarkdownTables([]string{path})
			if err != nil {
				t.Fatalf("CheckMarkdownTables: %v", err)
			}

			if len(findings) != len(tt.want) {
				for _, f := range findings {
					t.Logf("line %d: got %q, want %q", f.Line, f.Got, f.Want)
				}
				t.Fatalf("got %d findings, want %d", len(findings), len(tt.want))
			}

			for i, f := range findings {
				if f.Line != tt.want[i] {
					t.Errorf("finding %d is on line %d, want line %d", i, f.Line, tt.want[i])
				}
				if f.Want != tt.wants[i] {
					t.Errorf("finding %d wants\n%s\nexpected\n%s", i, f.Want, tt.wants[i])
				}
			}
		})
	}
}
