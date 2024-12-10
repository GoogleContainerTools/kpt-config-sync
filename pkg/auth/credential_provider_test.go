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

package auth

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"cloud.google.com/go/auth/credentials"
	"cloud.google.com/go/compute/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"kpt.dev/configsync/pkg/testing/testerrors"
)

const (
	googleAppCredsEnvVar     = "GOOGLE_APPLICATION_CREDENTIALS"
	googleMetadataHostEnvVar = "GCE_METADATA_HOST"
	homeEnvVar               = "HOME"
)

// TestIsCredentialsNotFoundError validates that IsCredentialsNotFoundError can
// detect the NotFound error from DetectDefault.
//
// This is NOT an exhaustive test of DetectDefault, which supports many
// different kinds of configuration inputs, including several JSON schemas.
// Instead, this is just intended to catch upstream changes to DetectDefault
// that might break IsCredentialsNotFoundError.
//
// Feel free to update the expected error string values if they change upstream.
func TestIsCredentialsNotFoundError(t *testing.T) {
	// Make temp dir for HOME
	tmpDirPath, err := os.MkdirTemp("", "kpt-config-sync-TestIsCredentialsNotFoundError-XXXXXX")
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, os.RemoveAll(tmpDirPath))
	})
	// Restore parent env after test
	origAppCredsPath := os.Getenv(googleAppCredsEnvVar)
	origMetadataHost := os.Getenv(googleMetadataHostEnvVar)
	origHomePath := os.Getenv(homeEnvVar)
	t.Cleanup(func() {
		assert.NoError(t, os.Setenv(googleAppCredsEnvVar, origAppCredsPath))
		assert.NoError(t, os.Setenv(googleMetadataHostEnvVar, origMetadataHost))
		assert.NoError(t, os.Setenv(homeEnvVar, origHomePath))
	})
	// Configure test env to hide host config, to force DetectDefault to fail.
	// Default location is $HOME/.config/gcloud/application_default_credentials.json
	require.NoError(t, os.Unsetenv(googleAppCredsEnvVar))
	require.NoError(t, os.Unsetenv(googleMetadataHostEnvVar))
	require.NoError(t, os.Setenv(homeEnvVar, tmpDirPath))

	tests := []struct {
		name             string
		opts             *credentials.DetectOptions
		expectedError    error
		expectedNotFound bool
	}{
		{
			name: "not found",
			opts: &credentials.DetectOptions{},
			expectedError: func() error {
				// On GCE, DetectDefault never errors.
				if metadata.OnGCE() {
					return nil
				}
				// Elsewhere we expect DetectDefault to error when the config is empty.
				// https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.1/auth/credentials/detect.go#L108
				return errors.New("credentials: could not find default credentials. " +
					"See https://cloud.google.com/docs/authentication/external/set-up-adc for more information")
			}(),
			expectedNotFound: func() bool {
				// On GCE, DetectDefault never errors.
				return !metadata.OnGCE()
			}(),
		},
		{
			name: "missing file",
			// Parser: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/credentials/detect.go#L223
			// Schema: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/internal/credsfile/filetype.go#L31
			opts: &credentials.DetectOptions{
				CredentialsFile: "/path/to/nothing",
			},
			expectedError: &fs.PathError{
				Op:   "open",
				Path: "/path/to/nothing",
				Err:  errors.New("no such file or directory"),
			},
			expectedNotFound: false,
		},
		{
			name: "json - empty",
			// Parser: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/credentials/detect.go#L223
			// Schema: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/internal/credsfile/filetype.go#L31
			opts: &credentials.DetectOptions{
				CredentialsJSON: []byte(`{}`),
			},
			expectedError:    errors.New("credentials: unsupported filetype '\\x00'"),
			expectedNotFound: false,
		},
		{
			name: "json - unsupported filetype",
			// Parser: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/credentials/detect.go#L223
			// Schema: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/internal/credsfile/filetype.go#L31
			opts: &credentials.DetectOptions{
				CredentialsJSON: []byte(`{"invalid-type":{}}`),
			},
			expectedError:    errors.New("credentials: unsupported filetype '\\x00'"),
			expectedNotFound: false,
		},
		{
			name: "json - valid service_account",
			// Parser: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/credentials/detect.go#L196
			// Type Detection: https://github.com/googleapis/google-cloud-go/blob/main/auth/credentials/filetypes.go#L30
			// Types: https://github.com/googleapis/google-cloud-go/blob/main/auth/internal/credsfile/credsfile.go#L60
			// Schema: https://github.com/googleapis/google-cloud-go/blob/main/auth/internal/credsfile/filetype.go#L38
			opts: &credentials.DetectOptions{
				CredentialsJSON: []byte(`{
	"type": "service_account",
	"client_email": "fake-email@example.com",
	"private_key": "fake-key"
}`),
			},
			expectedNotFound: false,
		},
		{
			name: "json - invalid service_account",
			// Parser: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/credentials/detect.go#L196
			// Type Detection: https://github.com/googleapis/google-cloud-go/blob/main/auth/credentials/filetypes.go#L30
			// Types: https://github.com/googleapis/google-cloud-go/blob/main/auth/internal/credsfile/credsfile.go#L60
			// Schema: https://github.com/googleapis/google-cloud-go/blob/main/auth/internal/credsfile/filetype.go#L38
			opts: &credentials.DetectOptions{
				CredentialsJSON: []byte(`{
	"type": "service_account"
}`),
			},
			// https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/auth.go#L507
			expectedError:    errors.New("auth: email must be provided"),
			expectedNotFound: false,
		},
		{
			name: "json - valid external_account",
			// Parser: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/credentials/detect.go#L196
			// Type Detection: https://github.com/googleapis/google-cloud-go/blob/main/auth/credentials/filetypes.go#L30
			// Types: https://github.com/googleapis/google-cloud-go/blob/main/auth/internal/credsfile/credsfile.go#L60
			// Schema: https://github.com/googleapis/google-cloud-go/blob/main/auth/internal/credsfile/filetype.go#L61
			opts: &credentials.DetectOptions{
				CredentialsJSON: []byte(`{
	"type": "external_account",
	"audience": "example",
	"subject_token_type": "subject",
	"credential_source": {
		"file": "/path/to/nothing"
	}
}`),
			},
			expectedNotFound: false,
		},
		{
			name: "json - invalid external_account",
			// Parser: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/credentials/detect.go#L196
			// Type Detection: https://github.com/googleapis/google-cloud-go/blob/main/auth/credentials/filetypes.go#L30
			// Types: https://github.com/googleapis/google-cloud-go/blob/main/auth/internal/credsfile/credsfile.go#L60
			// Schema: https://github.com/googleapis/google-cloud-go/blob/main/auth/internal/credsfile/filetype.go#L61
			opts: &credentials.DetectOptions{
				CredentialsJSON: []byte(`{
	"type": "external_account"
}`),
			},
			// https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/credentials/internal/externalaccount/externalaccount.go#L159
			expectedError:    errors.New("externalaccount: Audience must be set"),
			expectedNotFound: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := credentials.DetectDefault(tt.opts)
			testerrors.AssertEqual(t, tt.expectedError, err)
			if !assert.Equal(t, tt.expectedNotFound, IsCredentialsNotFoundError(err)) {
				t.Logf("error: %v", err)
			}
		})
	}
}
