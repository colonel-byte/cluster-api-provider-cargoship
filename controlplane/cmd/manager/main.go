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

// Command manager runs the cargoship control-plane provider.
//
// It converges a fleet to a cargoship package over SSH, in place of kubeadm. The reconcile holds
// no deadlines: every wait is a requeue, so machines and API servers coming up on whatever
// schedule their infrastructure provider gives them is the normal case rather than a timeout to
// tune.
package main

import (
	"flag"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/internal/controller"
	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/converge"
	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/manager"
)

func main() {
	var opts manager.Options
	opts.BindFlags(flag.CommandLine)
	opts.BindConvergeFlags(flag.CommandLine)
	applyLogging := manager.SetupLogging(flag.CommandLine)
	flag.Parse()
	applyLogging()

	opts.LeaderElectionID = "cargoship-controlplane.cluster.x-k8s.io"

	// Done before the manager starts, and fatal if it fails. Cargoship reads both of these from
	// process-wide globals, and the default cache path is "~/.cargoship-cache" -- which in a
	// container with no HOME resolves to a literal "~" directory under the working directory, on
	// the pod's writable layer. A converge would then appear to work and lose its blob cache on
	// every restart.
	if err := converge.SetPaths(converge.Paths{Cache: opts.CachePath, Temp: opts.TempPath}); err != nil {
		ctrl.Log.Error(err, "unable to prepare the cargoship cache and temp directories",
			"cache", opts.CachePath, "temp", opts.TempPath)
		os.Exit(1)
	}

	mgr, err := manager.New(opts)
	if err != nil {
		ctrl.Log.Error(err, "unable to build the manager")
		os.Exit(1)
	}

	reconciler := &controller.CargoshipControlPlaneReconciler{
		Client: mgr.GetClient(),
		// One runner for the process, so "at most one converge per cluster" holds across
		// reconciles rather than within one.
		Runner: converge.NewRunner(),
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "unable to set up the CargoshipControlPlane controller")
		os.Exit(1)
	}

	ctrl.Log.Info("starting the cargoship control-plane provider",
		"cache", opts.CachePath, "temp", opts.TempPath)
	manager.Run(mgr)
}
