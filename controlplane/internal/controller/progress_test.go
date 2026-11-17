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

package controller

import (
	"testing"

	"github.com/colonel-byte/cargoship/pkg/phase"

	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/converge"
)

func TestConvergingMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		hosts int
		run   *converge.Run
		want  string
	}{
		{
			name:  "before the first phase the sink is empty",
			hosts: 3,
			run:   &converge.Run{},
			want:  "converging 3 hosts",
		},
		{
			name:  "a phase in flight is named with its position",
			hosts: 3,
			run: &converge.Run{
				Progress: phase.Progress{
					Phase: "Upload files to hosts",
					Index: 4,
					Total: 11,
					State: phase.StateRunning,
				},
			},
			want: "converging 3 hosts: phase 4 of 11, Upload files to hosts",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := convergingMessage(tc.hosts, tc.run); got != tc.want {
				t.Errorf("convergingMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}
