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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1 "github.com/colonel-byte/cluster-api-provider-cargoship/bootstrap/api/v1alpha1"
)

// ownerMachine returns the Machine that owns a config, or nil if one is not set yet.
//
// A missing owner is not an error: the MachineSet or control-plane controller creates the config
// and sets itself as owner in a separate write, so there is a window in which the config exists
// with no owner at all.
func ownerMachine(ctx context.Context, c client.Client, config *bootstrapv1.CargoshipConfig) (*clusterv1.Machine, error) {
	for _, ref := range config.OwnerReferences {
		if ref.Kind != "Machine" {
			continue
		}
		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil || gv.Group != clusterv1.GroupVersion.Group {
			continue
		}

		machine := &clusterv1.Machine{}
		key := types.NamespacedName{
			Namespace: config.Namespace,
			Name:      ref.Name,
		}
		if err := c.Get(ctx, key, machine); err != nil {
			return nil, fmt.Errorf("unable to read the owning Machine %s: %w", key, err)
		}
		return machine, nil
	}
	return nil, nil
}

// controlPlaneRefFor finds the CargoshipControlPlane governing a machine's cluster.
//
// It goes through the Cluster rather than through the machine's own labels, because a worker
// machine has no reference to the control plane at all -- only the Cluster does. That is also why
// a worker's bootstrap config is rendered with the same authorized key as a controller's: the key
// belongs to the cluster, not to the machine.
func controlPlaneRefFor(ctx context.Context, c client.Client, machine *clusterv1.Machine) (types.NamespacedName, error) {
	cluster := &clusterv1.Cluster{}
	key := types.NamespacedName{
		Namespace: machine.Namespace,
		Name:      machine.Spec.ClusterName,
	}
	if err := c.Get(ctx, key, cluster); err != nil {
		return types.NamespacedName{}, fmt.Errorf("unable to read the Cluster %s: %w", key, err)
	}

	ref := cluster.Spec.ControlPlaneRef
	if ref.Kind != "CargoshipControlPlane" {
		return types.NamespacedName{}, fmt.Errorf(
			"cluster %s has control plane %s/%s, not a CargoshipControlPlane: a CargoshipConfig only makes sense alongside one",
			key, ref.APIGroup, ref.Kind)
	}

	return types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      ref.Name,
	}, nil
}

// setCondition records one condition on a config.
func setCondition(config *bootstrapv1.CargoshipConfig, conditionType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: config.Generation,
	})
}
