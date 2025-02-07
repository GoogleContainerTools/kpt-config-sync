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
	"os"

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

// getAnnotationByKey extracts a specific annotation by key from the raw JSON data.
func getAnnotationByKey(raw []byte, key string) (string, error) {
	var metadata map[string]interface{}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return "", err
	}

	annotations, ok := metadata["metadata"].(map[string]interface{})["annotations"].(map[string]interface{})
	if !ok {
		klog.Infof("No annotations found in the object")
		return "", nil
	}

	if value, found := annotations[key]; found {
		return fmt.Sprintf("%v", value), nil
	}

	klog.Infof("Annotation %s not found in the object", key)
	return "", nil
}

func verifyImageSignature(ctx context.Context, image string) error {
	if image == "" {
		return nil
	}

	pubKey, err := signature.LoadPublicKey(ctx, publicKeyPath)
	if err != nil {
		return fmt.Errorf("error loading public key: %v", err)
	}

	var registryClientOpts []ociremote.Option
	if os.Getenv("USE_GOOGLE_AUTHENTICATOR") == "true" {
		googleAuth, err := google.NewEnvAuthenticator(ctx)
		if err != nil {
			return err
		}
		registryClientOpts = append(registryClientOpts, ociremote.WithRemoteOptions(remote.WithAuth(googleAuth)))
	}

	opts := &cosign.CheckOpts{
		RegistryClientOpts: registryClientOpts,
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

	response := &admissionv1.AdmissionResponse{
		UID: admissionReview.Request.UID,
	}

	oldImage, err := getAnnotationByKey(admissionReview.Request.OldObject.Raw, imageToSync)
	if err != nil {
		klog.Errorf("Failed to extract old annotations: %v", err)
		response.Result = &metav1.Status{
			Message: fmt.Sprintf("Failed to extract old annotations: %v", err),
		}
		response.Allowed = false
		return
	}

	newImage, err := getAnnotationByKey(admissionReview.Request.Object.Raw, imageToSync)
	if err != nil {
		klog.Errorf("Failed to extract new annotations: %v", err)
		response.Result = &metav1.Status{
			Message: fmt.Sprintf("Failed to extract new annotations: %v", err),
		}
		response.Allowed = false
		return
	}

	if newImage != oldImage {
		klog.Infof("Annotation %s changed from %s to %s", imageToSync, oldImage, newImage)
		if err := verifyImageSignature(r.Context(), newImage); err != nil {
			klog.Errorf("Image verification failed: %v", err)
			response.Allowed = false
			response.Result = &metav1.Status{
				Message: fmt.Sprintf("Image verification failed: %v", err),
			}
		} else {
			klog.Infof("Image verification successful for %s", newImage)
			response.Allowed = true
		}
	} else {
		response.Allowed = true
	}

	admissionReview.Response = response
	if err := json.NewEncoder(w).Encode(admissionReview); err != nil {
		klog.Errorf("Failed to encode admission response: %v", err)
	}
}

func main() {
	http.HandleFunc("/validate", handleWebhook)
	// Use 8443 as a non-privileged port to avoid root privilege to bind to the port
	klog.Info("Starting webhook server on port 8443...")
	klog.Fatal(http.ListenAndServeTLS(":8443", "/tls/server.crt", "/tls/server.key", nil))
}
