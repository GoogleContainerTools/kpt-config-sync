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
	"context"

	goauth "cloud.google.com/go/auth"
)

// FakeCredentialProvider always provides the specified TokenProvider and Error.
type FakeCredentialProvider struct {
	// CredentialsOut is returned by every Credentials call
	CredentialsOut goauth.TokenProvider
	// CredentialsError is returned by every Credentials call
	CredentialsError error
}

// Credentials always returns the specified TokenProvider and Error.
func (p *FakeCredentialProvider) Credentials() (goauth.TokenProvider, error) {
	return p.CredentialsOut, p.CredentialsError
}

// FakeTokenProvider always provides the specified Token and Error.
type FakeTokenProvider struct {
	// TokenOut is returned by every Token call
	TokenOut *goauth.Token
	// TokenError is returned by every Token call
	TokenError error
}

// Token always returns the specified Token and Error
func (p *FakeTokenProvider) Token(_ context.Context) (*goauth.Token, error) {
	return p.TokenOut, p.TokenError
}
