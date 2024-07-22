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

package k8sobjects

import (
	v1 "k8s.io/api/admissionregistration/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/kinds"
)

// AdmissionWebhookObject initializes a AdmissionWebhook object.
func AdmissionWebhookObject(name string, opts ...core.MetaMutator) *v1.ValidatingWebhookConfiguration {
	result := &v1.ValidatingWebhookConfiguration{TypeMeta: ToTypeMeta(kinds.ValidatingWebhookConfiguration())}
	defaultMutate(result)
	mutate(result, core.Name(name))
	mutate(result, opts...)

	return result
}

// AdmissionWebhook returns a AdmissionWebhook in a FileObject.
func AdmissionWebhook(name, dir string, opts ...core.MetaMutator) ast.FileObject {
	relative := cmpath.RelativeSlash(dir).Join(cmpath.RelativeSlash("admission_webhook.yaml"))
	return FileObject(AdmissionWebhookObject(name, opts...), relative.SlashPath())
}
