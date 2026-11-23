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

package converge

import (
	"reflect"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
)

func TestApplyValuesFrom(t *testing.T) {
	tests := []struct {
		name       string
		inline     map[string]any
		sources    []ValuesSource
		want       map[string]any
		wantErr    string
		wantNoHash bool
	}{
		{
			name:       "no sources changes nothing",
			inline:     map[string]any{"a": "inline"},
			sources:    nil,
			want:       map[string]any{"a": "inline"},
			wantNoHash: true,
		},
		{
			name: "a document merges whole",
			sources: []ValuesSource{
				{
					Name:    "secret ns/creds",
					Payload: []byte("addons:\n  vsphere:\n    auth:\n      user: admin\n      pass: hunter2\n"),
				},
			},
			want: map[string]any{
				"addons": map[string]any{
					"vsphere": map[string]any{
						"auth": map[string]any{
							"user": "admin",
							"pass": "hunter2",
						},
					},
				},
			},
		},
		{
			name: "later sources win over earlier",
			sources: []ValuesSource{
				{
					Name:    "first",
					Payload: []byte("a: one\nnested:\n  keep: kept\n  move: old\n"),
				},
				{
					Name:    "second",
					Payload: []byte("nested:\n  move: new\n"),
				},
			},
			want: map[string]any{
				"a": "one",
				"nested": map[string]any{
					"keep": "kept",
					"move": "new",
				},
			},
		},
		{
			name:   "inline values win over every source",
			inline: map[string]any{"a": "inline"},
			sources: []ValuesSource{
				{
					Name:    "source",
					Payload: []byte("a: fromSecret\nb: kept\n"),
				},
			},
			want: map[string]any{
				"a": "inline",
				"b": "kept",
			},
		},
		{
			name: "targetPath sets one trimmed value",
			sources: []ValuesSource{
				{
					Name:       "secret ns/pass",
					Payload:    []byte("hunter2\n"),
					TargetPath: "addons.vsphere.auth.pass",
				},
			},
			want: map[string]any{
				"addons": map[string]any{
					"vsphere": map[string]any{
						"auth": map[string]any{
							"pass": "hunter2",
						},
					},
				},
			},
		},
		{
			name: "targetPath crossing a non-map value is refused",
			sources: []ValuesSource{
				{
					Name:    "doc",
					Payload: []byte("a: scalar\n"),
				},
				{
					Name:       "value",
					Payload:    []byte("x"),
					TargetPath: "a.b",
				},
			},
			wantErr: "crosses a non-map value",
		},
		{
			name: "a payload that is not a mapping is refused",
			sources: []ValuesSource{
				{
					Name:    "secret ns/creds",
					Payload: []byte("- just\n- a\n- list\n"),
				},
			},
			wantErr: "not a YAML mapping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := cluster.Config{Values: tt.inline}
			hash, err := ApplyValuesFrom(&cfg, tt.sources)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.Values, tt.want) {
				t.Fatalf("merged values:\n got %#v\nwant %#v", cfg.Values, tt.want)
			}
			if tt.wantNoHash != (hash == "") {
				t.Fatalf("hash %q, wantNoHash=%v", hash, tt.wantNoHash)
			}
		})
	}
}

func TestApplyValuesFromHashMovesWithASource(t *testing.T) {
	hashOf := func(payload string) string {
		cfg := cluster.Config{}
		hash, err := ApplyValuesFrom(&cfg, []ValuesSource{
			{
				Name:    "secret",
				Payload: []byte(payload),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return hash
	}
	first := hashOf("a: one\n")
	if first == hashOf("a: two\n") {
		t.Fatal("a rotated source must change the hash")
	}
	if again := hashOf("a: one\n"); first != again {
		t.Fatal("the same source must hash the same")
	}
}
