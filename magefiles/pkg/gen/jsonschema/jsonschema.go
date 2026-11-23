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

// Package jsonschema extracts each generated CRD's openAPIV3Schema into a standalone JSON Schema
// under schema/, one file per kind and version, for editors (yaml-language-server) and manifest
// validators (kubeconform) that read JSON Schema rather than CRDs.
//
// Nothing is translated: a structural schema already is a JSON Schema, written in Kubernetes'
// OpenAPI v3 dialect, and these CRDs use none of the parts that differ. What a file cannot carry
// is anything the API server enforces outside the schema -- the CEL x-kubernetes-validation
// rules ride along as annotations most validators ignore, and the Go-side checks on
// spec.clusterConfig appear nowhere -- so a document these schemas accept can still be refused
// by the API server.
package jsonschema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

// crdDirs is where `make manifests` writes the CRDs, relative to the repository root.
var crdDirs = []string{
	"bootstrap/config/crd/bases",
	"controlplane/config/crd/bases",
}

// outDir is where the schemas land, relative to the repository root. The file naming follows
// colonel-byte/cargoship's schema/ directory -- distro-v1alpha1-schema.json -- so the two
// repositories' schemas read the same way.
const outDir = "schema"

// Generate writes one <kind>-<version>-schema.json per CRD version into root's schema/
// directory, and deletes any *-schema.json there that no current CRD produces, so a renamed or
// removed kind does not leave a stale schema behind.
func Generate(root string) error {
	written := map[string]bool{}

	for _, dir := range crdDirs {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			return fmt.Errorf("unable to read the CRD directory: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			path := filepath.Join(root, dir, entry.Name())
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var crd apiextensionsv1.CustomResourceDefinition
			if err := yaml.UnmarshalStrict(raw, &crd); err != nil {
				return fmt.Errorf("%s: not a CustomResourceDefinition: %w", path, err)
			}
			for _, version := range crd.Spec.Versions {
				if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
					return fmt.Errorf("%s: version %s has no openAPIV3Schema", path, version.Name)
				}
				name := fmt.Sprintf("%s-%s-schema.json",
					strings.ToLower(crd.Spec.Names.Kind), version.Name)
				out, err := render(version.Schema.OpenAPIV3Schema)
				if err != nil {
					return fmt.Errorf("%s: version %s: %w", path, version.Name, err)
				}
				if err := os.MkdirAll(filepath.Join(root, outDir), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(root, outDir, name), out, 0o644); err != nil {
					return err
				}
				written[name] = true
			}
		}
	}
	if len(written) == 0 {
		return fmt.Errorf("no CRD produced a schema; run `make manifests` first")
	}

	// A schema whose kind went away must go with it: schema/ holds only generated files, so
	// anything matching the generated pattern and not written this run is stale.
	entries, err := os.ReadDir(filepath.Join(root, outDir))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), "-schema.json") || written[entry.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(root, outDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// render marshals one version's schema with a $schema header, pretty-printed and
// newline-terminated so the files diff cleanly.
//
// The header names JSON Schema draft 4 because that is the draft OpenAPI v3's dialect descends
// from, and what kubeconform's own converter stamps.
//
// shortcut: the schema is emitted as the CRD carries it, with no dialect translation -- nothing
// here uses x-kubernetes-int-or-string, the one construct that would need one; revisit if a
// field ever marshals to that.
func render(props *apiextensionsv1.JSONSchemaProps) ([]byte, error) {
	raw, err := json.Marshal(props)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	doc["$schema"] = "http://json-schema.org/draft-04/schema#"

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
