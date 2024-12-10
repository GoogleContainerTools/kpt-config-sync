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
	"fmt"
	"net/http"

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/auth"
)

// Server contains server wide state and settings for the askpass sidecar
type Server struct {
	Email              string
	CredentialProvider auth.CredentialProvider
}

// GitAskPassHandler handles credential requests, fetching a valid auth token
// from the credential manager and writing it to the response.
//
// The underlying Credentials abstraction handles caching the auth token until
// it expires, minus the EarlyTokenRefresh duration.
func (aps *Server) GitAskPassHandler(w http.ResponseWriter, r *http.Request) {
	klog.Infof("handling new askpass request from host: %s", r.Host)

	token, err := auth.FetchToken(r.Context(), aps.CredentialProvider)
	if err != nil {
		klog.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// write the username and password (aka token) to the response
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "username=%s\npassword=%s", aps.Email, token.Value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		klog.Error(err)
	}
}
