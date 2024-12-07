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
	"testing"

	"cloud.google.com/go/auth/credentials"
	"github.com/stretchr/testify/assert"
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
	tests := []struct {
		name             string
		opts             *credentials.DetectOptions
		expectedErrorMsg string
		expectedNotFound bool
	}{
		{
			name: "not found",
			opts: &credentials.DetectOptions{},
			expectedErrorMsg: "credentials: could not find default credentials. " +
				"See https://cloud.google.com/docs/authentication/external/set-up-adc for more information",
			expectedNotFound: true,
		},
		{
			name: "missing file",
			// Parser: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/credentials/detect.go#L223
			// Schema: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/internal/credsfile/filetype.go#L31
			opts: &credentials.DetectOptions{
				CredentialsFile: "/path/to/nothing",
			},
			expectedErrorMsg: "open /path/to/nothing: no such file or directory",
			expectedNotFound: false,
		},
		{
			name: "json - invalid",
			// Parser: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/credentials/detect.go#L223
			// Schema: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/internal/credsfile/filetype.go#L31
			opts: &credentials.DetectOptions{
				CredentialsJSON: []byte(`123`),
			},
			expectedErrorMsg: "json: cannot unmarshal number into Go value of type credsfile.fileTypeChecker",
			expectedNotFound: false,
		},
		{
			name: "json - empty",
			// Parser: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/credentials/detect.go#L223
			// Schema: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/internal/credsfile/filetype.go#L31
			opts: &credentials.DetectOptions{
				CredentialsJSON: []byte(`{}`),
			},
			expectedErrorMsg: "credentials: unsupported filetype '\\x00'",
			expectedNotFound: false,
		},
		{
			name: "json - unsupported filetype",
			// Parser: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/credentials/detect.go#L223
			// Schema: https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/internal/credsfile/filetype.go#L31
			opts: &credentials.DetectOptions{
				CredentialsJSON: []byte(`{"invalid-type":{}}`),
			},
			expectedErrorMsg: "credentials: unsupported filetype '\\x00'",
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
			expectedErrorMsg: "auth: email must be provided",
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
			expectedErrorMsg: "externalaccount: Audience must be set",
			expectedNotFound: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := credentials.DetectDefault(tt.opts)
			if tt.expectedErrorMsg != "" {
				assert.Equal(t, tt.expectedErrorMsg, err.Error())
			}
			if !assert.Equal(t, tt.expectedNotFound, IsCredentialsNotFoundError(err)) {
				t.Logf("error: %v", err)
			}
		})
	}
}
