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
	"sync"
	"time"

	goauth "cloud.google.com/go/auth"
	"k8s.io/klog/v2"
)

// LoggingTokenProvider wraps a delegate TokenProvider and logs when a new token
// is fetched. This helps debugging when the token was last refreshed and when
// it will expire.
type LoggingTokenProvider struct {
	Delegate goauth.TokenProvider

	// mux protects the cached tokenExpiry from concurrent access
	mux sync.RWMutex
	// tokenExpiry is used as a proxy for the token, to detect when the token
	// has been refreshed, without storing the token itself.
	tokenExpiry time.Time
}

// Token fetches a token from the delegate provider and logs if the token expiry
// has changed.
func (p *LoggingTokenProvider) Token(ctx context.Context) (*goauth.Token, error) {
	token, err := p.Delegate.Token(ctx)
	if err != nil {
		return nil, err
	}

	if p.getTokenExpiry() == token.Expiry {
		return token, nil // no change
	}

	p.mux.Lock()
	defer p.mux.Unlock()
	p.tokenExpiry = token.Expiry
	klog.Infof("fetched new auth token (type: %s, expiration: %v)",
		token.Type, token.Expiry)
	return token, nil
}

func (p *LoggingTokenProvider) getTokenExpiry() time.Time {
	p.mux.RLock()
	defer p.mux.RUnlock()
	return p.tokenExpiry
}
