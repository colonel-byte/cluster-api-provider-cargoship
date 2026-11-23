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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"sigs.k8s.io/yaml"
)

// ValuesSource is one resolved valuesFrom entry: the referenced key's content, already read out
// of its ConfigMap or Secret by the caller, so this package stays free of API clients.
type ValuesSource struct {
	// Name identifies the source in errors, e.g. `secret ns/vsphere-creds key "values.yaml"`.
	Name string
	// Payload is the referenced key's content: a YAML mapping, or one value when TargetPath is
	// set.
	Payload []byte
	// TargetPath, when set, is the dotted path Payload is set at as a single string value.
	TargetPath string
}

// ApplyValuesFrom merges the sources into cfg.Values, in memory, before the config reaches an
// apply.
//
// The precedence is a Flux HelmRelease's: sources merge in order, later over earlier, and the
// values already inline in cfg -- spec.clusterConfig.values as written -- win over all of them,
// so a config that worked keeps working when a source is added beside it.
//
// It returns the hex SHA-256 of the merged values, so a caller can fold "a referenced secret
// rotated" into the desired-state fingerprint without any value itself sitting in a fingerprint
// input. Empty sources merge nothing and hash to "", which keeps every fingerprint that does not
// use valuesFrom exactly as it was.
func ApplyValuesFrom(cfg *cluster.Config, sources []ValuesSource) (string, error) {
	if len(sources) == 0 {
		return "", nil
	}

	merged := map[string]any{}
	for _, s := range sources {
		if s.TargetPath != "" {
			// One value, the way Flux treats targetPath: the content is the value, as a
			// string, trimmed of the trailing newline every `kubectl create secret
			// --from-literal` sibling tool leaves behind.
			if err := setAtPath(merged, s.TargetPath, strings.TrimSpace(string(s.Payload))); err != nil {
				return "", fmt.Errorf("%s: %w", s.Name, err)
			}
			continue
		}
		var doc map[string]any
		if err := yaml.Unmarshal(s.Payload, &doc); err != nil {
			return "", fmt.Errorf("%s: not a YAML mapping: %w", s.Name, err)
		}
		deepMerge(merged, doc)
	}
	deepMerge(merged, cfg.Values)
	cfg.Values = merged

	// json.Marshal sorts map keys, so the same values hash the same whatever order they merged
	// in.
	b, err := json.Marshal(merged)
	if err != nil {
		return "", fmt.Errorf("unable to hash the merged values: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// deepMerge folds src into dst, src winning: maps merge recursively, everything else replaces.
func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if sv, ok := v.(map[string]any); ok {
			if dv, ok := dst[k].(map[string]any); ok {
				deepMerge(dv, sv)
				continue
			}
		}
		dst[k] = v
	}
}

// setAtPath sets value at a dotted path, creating maps along the way. A path crossing an
// existing non-map value is an error rather than an overwrite, because the collision means two
// sources disagree about the shape of the tree.
//
// shortcut: plain dotted keys only -- no list indices and no escape for a key containing a dot;
// upgrade to a real path parser when a package's values need one.
func setAtPath(m map[string]any, path, value string) error {
	keys := strings.Split(path, ".")
	for _, k := range keys {
		if k == "" {
			return fmt.Errorf("targetPath %q has an empty segment", path)
		}
	}
	for _, k := range keys[:len(keys)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			if _, exists := m[k]; exists {
				return fmt.Errorf("targetPath %q crosses a non-map value at %q", path, k)
			}
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	m[keys[len(keys)-1]] = value
	return nil
}
