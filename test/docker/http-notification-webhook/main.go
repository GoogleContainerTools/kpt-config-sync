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

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"kpt.dev/configsync/e2e/nomostest"
)

// mux is used to synchronize operations across the various endpoints
var mux sync.Mutex

// store is a simple in memory storage of notification records.
var store nomostest.NotificationRecords

func doGet(w http.ResponseWriter, r *http.Request) {
	mux.Lock()
	defer mux.Unlock()
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(store)
	if err != nil {
		msg := fmt.Sprintf("error encoding response: %v", err)
		log.Print(msg)
		_, writeErr := io.WriteString(w, msg)
		if writeErr != nil {
			log.Printf("error writing string for GET response: %v", writeErr)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func doPost(w http.ResponseWriter, r *http.Request) {
	mux.Lock()
	defer mux.Unlock()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		msg := fmt.Sprintf("error reading body: %v", err)
		log.Print(msg)
		_, writeErr := io.WriteString(w, msg)
		if writeErr != nil {
			log.Printf("error writing string for POST response: %v", writeErr)
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	record := nomostest.NotificationRecord{
		Message: string(body),
		Auth:    r.Header.Get("Authorization"),
	}
	store.Records = append(store.Records, record)
	w.WriteHeader(http.StatusCreated)
}

func doDelete(w http.ResponseWriter, r *http.Request) {
	mux.Lock()
	defer mux.Unlock()
	store.Records = nil
	w.WriteHeader(http.StatusNoContent)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s", r.Method, r.URL.String())
	switch strings.ToUpper(r.Method) {
	case http.MethodGet:
		doGet(w, r)
	case http.MethodPost:
		doPost(w, r)
	case http.MethodDelete:
		doDelete(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// This is a simple http server used for recording notification deliveries in e2e
// tests.
func main() {
	http.HandleFunc("/", rootHandler)

	port := fmt.Sprintf(":%d", nomostest.TestNotificationWebhookPort)
	log.Printf("listening on port %s", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
