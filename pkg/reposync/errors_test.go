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

package reposync

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func withSyncStatus(status v1beta1.Status) core.MetaMutator {
	return func(o client.Object) {
		rs := o.(*v1beta1.RepoSync)
		rs.Status.Status = status
	}
}

func fakeSyncStatus() v1beta1.Status {
	return v1beta1.Status{
		Rendering: v1beta1.RenderingStatus{
			Errors: []v1beta1.ConfigSyncError{
				{Code: "1061", ErrorMessage: "rendering-error-message"},
			},
		},
		Source: v1beta1.SourceStatus{
			Errors: []v1beta1.ConfigSyncError{
				{Code: "1021", ErrorMessage: "1021-error-message"},
				{Code: "1022", ErrorMessage: "1022-error-message"},
			},
		},
		Sync: v1beta1.SyncStatus{
			Errors: []v1beta1.ConfigSyncError{
				{Code: "2009", ErrorMessage: "apiserver error"},
				{Code: "2009", ErrorMessage: "webhook error"},
			},
		},
	}
}

func TestErrors(t *testing.T) {
	testCases := []struct {
		name         string
		rs           *v1beta1.RepoSync
		errorSources []v1beta1.ErrorSource
		want         []v1beta1.ConfigSyncError
	}{
		{
			name:         "errorSources is nil, rs is nil",
			rs:           nil,
			errorSources: nil,
			want:         nil,
		},
		{
			name:         "errorSources is nil, rs is not nil",
			rs:           fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName),
			errorSources: nil,
			want:         nil,
		},
		{
			name:         "errorSources is not nil, rs is nil",
			rs:           nil,
			errorSources: []v1beta1.ErrorSource{v1beta1.RenderingError},
			want:         nil,
		},
		{
			name:         "errorSources = {}",
			rs:           fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withSyncStatus(fakeSyncStatus())),
			errorSources: []v1beta1.ErrorSource{},
			want:         nil,
		},
		{
			name:         "errorSources = {RenderingError}",
			rs:           fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withSyncStatus(fakeSyncStatus())),
			errorSources: []v1beta1.ErrorSource{v1beta1.RenderingError},
			want: []v1beta1.ConfigSyncError{
				{Code: "1061", ErrorMessage: "rendering-error-message"},
			},
		},
		{
			name:         "errorSources = {SourceError}",
			rs:           fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withSyncStatus(fakeSyncStatus())),
			errorSources: []v1beta1.ErrorSource{v1beta1.SourceError},
			want: []v1beta1.ConfigSyncError{
				{Code: "1021", ErrorMessage: "1021-error-message"},
				{Code: "1022", ErrorMessage: "1022-error-message"},
			},
		},
		{
			name:         "errorSources = {SyncError}",
			rs:           fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withSyncStatus(fakeSyncStatus())),
			errorSources: []v1beta1.ErrorSource{v1beta1.SyncError},
			want: []v1beta1.ConfigSyncError{
				{Code: "2009", ErrorMessage: "apiserver error"},
				{Code: "2009", ErrorMessage: "webhook error"},
			},
		},
		{
			name:         "errorSources = {RenderingError, SourceError}",
			rs:           fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withSyncStatus(fakeSyncStatus())),
			errorSources: []v1beta1.ErrorSource{v1beta1.RenderingError, v1beta1.SourceError},
			want: []v1beta1.ConfigSyncError{
				{Code: "1061", ErrorMessage: "rendering-error-message"},
				{Code: "1021", ErrorMessage: "1021-error-message"},
				{Code: "1022", ErrorMessage: "1022-error-message"},
			},
		},
		{
			name:         "errorSources = {RenderingError, SyncError}",
			rs:           fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withSyncStatus(fakeSyncStatus())),
			errorSources: []v1beta1.ErrorSource{v1beta1.RenderingError, v1beta1.SyncError},
			want: []v1beta1.ConfigSyncError{
				{Code: "1061", ErrorMessage: "rendering-error-message"},
				{Code: "2009", ErrorMessage: "apiserver error"},
				{Code: "2009", ErrorMessage: "webhook error"},
			},
		},
		{
			name:         "errorSources = {SourceError, SyncError}",
			rs:           fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withSyncStatus(fakeSyncStatus())),
			errorSources: []v1beta1.ErrorSource{v1beta1.SourceError, v1beta1.SyncError},
			want: []v1beta1.ConfigSyncError{
				{Code: "1021", ErrorMessage: "1021-error-message"},
				{Code: "1022", ErrorMessage: "1022-error-message"},
				{Code: "2009", ErrorMessage: "apiserver error"},
				{Code: "2009", ErrorMessage: "webhook error"},
			},
		},
		{
			name:         "errorSources = {RenderingError, SourceError, SyncError}",
			rs:           fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withSyncStatus(fakeSyncStatus())),
			errorSources: []v1beta1.ErrorSource{v1beta1.RenderingError, v1beta1.SourceError, v1beta1.SyncError},
			want: []v1beta1.ConfigSyncError{
				{Code: "1061", ErrorMessage: "rendering-error-message"},
				{Code: "1021", ErrorMessage: "1021-error-message"},
				{Code: "1022", ErrorMessage: "1022-error-message"},
				{Code: "2009", ErrorMessage: "apiserver error"},
				{Code: "2009", ErrorMessage: "webhook error"},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Errors(tc.rs, tc.errorSources)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Errors() got %v, want %v", got, tc.want)
			}
		})
	}
}
