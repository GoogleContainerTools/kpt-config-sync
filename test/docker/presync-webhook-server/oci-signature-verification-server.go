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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/google"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sigstore/cosign/v2/pkg/cosign"
	ociremote "github.com/sigstore/cosign/v2/pkg/oci/remote"
	"github.com/sigstore/cosign/v2/pkg/signature"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

const (
	imageToSync   = "configsync.gke.io/image-to-sync"
	publicKeyPath = "/cosign-key/cosign.pub"
)

// getAnnotations extracts annotations from the raw JSON data.
func getAnnotations(raw []byte) (map[string]string, error) {
	var metadata map[string]interface{}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, err
	}
	if annotations, ok := metadata["metadata"].(map[string]interface{})["annotations"].(map[string]interface{}); ok {
		annotationsMap := make(map[string]string)
		for k, v := range annotations {
			annotationsMap[k] = fmt.Sprintf("%v", v)
		}
		return annotationsMap, nil
	}
	return nil, fmt.Errorf("no annotations found")
}

func verifyImageSignature(image string) error {
	if image == "" {
		return nil
	}

	ctx := context.Background()
	pubKey, err := signature.LoadPublicKey(ctx, publicKeyPath)
	if err != nil {
		return fmt.Errorf("error loading public key: %v", err)
	}

	googleAuth, err := google.NewEnvAuthenticator(ctx)
	if err != nil {
		return err
	}

	opts := &cosign.CheckOpts{
		RegistryClientOpts: []ociremote.Option{ociremote.WithRemoteOptions(remote.WithAuth(googleAuth))},
		SigVerifier:        pubKey,
		IgnoreTlog:         true,
	}

	ref, err := name.ParseReference(image)
	if err != nil {
		return fmt.Errorf("failed to parse image reference: %v", err)
	}
	_, _, err = cosign.VerifyImageSignatures(ctx, ref, opts)
	if err != nil {
		return fmt.Errorf("image verification failed for %s: %v", image, err)
	}

	klog.Infof("Image %s verified successfully", image)
	return nil
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		klog.Errorf("Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var admissionReview admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &admissionReview); err != nil {
		klog.Errorf("Failed to unmarshal admission review: %v", err)
		http.Error(w, "Failed to unmarshal admission review", http.StatusBadRequest)
		return
	}

	oldAnnotations, err := getAnnotations(admissionReview.Request.OldObject.Raw)
	if err != nil {
		klog.Errorf("Failed to extract old annotations: %v", err)
	}

	newAnnotations, err := getAnnotations(admissionReview.Request.Object.Raw)
	if err != nil {
		klog.Errorf("Failed to extract new annotations: %v", err)
	}

	response := &admissionv1.AdmissionResponse{
		UID: admissionReview.Request.UID,
	}

	if newAnnotations[imageToSync] != oldAnnotations[imageToSync] {
		klog.Infof("Annotation image-to-sync changed from %s to %s", oldAnnotations[imageToSync], newAnnotations[imageToSync])
		if err := verifyImageSignature(newAnnotations[imageToSync]); err != nil {
			klog.Errorf("Image verification failed: %v", err)
			response.Allowed = false
			response.Result = &metav1.Status{
				Message: fmt.Sprintf("Image verification failed: %v", err),
			}
		} else {
			klog.Infof("Image verification successful for %s", newAnnotations[imageToSync])
			response.Allowed = true
		}
	} else {
		response.Allowed = true
	}

	admissionReview.Response = response
	if err := json.NewEncoder(w).Encode(admissionReview); err != nil {
		klog.Infof("Failed to encode admission response: %v", err)
	}
}

func main() {
	http.HandleFunc("/validate", handleWebhook)
	klog.Info("Starting webhook server on port 443...")
	klog.Fatal(http.ListenAndServeTLS(":443", "/tls/server.crt", "/tls/server.key", nil))
}
