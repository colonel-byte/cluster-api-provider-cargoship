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

// Command manager runs the cargoship bootstrap provider.
//
// It renders the bootstrap data a machine needs to be reachable over SSH, and nothing else -- see
// bootstrap/internal/cloudinit for why that is the whole job.
package main

import (
	"flag"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/colonel-byte/cluster-api-provider-cargoship/bootstrap/internal/controller"
	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/manager"
)

func main() {
	var opts manager.Options
	opts.BindFlags(flag.CommandLine)
	applyLogging := manager.SetupLogging(flag.CommandLine)
	flag.Parse()
	applyLogging()

	opts.LeaderElectionID = "cargoship-bootstrap.cluster.x-k8s.io"

	mgr, err := manager.New(opts)
	if err != nil {
		ctrl.Log.Error(err, "unable to build the manager")
		os.Exit(1)
	}

	if err := (&controller.CargoshipConfigReconciler{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "unable to set up the CargoshipConfig controller")
		os.Exit(1)
	}

	ctrl.Log.Info("starting the cargoship bootstrap provider")
	manager.Run(mgr)
}
