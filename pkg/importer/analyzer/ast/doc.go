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

/*
Package ast declares the types used for loading Kubernetes resources from the filesystem into something
like an Abstract Syntax Tree (AST) that allows for writing reusable visitors.  The visitor package
defines some base visitors to use for iterating over the tree and performing transforms.

Each node in the AST implements the "Node" interface which has only one method, "Accept".  For a
visitor to visit a node, it should pass itself to the node's Accept method, and then the node will
call the appropriate "Visit[Type]" method on the visitor.  Iteration is started by having the root
of the tree Accept() the visitor.

Note that this isn't quite exactly a "true" AST as the subtypes are all fully typed, and it is
feasible to iterate over the entire contents in a fully typed manner.  The visitor itself is here
for convenience to make processing and transforming the tree relatively concise.
*/
package ast
