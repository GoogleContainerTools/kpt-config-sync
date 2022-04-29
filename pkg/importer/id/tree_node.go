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

package id

import (
	"fmt"
)

// TreeNode represents a named node in the config hierarchy.
type TreeNode interface {
	// Path is the embedded interface providing path information to this node.
	Path
	// Name returns the name of this node.
	Name() string
}

// PrintTreeNode returns a convenient representation of a TreeNode for error messages.
func PrintTreeNode(n TreeNode) string {
	return fmt.Sprintf("path: %[1]s\n"+
		"name: %[2]s",
		n.SlashPath(), n.Name())
}
