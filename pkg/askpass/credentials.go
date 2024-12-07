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

package askpass

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	"k8s.io/klog/v2"
)

// DefaultSourceScopes returns the scopes needed to fetch source from CSR and SSM.
func DefaultSourceScopes() []string {
	return []string{"https://www.googleapis.com/auth/cloud-platform"}
}

// IsCredentialsNotFoundError returns true if an error from
// credentials.DetectDefault indicates that no credentials are configured.
func IsCredentialsNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Check the error prefix, because credentials.DetectDefault doesn't use a
	// custom error type.
	// https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/credentials/detect.go#L108
	return strings.HasPrefix(err.Error(), "credentials: could not find default credentials.")
}

// CredentialProvider specifies an interface for anything that can return
// credentials.
type CredentialProvider interface {
	// Credentials returns a TokenProvider or an error.
	// The returned TokenProvider must not be modified.
	Credentials() (auth.TokenProvider, error)
}

// CachingCredentialProvider provides cached default detected credentials.
// The credentials are only detected until successful, then cached forever.
type CachingCredentialProvider struct {
	// Scopes that credentials tokens should have.
	Scopes []string

	mux   sync.RWMutex
	creds auth.TokenProvider
}

// Credentials returns a TokenProvider that manages caching and refreshing auth
// tokens. The token expiration will be logged when refreshed.
//
// The Subject identity is auto-detected:
//   - Node Identity uses a GCP service account supplied by the metadata service.
//   - Workload Identity also uses a GCP service account supplied by the
//     metadata service, specific to the Pod.
//   - Fleet Workload Identity reads the credential config from the path
//     specified by the GOOGLE_APPLICATION_CREDENTIALS env var, which is set by
//     reconciler-manager from the config.kubernetes.io/fleet-workload-identity
//     annotation on the reconciler Pod, copied from the reconciler Deployment.
//     This may use a GCP service account with impersonation by a K8s service
//     account or a K8s service account directly (BYOID).
//   - Application Identity also reads the credential config from the path
//     specified by the GOOGLE_APPLICATION_CREDENTIALS env var, or falling back
//     to the default credential file path.
//
// The scopes are hardcoded to "https://www.googleapis.com/auth/cloud-platform".
func (p *CachingCredentialProvider) Credentials() (auth.TokenProvider, error) {
	if creds := p.getCachedCredentials(); creds != nil {
		return p.creds, nil
	}

	p.mux.Lock()
	defer p.mux.Unlock()
	creds, err := credentials.DetectDefault(&credentials.DetectOptions{
		Scopes: p.Scopes,
		// EarlyTokenRefresh defaults to 3m45s, because the MDS cache TTL is 4m.
		// https://github.com/googleapis/google-cloud-go/blob/auth/v0.12.0/auth/auth.go#L47
		// EarlyTokenRefresh:   3 * time.Minute + 45 * time.Second,
		DisableAsyncRefresh: true, // TODO: Is async refresh safe to use?
	})
	if err != nil {
		return nil, fmt.Errorf("detecting credentials: %w", err)
	}
	p.creds = &LoggingTokenProvider{
		Delegate: creds,
	}
	return p.creds, nil
}

func (p *CachingCredentialProvider) getCachedCredentials() auth.TokenProvider {
	p.mux.RLock()
	defer p.mux.RUnlock()
	return p.creds
}

// LoggingTokenProvider wraps a delegate TokenProvider and logs when a new token
// is fetched. This helps debugging when the token was last refreshed and when
// it will expire.
type LoggingTokenProvider struct {
	Delegate auth.TokenProvider

	// mux protects the cached tokenExpiry from concurrent access
	mux sync.RWMutex
	// tokenExpiry is used as a proxy for the token, to detect when the token
	// has been refreshed, without storing the token itself.
	tokenExpiry time.Time
}

// Token fetches a token from the delegate provider and logs if the token expiry
// has changed.
func (p *LoggingTokenProvider) Token(ctx context.Context) (*auth.Token, error) {
	token, err := p.Delegate.Token(ctx)
	if err != nil {
		return nil, err
	}

	if p.getTokenExpiry() == token.Expiry {
		return token, nil // no change
	}

	p.mux.Lock()
	defer p.mux.Lock()
	p.tokenExpiry = token.Expiry
	klog.Infof("fetched new auth token (type: %s, expiration: %v)",
		token.Type, token.Expiry)
	return token, nil
}

func (p *LoggingTokenProvider) getTokenExpiry() time.Time {
	p.mux.RLock()
	defer p.mux.RLock()
	return p.tokenExpiry
}
