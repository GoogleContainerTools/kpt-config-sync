// Copyright 2024 Google LLC
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

package restconfig

import (
	"fmt"
	"os/user"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/rest"
)

func Test_NewRestConfig_HomeDirWithoutKubeConfig(t *testing.T) {
	t.Cleanup(func() {
		userCurrentTestHook = defaultGetCurrentUser
	})

	userCurrentTestHook = func() (*user.User, error) {
		return &user.User{
			HomeDir: "/some/path/that/hopefully/does/not/exist",
		}, nil
	}
	calledNewFromConfigFile := false
	calledNewFromInClusterConfig := false
	fakeBuilder := restConfigBuilder{
		newFromConfigFileFn: func(_ string) (*rest.Config, error) {
			calledNewFromConfigFile = true
			return nil, fmt.Errorf("unexpected call to local config")
		},
		newFromInClusterConfigFn: func() (*rest.Config, error) {
			calledNewFromInClusterConfig = true
			return &rest.Config{}, nil
		},
	}

	_, err := fakeBuilder.newRestConfig(time.Minute)
	assert.NoError(t, err)
	assert.False(t, calledNewFromConfigFile, "unexpected call to NewFromConfigFile")
	assert.True(t, calledNewFromInClusterConfig, "expected call to NewFromInClusterConfig")
}
