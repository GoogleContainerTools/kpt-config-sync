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

package askpass

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// GitAskPassHandler performs a basic "smoke test"
// which assumes that we have a token already and we don't
// need to call to any external services to get another one.
func TestCachedToken(t *testing.T) {
	req, err := http.NewRequest("GET", "/git_askpass", nil)
	if err != nil {
		t.Fatal(err)
	}

	token := &oauth2.Token{
		AccessToken:  "0xBEEFCAFE",
		TokenType:    "Bearer",
		RefreshToken: "0xDEADC0DE",
		// Expiry needs to be long enough to account for early refresh (5m).
		Expiry: time.Now().Add(time.Minute * 6),
	}

	aps := &Server{
		Email: "foo@bar.com",
		token: token,
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(aps.GitAskPassHandler)

	handler.ServeHTTP(rr, req)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	// Check the response body is what we expect.
	expected := fmt.Sprintf("username=%s\npassword=%s", aps.Email, token.AccessToken)
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), expected)
	}
}
