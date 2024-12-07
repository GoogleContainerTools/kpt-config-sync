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

package oci

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"kpt.dev/configsync/pkg/askpass"
)

// DefaultSourceScopes returns the scopes needed to fetch source from GCR & GAR.
func DefaultSourceScopes() []string {
	return []string{"https://www.googleapis.com/auth/cloud-platform"}
}

// CredentialAuthenticator wraps a CredentialProvider to generate AuthConfigs
// used by the container registry client.
type CredentialAuthenticator struct {
	// CredentialProvider provides credentials that are used to fetch auth tokens.
	CredentialProvider askpass.CredentialProvider
}

var _ authn.Authenticator = &CredentialAuthenticator{}

// Authorization returns the value to use in an http transport's Authorization header.
func (a *CredentialAuthenticator) Authorization() (*authn.AuthConfig, error) {
	creds, err := a.CredentialProvider.Credentials()
	if err != nil {
		return nil, err
	}
	// TODO: update authn.Authenticator.Authorization to pass in a Context
	token, err := creds.Token(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("fetching auth token: %w", err)
	}
	return &authn.AuthConfig{
		// Username hard-coded to match tokenSourceAuth from go-containerregistry.
		// https://github.com/google/go-containerregistry/blob/v0.20.2/pkg/v1/google/auth.go#L123
		Username: "_token",
		Password: token.Value,
	}, nil
}
