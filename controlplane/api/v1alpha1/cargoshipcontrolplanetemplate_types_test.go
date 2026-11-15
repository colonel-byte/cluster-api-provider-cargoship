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

package v1alpha1

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// topologyOwned are the fields of CargoshipControlPlaneSpec that Cluster API fills in when it
// stamps a CargoshipControlPlane out of a template, and which therefore must not be in the
// template at all. Each is required or defaulted on the live type, so carrying it in the template
// would mean an operator writing a value the topology controller immediately overwrites.
var topologyOwned = map[string]string{
	"version":  "Cluster.spec.topology.version",
	"replicas": "Cluster.spec.topology.controlPlane.replicas",
}

// topologyOwnedMachineTemplate is the same for the machine template within it.
var topologyOwnedMachineTemplate = map[string]string{
	"infrastructureRef": "ClusterClass.spec.controlPlane.machineInfrastructure.templateRef",
}

// TestTemplateSpecCoversTheLiveSpec is what keeps a class able to carry everything a
// CargoshipControlPlane has.
//
// Nothing in this repository converts between the two specs: the topology controller copies
// spec.template.spec onto the new object as it is, matching by field name. So a field added to
// the live spec and not to the template is a field a class cannot set and nobody finds out about
// -- the resource validates, the cluster stamps, and the setting is simply absent.
func TestTemplateSpecCoversTheLiveSpec(t *testing.T) {
	live := jsonFields(reflect.TypeFor[CargoshipControlPlaneSpec]())
	template := jsonFields(reflect.TypeFor[CargoshipControlPlaneTemplateResourceSpec]())

	for name := range live {
		if owner, ok := topologyOwned[name]; ok {
			if _, present := template[name]; present {
				t.Errorf("the template carries %q, which %s sets: an operator would be writing a value that gets overwritten", name, owner)
			}
			continue
		}
		if _, ok := template[name]; !ok {
			t.Errorf("CargoshipControlPlaneSpec has %q and the template does not, so a ClusterClass cannot set it; "+
				"add it to CargoshipControlPlaneTemplateResourceSpec, or to topologyOwned with what sets it", name)
		}
	}

	for name := range template {
		if _, ok := live[name]; !ok {
			t.Errorf("the template carries %q, which CargoshipControlPlaneSpec does not: it would be stamped onto an "+
				"object with no such field and dropped", name)
		}
	}
}

// The machine template is the one place the two specs deliberately differ in shape, so it is
// compared on its own rather than by type.
func TestTemplateMachineTemplateCoversTheLiveOne(t *testing.T) {
	live := jsonFields(reflect.TypeFor[CargoshipControlPlaneMachineTemplateSpec]())
	template := jsonFields(reflect.TypeFor[CargoshipControlPlaneTemplateMachineTemplateSpec]())

	for name := range live {
		if owner, ok := topologyOwnedMachineTemplate[name]; ok {
			if _, present := template[name]; present {
				t.Errorf("the template's machineTemplate carries %q, which %s supplies", name, owner)
			}
			continue
		}
		if _, ok := template[name]; !ok {
			t.Errorf("machineTemplate.spec has %q and the template's does not, so a ClusterClass cannot set it", name)
		}
	}

	for name := range template {
		if _, ok := live[name]; !ok {
			t.Errorf("the template's machineTemplate carries %q, which the live one does not", name)
		}
	}

	// Both halves of the metadata the class propagates have to exist, since that is what a
	// ClusterClass and a topology merge their own labels into.
	if _, ok := jsonFields(reflect.TypeFor[CargoshipControlPlaneTemplateMachineTemplate]())["metadata"]; !ok {
		t.Error("the template's machineTemplate has no metadata, so a class could not label control-plane machines")
	}
}

// jsonFields maps a struct's JSON field names to their types, ignoring the "-" and inline cases
// neither spec uses.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	for i := range t.NumField() {
		field := t.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		fields[name] = field.Type
	}
	return fields
}

// TestTemplateIsRegistered covers the one line that is easy to leave out: a kind missing from
// objectTypes builds, generates a CRD, installs, and then fails at runtime with "no kind is
// registered" the first time anything reads one.
func TestTemplateIsRegistered(t *testing.T) {
	var kinds []string
	for _, obj := range objectTypes {
		kinds = append(kinds, reflect.TypeOf(obj).Elem().Name())
	}
	sort.Strings(kinds)

	for _, want := range []string{"CargoshipControlPlaneTemplate", "CargoshipControlPlaneTemplateList"} {
		found := false
		for _, kind := range kinds {
			if kind == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s is not in objectTypes; the scheme holds %v", want, kinds)
		}
	}
}
