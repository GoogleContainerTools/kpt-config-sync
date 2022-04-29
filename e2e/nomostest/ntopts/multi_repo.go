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

package ntopts

import "k8s.io/apimachinery/pkg/types"

// RepoOpts defines options for a Repo
type RepoOpts struct {
	// UpstreamURL provides the upstream repo to initialize the repo with
	UpstreamURL string
}

// MultiRepo configures the NT for use with multi-repo tests.
// If NonRootRepos is non-empty, the test is assumed to be running in
// multi-repo mode.
type MultiRepo struct {
	// NamespaceRepos is a set representing the Namespace repos to create.
	//
	// We don't support referencing the Root repository in this map; while we do
	// support this use case, it isn't special behavior that tests any unique code
	// paths.
	NamespaceRepos map[types.NamespacedName]RepoOpts

	// RootRepos is a set representing the Root repos to create.
	RootRepos map[string]RepoOpts

	// Control indicates options for configuring Namespace Repos.
	Control repoControl

	// SkipMultiRepo will skip the test if run in multi repo mode.  This stutters because we decided to embed
	// this struct inside of the "New" struct rather than have it as a member.
	SkipMultiRepo bool

	// MultiRepoIncompatible will disable the test for multi repo.  Setting --skip-mode will not affect whether it gets run.
	// This should be used for disabling tests
	MultiRepoIncompatible bool

	// SkipMonoRepo will skip the test if run in mono repo mode.
	SkipMonoRepo bool

	// ResourceGroup indicates that NT should also install the resource-group controller
	ResourceGroup bool
}

// NamespaceRepo tells the test case that a Namespace Repo should be configured
// that points at the provided Repository.
func NamespaceRepo(ns, name string) func(opt *New) {
	return func(opt *New) {
		nn := types.NamespacedName{
			Namespace: ns,
			Name:      name,
		}
		opt.NamespaceRepos[nn] = RepoOpts{UpstreamURL: ""}
	}
}

// RootRepo tells the test case that a Root Repo should be configured
// that points at the provided Repository.
func RootRepo(name string) func(opt *New) {
	return func(opt *New) {
		opt.RootRepos[name] = RepoOpts{UpstreamURL: ""}
	}
}

// NamespaceRepoWithUpstream tells the test case that a Namespace Repo should be configured
// that points at the provided Repository.
func NamespaceRepoWithUpstream(ns, name string, upstreamURL string) func(opt *New) {
	return func(opt *New) {
		nn := types.NamespacedName{
			Namespace: ns,
			Name:      name,
		}
		opt.NamespaceRepos[nn] = RepoOpts{UpstreamURL: upstreamURL}
	}
}

// UpstreamRepo tells the test case that an Upstream Repo should be used to seed the test repo
func UpstreamRepo(upstreamURL string) func(opt *New) {
	return func(opt *New) {
		opt.UpstreamURL = upstreamURL
	}
}

// SkipMultiRepo will skip the test in multi repo mode.
func SkipMultiRepo(opt *New) {
	opt.SkipMultiRepo = true
}

// MultiRepoIncompatible will always skip the test in multi repo mode.
func MultiRepoIncompatible(opt *New) {
	opt.MultiRepoIncompatible = true
}

// SkipMonoRepo will skip the test in mono repo mode.
func SkipMonoRepo(opt *New) {
	opt.SkipMonoRepo = true
}

// InstallResourceGroupController installs the resource-group controller.
func InstallResourceGroupController(opts *New) {
	opts.ResourceGroup = true
}

// WithDelegatedControl will specify the Delegated Control Pattern.
func WithDelegatedControl(opt *New) {
	opt.Control = DelegatedControl
}

// WithCentralizedControl will specify the Central Control Pattern.
func WithCentralizedControl(opt *New) {
	opt.Control = CentralControl
}

// repoControl indicates the type of control for Namespace repos.
type repoControl string

const (
	// DelegatedControl indicates the central admin only declares the Namespace
	// in the Root Repo and delegates declaration of RepoSync to the app operator.
	DelegatedControl = "Delegated"
	// CentralControl indicates the central admin only declares the Namespace
	// in the Root Repo and delegates declaration of RepoSync to the app operator.
	CentralControl = "Central"
)
