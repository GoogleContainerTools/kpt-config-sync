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

package bugreport

import (
	"context"
	"io"
	"path"

	v1 "k8s.io/api/core/v1"
)

type logSource struct {
	ns   v1.Namespace
	pod  v1.Pod
	cont v1.Container
}

func (l *logSource) pathName() string {
	return path.Join(Namespace, l.ns.Name, l.pod.Name, l.cont.Name)
}

func (l *logSource) fetchRcForLogSource(ctx context.Context, cs coreClient) (io.ReadCloser, error) {
	options := v1.PodLogOptions{Timestamps: true, Container: l.cont.Name}
	return cs.CoreV1().Pods(l.ns.Name).GetLogs(l.pod.Name, &options).Stream(ctx)
}
