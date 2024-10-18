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
	"log"
	"net/http"
	"strings"
	"sync"

	credentials "cloud.google.com/go/iam/credentials/apiv1"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sigstore/cosign/v2/pkg/cosign"
	ociremote "github.com/sigstore/cosign/v2/pkg/oci/remote"
	"github.com/sigstore/cosign/v2/pkg/signature"
	"google.golang.org/api/option"
	credentialspb "google.golang.org/genproto/googleapis/iam/credentials/v1"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	sourceURL     = "configsync.gke.io/source-url"
	sourceCommit  = "configsync.gke.io/source-commit"
	publicKeyPath = "/cosign-key/cosign.pub"
)

var authorized bool
var authMutex sync.Mutex

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

type GoogleAuthKeychain struct {
	accessToken string
}

func (gk *GoogleAuthKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	return authn.FromConfig(authn.AuthConfig{
		Username: "oauth2accesstoken",
		Password: gk.accessToken,
	}), nil
}

// Function to acquire OAuth2 access token programmatically
func getGoogleAccessToken(ctx context.Context) (string, error) {
	client, err := credentials.NewIamCredentialsClient(ctx, option.WithScopes("https://www.googleapis.com/auth/cloud-platform"))
	if err != nil {
		return "", fmt.Errorf("failed to create IAM credentials client: %v", err)
	}
	defer client.Close()

	req := &credentialspb.GenerateAccessTokenRequest{
		Name:  "projects/-/serviceAccounts/e2e-test-ar-reader@peip-monitor.iam.gserviceaccount.com",
		Scope: []string{"https://www.googleapis.com/auth/cloud-platform"},
	}

	resp, err := client.GenerateAccessToken(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to generate access token: %v", err)
	}
	return resp.AccessToken, nil
}

func getAuthenticatedKeychain(ctx context.Context) (authn.Keychain, error) {
	accessToken, err := getGoogleAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Google access token: %v", err)
	}

	return &GoogleAuthKeychain{accessToken: accessToken}, nil
}

func verifyImageSignature(image, commit string) error {
	if image == "" || commit == "" {
		return nil
	}
	ctx := context.Background()
	pubKey, err := signature.LoadPublicKey(ctx, publicKeyPath)
	if err != nil {
		return fmt.Errorf("error loading public key: %v", err)
	}

	keychain, err := getAuthenticatedKeychain(ctx)
	if err != nil {
		return fmt.Errorf("failed to get authenticated keychain: %v", err)
	}

	opts := &cosign.CheckOpts{
		RegistryClientOpts: []ociremote.Option{ociremote.WithRemoteOptions(remote.WithAuthFromKeychain(keychain))},
		SigVerifier:        pubKey,
		IgnoreTlog:         true,
	}

	imageWithDigest, err := replaceTagWithDigest(image, commit)
	if err != nil {
		return fmt.Errorf("failed to replace tag with digest: %v", err)
	}
	ref, err := name.ParseReference(imageWithDigest)
	if err != nil {
		return fmt.Errorf("failed to parse image reference: %v", err)
	}
	_, _, err = cosign.VerifyImageSignatures(ctx, ref, opts)
	if err != nil {
		return fmt.Errorf("image verification failed: %v", err)
	}

	klog.Infof("Image %s verified successfully", imageWithDigest)
	return nil
}

// replaceTagWithDigest replaces the tag in an image URL with the given digest SHA.
func replaceTagWithDigest(imageURL, commitSHA string) (string, error) {
	if !strings.Contains(imageURL, ":") {
		klog.Infof("Source URL does not contain label or digest, skip parsing")
		return "", nil
	}
	imageWithoutTag := strings.Split(imageURL, ":")[0]

	// image URL has digest
	if strings.Contains(imageURL, "@sha256:") {
		URLWithSha := fmt.Sprintf("%s:%s", imageWithoutTag, commitSHA)
		klog.Infof("Replaced existing digest with new digest: %s", URLWithSha)
		return URLWithSha, nil
	}

	// image URL has tag
	URLWithSha := fmt.Sprintf("%s@sha256:%s", imageWithoutTag, commitSHA)
	return URLWithSha, nil
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

	if newAnnotations[sourceURL] != oldAnnotations[sourceURL] ||
		newAnnotations[sourceCommit] != oldAnnotations[sourceCommit] {
		klog.Infof("Detected pre-sync annotation changes")
		if err := verifyImageSignature(newAnnotations[sourceURL], newAnnotations[sourceCommit]); err != nil {
			klog.Errorf("Image validation failed: %v", err)
			response.Allowed = false
			response.Result = &metav1.Status{
				Message: fmt.Sprintf("Image validation failed: %v", err),
			}
		} else {
			klog.Infof("Image validation successful for %s", newAnnotations[sourceURL])
			response.Allowed = true
		}
	} else {
		klog.Infof("No annotation changes detected")
		response.Allowed = true
	}

	admissionReview.Response = response
	if err := json.NewEncoder(w).Encode(admissionReview); err != nil {
		klog.Infof("Failed to encode admission response: %v", err)
	}
}

func main() {
	http.HandleFunc("/validate", handleWebhook)
	log.Println("Starting webhook server on port 443...")
	log.Fatal(http.ListenAndServeTLS(":443", "/tls/tls.crt", "/tls/tls.key", nil))
}
