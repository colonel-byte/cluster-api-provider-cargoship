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

package ociartifact

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
)

// release writes what `make release-manifests` leaves behind.
func release(t *testing.T, files ...string) string {
	t.Helper()

	dir := t.TempDir()
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# "+name+"\n"), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}

func TestGenerate(t *testing.T) {
	t.Parallel()

	dir := release(t, Files...)
	if err := Generate(dir, "v0.1.0"); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	store, err := oci.New(filepath.Join(dir, "oci"))
	if err != nil {
		t.Fatalf("reading the layout back: %v", err)
	}

	ctx := context.Background()
	// Resolving by tag is what `oras cp --from-oci-layout <dir>:<tag>` does, so a layout whose tag
	// is missing is one nobody can copy out of however good its layers are.
	desc, err := store.Resolve(ctx, "v0.1.0")
	if err != nil {
		t.Fatalf("resolving the tag: %v", err)
	}

	raw, err := content.FetchAll(ctx, store, desc)
	if err != nil {
		t.Fatalf("fetching the manifest: %v", err)
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parsing the manifest: %v", err)
	}

	// The title annotation is the only thing the operator reads a layer by.
	titles := map[string]bool{}
	for _, layer := range manifest.Layers {
		titles[layer.Annotations[ocispec.AnnotationTitle]] = true
	}
	for _, name := range Files {
		if !titles[name] {
			t.Errorf("no layer titled %q; the operator would not find it, titles are %v", name, titles)
		}
	}
	if manifest.ArtifactType != artifactType {
		t.Errorf("artifact type = %q, want %q", manifest.ArtifactType, artifactType)
	}
	if manifest.Annotations[ocispec.AnnotationVersion] != "v0.1.0" {
		t.Errorf("version annotation = %q, want v0.1.0", manifest.Annotations[ocispec.AnnotationVersion])
	}
}

func TestGenerateSaysWhichManifestIsMissing(t *testing.T) {
	t.Parallel()

	// The state of a tree where `make release-manifests` has not run, or has run for one provider.
	dir := release(t, "bootstrap-components.yaml", "metadata.yaml")

	err := Generate(dir, "v0.1.0")
	if err == nil {
		t.Fatal("packed an artifact missing control-plane-components.yaml")
	}
	if !strings.Contains(err.Error(), "control-plane-components.yaml") {
		t.Errorf("error does not name the missing file: %v", err)
	}
	if !strings.Contains(err.Error(), "make release-manifests") {
		t.Errorf("error does not say how to produce it: %v", err)
	}
}

func TestGenerateRefusesAnEmptyVersion(t *testing.T) {
	t.Parallel()

	// An artifact with no tag cannot be copied out of the layout, and the operator resolves the
	// provider version as the tag when fetchConfig.oci carries none.
	if err := Generate(release(t, Files...), ""); err == nil {
		t.Error("packed an untagged artifact")
	}
}
