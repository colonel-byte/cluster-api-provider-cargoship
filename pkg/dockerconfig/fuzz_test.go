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

package dockerconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// maxEntries bounds the file the fuzz target accumulates. Every iteration adds the registries its
// payload names, and a fuzzing run is hundreds of thousands of iterations; the file is emptied once
// it is past this, which starts the invariant over rather than weakening it.
const maxEntries = 64

// FuzzMerge drives the read-modify-write with payloads from a pull secret, which is the only state
// this package keeps. The file is shared by every CargoshipControlPlane in the process, so the
// property that matters is that a merge for one cluster's registry never damages another's:
//
//   - the file on disk always parses, so a pull that reads it mid-reconcile finds a usable store
//   - an entry written earlier is still there, byte for byte, unless this payload named it
//   - a payload Merge accepted has every registry it named in the file afterwards
//
// Payloads accumulate across iterations on purpose: a single Merge against an empty file would
// exercise none of the merging.
func FuzzMerge(f *testing.F) {
	f.Add([]byte(`{"auths":{"ghcr.io":{"auth":"Z2g6b25l"}}}`))
	f.Add([]byte(`{"auths":{"registry.example:5000":{"username":"u","password":"p"}}}`))
	f.Add([]byte(`{"auths":{}}`))
	f.Add([]byte(`{"auths":null}`))
	f.Add([]byte(`{"auths":{"ghcr.io":null}}`))
	f.Add([]byte(`not json`))

	dir := filepath.Join(f.TempDir(), "docker")
	f.Setenv(EnvVar, "")
	if err := Use(dir); err != nil {
		f.Fatalf("Use(%q) = %v, want no error", dir, err)
	}
	path := filepath.Join(dir, fileName)

	// What the file is known to hold, kept outside the iteration so one payload's merge is checked
	// against the entries the payloads before it wrote.
	expected := map[string]json.RawMessage{}

	f.Fuzz(func(t *testing.T, payload []byte) {
		// Read before merging, so a registry the payload names can be told from one it did not
		// touch. A payload Merge rejects leaves this unused.
		incoming := &config{}
		if parsed, err := parse(payload); err == nil {
			incoming = parsed
		}

		mergeErr := Merge(payload)

		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			if len(expected) > 0 {
				t.Fatalf("%s is gone, but %d entries were written to it", path, len(expected))
			}
			if mergeErr == nil {
				t.Fatalf("Merge(%q) succeeded without writing %s", payload, path)
			}
			return
		}
		if err != nil {
			t.Fatalf("unable to read %s: %v", path, err)
		}

		current, err := parse(b)
		if err != nil {
			t.Fatalf("%s does not parse after Merge(%q): %v\n%s", path, payload, err, b)
		}

		for registry, want := range expected {
			got, ok := current.Auths[registry]
			if !ok {
				t.Fatalf("the entry for %q is gone after Merge(%q)", registry, payload)
			}
			if _, named := incoming.Auths[registry]; named {
				continue
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("the entry for %q changed after Merge(%q):\n got  = %s\n want = %s",
					registry, payload, got, want)
			}
		}

		if mergeErr == nil {
			for registry := range incoming.Auths {
				if _, ok := current.Auths[registry]; !ok {
					t.Fatalf("Merge(%q) succeeded but wrote no entry for %q", payload, registry)
				}
			}
		}

		if len(current.Auths) > maxEntries {
			if err := os.Remove(path); err != nil {
				t.Fatalf("unable to empty %s: %v", path, err)
			}
			expected = map[string]json.RawMessage{}
			return
		}
		expected = current.Auths
	})
}
