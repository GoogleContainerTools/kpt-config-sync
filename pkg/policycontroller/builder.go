// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package policycontroller

import (
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/policycontroller/constraint"
	"kpt.dev/configsync/pkg/policycontroller/constrainttemplate"
	"kpt.dev/configsync/pkg/util/watch"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type builder struct{}

var _ watch.ControllerBuilder = &builder{}

// StartControllers starts a new constraint controller for each of the specified
// constraint GVKs. It also starts the controller for ConstraintTemplates if
// their CRD is present.
func (b *builder) StartControllers(mgr manager.Manager, gvks map[schema.GroupVersionKind]bool, _ metav1.Time) error {
	ctGVK := constrainttemplate.GVK.String()
	for gvk := range gvks {
		if gvk.String() == ctGVK {
			if err := constrainttemplate.AddController(mgr); err != nil {
				return errors.Wrap(err, "controller for ConstraintTemplate")
			}
		} else if err := constraint.AddController(mgr, gvk.Kind); err != nil {
			return errors.Wrapf(err, "controller for %s", gvk.String())
		}
	}
	return nil
}
