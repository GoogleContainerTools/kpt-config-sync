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

package node

const (
	// Namespace represents a leaf node in the hierarchy which is materialized as a kubernetes Namespace.
	Namespace = Type("Namespace")
	// AbstractNamespace represents a non-leaf node in the hierarchy.
	AbstractNamespace = Type("Abstract Namespace")
)

// Type represents the type of the node.
type Type string
