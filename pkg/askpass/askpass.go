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

	if time.Now().After(aps.token.Expiry) {
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
