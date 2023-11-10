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

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestStorageResourceGroup(t *testing.T) {
	key := types.NamespacedName{
		Name:      "foo",
		Namespace: "default",
	}
	created := &ResourceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: ResourceGroupSpec{},
	}

	// Test Create
	fetched := &ResourceGroup{}
	err := c.Create(context.TODO(), created)
	assert.NoError(t, err)

	err = c.Get(context.TODO(), key, fetched)
	assert.NoError(t, err)
	assert.Equal(t, created, fetched)

	// Test Updating the Labels
	updated := fetched.DeepCopy()
	updated.Labels = map[string]string{"hello": "world"}
	err = c.Update(context.TODO(), updated)
	assert.NoError(t, err)

	err = c.Get(context.TODO(), key, fetched)
	assert.NoError(t, err)
	assert.Equal(t, updated, fetched)

	// Test Delete
	err = c.Delete(context.TODO(), fetched)
	assert.NoError(t, err)
	err = c.Get(context.TODO(), key, fetched)
	assert.Error(t, err)
}
