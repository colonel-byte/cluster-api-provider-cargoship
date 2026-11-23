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

package jsonschema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimalCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.io
spec:
  group: example.io
  names:
    kind: Widget
    listKind: WidgetList
    plural: widgets
    singular: widget
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                size:
                  type: integer
`

// root lays out a fake repository with one CRD in each generated directory.
func root(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, crdDir := range crdDirs {
		if err := os.MkdirAll(filepath.Join(dir, crdDir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, crdDirs[0], "example.io_widgets.yaml")
	if err := os.WriteFile(path, []byte(minimalCRD), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGenerateWritesOneSchemaPerKindAndVersion(t *testing.T) {
	dir := root(t)
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, outDir, "widget-v1alpha1-schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["$schema"] != "http://json-schema.org/draft-04/schema#" {
		t.Fatalf("$schema = %v", doc["$schema"])
	}
	if doc["type"] != "object" {
		t.Fatalf("type = %v, the openAPIV3Schema was not carried over", doc["type"])
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatal("the file does not end with a newline")
	}
}

func TestGenerateRemovesASchemaWhoseKindWentAway(t *testing.T) {
	dir := root(t)
	stale := filepath.Join(dir, outDir, "gadget-v1alpha1-schema.json")
	if err := os.MkdirAll(filepath.Join(dir, outDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(dir, outDir, "README.md")
	if err := os.WriteFile(kept, []byte("hand-written\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("the stale schema was not removed")
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatal("a file outside the generated pattern was removed")
	}
}

func TestGenerateRefusesAnEmptyTree(t *testing.T) {
	dir := t.TempDir()
	for _, crdDir := range crdDirs {
		if err := os.MkdirAll(filepath.Join(dir, crdDir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := Generate(dir); err == nil {
		t.Fatal("want an error when no CRD produced a schema")
	}
}
