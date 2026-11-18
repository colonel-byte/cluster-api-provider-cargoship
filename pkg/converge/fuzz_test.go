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
	"bytes"
	"encoding/json"
	"testing"
)

// FuzzDecodeClusterConfig drives the one place arbitrary bytes from a custom resource reach a
// typed cargoship struct. spec.clusterConfig is an opaque object in the schema, so the API server
// validates nothing about it beyond it being JSON.
//
// The invariant is that a config the decoder accepts survives a round trip: re-encoding it and
// decoding that again has to succeed and produce the same bytes. Decoding is strict about unknown
// fields, so this fails if cargoship ever gains a field whose marshalled name the decoder will not
// accept -- a config that decodes once and not twice would mean an accepted config the provider
// cannot write back out.
func FuzzDecodeClusterConfig(f *testing.F) {
	f.Add("")
	f.Add("null")
	f.Add(`{}`)
	f.Add(`{"registries":[{"name":"registry.example"}],"values":{"cni":"cilium"}}`)
	f.Add(`{"registires":[{"name":"registry.example"}]}`)
	f.Add(`{"values":`)

	f.Fuzz(func(t *testing.T, raw string) {
		cfg, err := DecodeClusterConfig([]byte(raw))
		if err != nil {
			return
		}

		first, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("a decoded config did not re-encode: %v", err)
		}

		again, err := DecodeClusterConfig(first)
		if err != nil {
			t.Fatalf("re-encoded config %s was refused on the way back in: %v", first, err)
		}

		second, err := json.Marshal(again)
		if err != nil {
			t.Fatalf("a round-tripped config did not re-encode: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("the round trip changed the config:\n first  = %s\n second = %s", first, second)
		}
	})
}
