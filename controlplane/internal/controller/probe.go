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
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/converge"
)

// probeTimeout is how long one reachability check waits.
//
// Short on purpose. This is a "is it up yet" question asked once per reconcile, not a connection
// attempt worth persisting with -- the answer "not yet" costs a 15-second requeue and nothing
// else.
const probeTimeout = 3 * time.Second

// Prober reports whether a host is accepting SSH.
type Prober interface {
	// Reachable returns true when the host answers.
	//
	// It returns (false, nil) for a host that is simply not up yet, and (false, err) only when
	// the question could not be asked. The distinction matters because the first is the ordinary
	// state of a machine mid-boot and must not be logged as an error on every pass.
	Reachable(ctx context.Context, host converge.Host) (bool, error)
}

// DialProbe answers by opening a TCP connection to the SSH port.
//
// It deliberately does not complete an SSH handshake or authenticate. What the reconcile needs to
// know is whether starting a converge is worth it, and a listening sshd is a good enough answer:
// a key that is wrong or a user that does not exist is a configuration error that should surface
// as a failed converge with cargoship's own message, not as a probe that quietly returns false
// forever.
//
// The reason the probe exists at all is that cargoship's connect phase retries for fifteen minutes
// by default. Against a machine that is still booting that would hold the runner's single slot for
// the whole window and report nothing but "converging" to the operator.
type DialProbe struct{}

// Reachable opens and closes a TCP connection to the host's SSH port.
func (p *DialProbe) Reachable(ctx context.Context, host converge.Host) (bool, error) {
	cfg := host.Host.ConnectionConfig.SSH
	if cfg == nil {
		return false, fmt.Errorf("host %s has no ssh configuration", host.Address)
	}

	port := cfg.Port
	if port == 0 {
		port = 22
	}

	dialCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(dialCtx, "tcp", net.JoinHostPort(cfg.Address, strconv.Itoa(port)))
	if err != nil {
		// Refused, unreachable or timed out: all of them mean "not yet", which is not an error.
		return false, nil
	}
	// The probe's whole answer is "something is listening". A failure to close a socket we
	// opened only to drop does not change that, and there is no caller to tell.
	_ = conn.Close() //nolint:errcheck // nothing acts on a failed close of a discarded probe socket
	return true, nil
}

// firstController returns the first controller host in an inventory.
//
// BuildInventory guarantees there is one, so this does not return an error.
func firstController(inv *converge.Inventory) converge.Host {
	for _, h := range inv.Hosts {
		if h.Role == converge.RoleController {
			return h
		}
	}
	return inv.Hosts[0]
}
