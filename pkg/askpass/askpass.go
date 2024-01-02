// Copyright 2023 Google LLC
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

// Package askpass is designed to be used in the askpass sidecar
// to provide GSA authentication services.
package askpass

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"k8s.io/klog/v2"
)

// bufferTime is the approximate amount of time that the credentials should
// remain valid after being returned by the askpass sidecar.
//
// A 5m buffer was chosen as a compromise to avoid git API call errors without
// significantly increasing credential refresh request volume. It's assumed that
// credentials are valid for ~60m, so a 5m early refresh should cause less than
// 10% increase in refresh call volume, while still allowing enough time for the
// askpass service to respond and git to make at least one API call, possibly
// multiple and/or retries.
const bufferTime = 5 * time.Minute

// Server contains server wide state and settings for the askpass sidecar
type Server struct {
	Email string
	token *oauth2.Token
}

// GitAskPassHandler is the main method for clients to ask us for
// credentials
func (aps *Server) GitAskPassHandler(w http.ResponseWriter, r *http.Request) {
	klog.Infof("handling new askpass request from host: %s", r.Host)

	if aps.needNewToken() {
		err := aps.retrieveNewToken(r.Context())
		if err != nil {
			klog.Error(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		klog.Infof("reusing existing oauth2 token, type: %s, expiration: %v",
			aps.token.TokenType, aps.token.Expiry)
	}

	// this this point we should be equipped with all the credentials
	// and it's just a matter of sending it back to the caller.
	password := aps.token.AccessToken
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "username=%s\npassword=%s", aps.Email, password); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		klog.Error(err)
	}
}

// needNewToken will tell us if we have an Oauth2 token that is
// has not expired yet.
func (aps *Server) needNewToken() bool {
	if aps.token == nil {
		return true
	}

	if time.Now().Add(bufferTime).After(aps.token.Expiry) {
		return true
	}

	return false
}

// retrieveNewToken will use the default credentials in order to
// fetch to fetch a new token.  Note the side effect that the
// server token will be replaced in case of a successful retrieval.
func (aps *Server) retrieveNewToken(ctx context.Context) error {
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return fmt.Errorf("error calling google.FindDefaultCredentials: %w", err)
	}
	aps.token, err = creds.TokenSource.Token()
	if err != nil {
		return fmt.Errorf("error retrieveing TokenSource.Token: %w", err)
	}

	klog.Infof("retrieved new Oauth2 token, type: %s, expiration: %v",
		aps.token.TokenType, aps.token.Expiry)
	return nil
}
