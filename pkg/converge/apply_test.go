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
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/config"
)

// TestWriteSignatureKey covers the two things the apply depends on: the key cargoship reads back
// is the key it was handed, and the cleanup takes the file away. Not parallel, because it sets a
// cargoship global.
func TestWriteSignatureKey(t *testing.T) {
	dir := t.TempDir()
	restore := config.CommonOptions.TempDirectory
	config.CommonOptions.TempDirectory = dir
	t.Cleanup(func() { config.CommonOptions.TempDirectory = restore })

	want := []byte("-----BEGIN PUBLIC KEY-----\naaaa\n-----END PUBLIC KEY-----\n")
	path, remove, err := writeSignatureKey(want)
	if err != nil {
		t.Fatalf("writeSignatureKey: %v", err)
	}

	// On the mounted volume rather than wherever os.TempDir happens to point, which is the whole
	// reason the directory is named rather than left to the default.
	if got := filepath.Dir(path); got != dir {
		t.Errorf("writeSignatureKey() wrote to %s, want a file in %s", got, dir)
	}

	got, err := os.ReadFile(path) //nolint:gosec // the path is the one just written above
	if err != nil {
		t.Fatalf("reading back the key: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("writeSignatureKey() wrote %q, want %q", got, want)
	}

	remove()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the key file is still there after the cleanup: %v", err)
	}
}
