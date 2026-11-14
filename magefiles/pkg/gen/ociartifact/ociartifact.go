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

// Package ociartifact packs the released component manifests into an OCI artifact, which is the
// third way the Cluster API Operator - and so Rancher Turtles - can install a provider: not from
// a release page, and not from ConfigMaps, but from a registry inside the air gap.
//
// See docs/guides/rancher-turtles.md for the consuming side.
package ociartifact

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
)

// Files are the manifests the artifact holds, named as the operator looks them up.
//
// These are the operator's *typed* names, and the choice matters. It looks for three spellings in
// order: `<type>-<crName>-<version>-components.yaml`, then `<type>-components.yaml`, then
// `components.yaml`. The first is tempting because one artifact could then hold several versions,
// but the name in the middle is the *custom resource's* name rather than the provider's -- a
// CAPIProvider called `cargoship-bootstrap` would have the operator looking for
// `bootstrap-cargoship-bootstrap-v0.1.0-components.yaml`, so the artifact would only work for
// consumers who happened to name their objects the way we guessed. The typed names depend on
// nothing but the provider type, and one artifact still covers both halves of this provider.
var Files = []string{
	"bootstrap-components.yaml",
	"control-plane-components.yaml",
	"metadata.yaml",
}

const (
	// layerMediaType is what each manifest is stored as. The operator reads layers by their
	// title annotation and never looks at the media type -- its own publisher pushes
	// `application/vnd.test.file` -- so this says what the bytes actually are.
	layerMediaType = "application/yaml"

	// artifactType marks the whole artifact as provider components, so that a registry listing
	// says what the thing is.
	artifactType = "application/vnd.cluster-api-operator.provider-components.v1"

	// sourceAnnotation is what makes a registry link the artifact back to this repository.
	sourceURL = "https://github.com/colonel-byte/cluster-api-provider-cargoship"
)

// Generate writes an OCI image layout under releaseDir, tagged with version, holding every file in
// Files. The layout is a directory a mirror can be filled from with
// `oras cp --from-oci-layout`; nothing here pushes, because the registry this is destined for is
// usually on the other side of an air gap from the machine that builds it.
func Generate(releaseDir, version string) error {
	if version == "" {
		return fmt.Errorf("no version given: pass the release tag, as `make oci-artifact TAG=v0.1.0` does")
	}

	layoutDir := filepath.Join(releaseDir, "oci")
	// Removed rather than added to: a layout that still holds the layers of a previous run would
	// publish manifests nobody asked for, and the digests of the current ones are no easier to
	// tell apart for being next to them.
	if err := os.RemoveAll(layoutDir); err != nil {
		return fmt.Errorf("unable to clear %s: %w", layoutDir, err)
	}

	store, err := oci.New(layoutDir)
	if err != nil {
		return fmt.Errorf("unable to create the oci layout in %s: %w", layoutDir, err)
	}

	ctx := context.Background()
	layers := make([]ocispec.Descriptor, 0, len(Files))
	for _, name := range Files {
		path := filepath.Join(releaseDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("unable to read %s; run `make release-manifests` first: %w", path, err)
		}

		desc := content.NewDescriptorFromBytes(layerMediaType, data)
		// The one thing the operator does read. It fetches each layer by the file name in this
		// annotation, which is also what `oras push` writes, so an artifact built here and one
		// built with the oras CLI are interchangeable.
		desc.Annotations = map[string]string{ocispec.AnnotationTitle: name}

		if err := store.Push(ctx, desc, bytes.NewReader(data)); err != nil {
			return fmt.Errorf("unable to add %s to the artifact: %w", name, err)
		}
		layers = append(layers, desc)
	}

	manifest, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, artifactType,
		oras.PackManifestOptions{
			Layers: layers,
			ManifestAnnotations: map[string]string{
				ocispec.AnnotationSource:  sourceURL,
				ocispec.AnnotationVersion: version,
			},
		})
	if err != nil {
		return fmt.Errorf("unable to pack the artifact: %w", err)
	}

	if err := store.Tag(ctx, manifest, version); err != nil {
		return fmt.Errorf("unable to tag the artifact %s: %w", version, err)
	}

	// oras-go stages blobs in here on the way in and leaves the directory behind. It is not part
	// of the image layout, and an artifact that travels as a tar should not carry it.
	if err := os.RemoveAll(filepath.Join(layoutDir, "ingest")); err != nil {
		return fmt.Errorf("unable to clear the staging directory: %w", err)
	}

	fmt.Printf("%s:%s\n", layoutDir, version)
	return nil
}
