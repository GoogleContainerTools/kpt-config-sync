// Copyright 2022 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package flowcontrol

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	flowcontrolapi "k8s.io/api/flowcontrol/v1beta2"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
)

const pingPath = "/livez/ping"

// IsEnabled returns true if the server has the PriorityAndFairness flow control
// filter enabled. This check performs a GET request to the /version endpoint
// and looks for the presence of the `X-Kubernetes-PF-FlowSchema-UID` header.
func IsEnabled(ctx context.Context, config *rest.Config) (bool, error) {
	// Build a RoundTripper from the provided REST client config.
	// RoundTriper handles TLS, auth, auth proxy, user agent, impersonation, and
	// debug logs. It also provides acess to the response headers, unlike the
	// REST client. And we can't just use an HTTP client, because the version
	// endpoint may or may not require auth, depending on RBAC config.
	tcfg, err := config.TransportConfig()
	if err != nil {
		return false, fmt.Errorf("building transport config: %w", err)
	}
	roudtripper, err := transport.New(tcfg)
	if err != nil {
		return false, fmt.Errorf("building round tripper: %w", err)
	}

	// Build the base apiserver URL from the provided REST client config.
	url, err := serverURL(config)
	if err != nil {
		return false, fmt.Errorf("building server URL: %w", err)
	}

	// Use the ping endpoint, because it's small and fast.
	// It's alpha in v1.23+, but a 404 will still have the flowcontrol headers.
	// Replacing the path is safe, because DefaultServerURL will have errored
	// if it wasn't empty from the config.
	url.Path = pingPath

	// Build HEAD request with an empty body.
	req, err := http.NewRequestWithContext(ctx, "HEAD", url.String(), nil)
	if err != nil {
		return false, fmt.Errorf("building request: %w", err)
	}

	if config.UserAgent != "" {
		req.Header.Set("User-Agent", config.UserAgent)
	}

	// We don't care what the response body is.
	// req.Header.Set("Accept", "text/plain")

	// Perform the request.
	resp, err := roudtripper.RoundTrip(req)
	if err != nil {
		return false, fmt.Errorf("making %s request: %w", pingPath, err)
	}
	// Probably nil for HEAD, but check anyway.
	if resp.Body != nil {
		// Always close the response body, to free up resources.
		err := resp.Body.Close()
		if err != nil {
			return false, fmt.Errorf("closing response body: %v", err)
		}
	}

	// If the response has one of the flowcontrol headers,
	// that means the flowcontrol filter is enabled.
	// There are two headers, but they're always both set by FlowControl.
	// So we only need to check one.
	// key = flowcontrolapi.ResponseHeaderMatchedPriorityLevelConfigurationUID
	key := flowcontrolapi.ResponseHeaderMatchedFlowSchemaUID
	if value := resp.Header.Get(key); value != "" {
		// We don't care what the value is (always a UID).
		// We just care that the header is present.
		return true, nil
	}
	return false, nil
}

// serverUrl returns the base URL for the cluster based on the supplied config.
// Host and Version are required. GroupVersion is ignored.
// Based on `defaultServerUrlFor` from k8s.io/client-go@v0.23.2/rest/url_utils.go
func serverURL(config *rest.Config) (*url.URL, error) {
	// TODO: move the default to secure when the apiserver supports TLS by default
	// config.Insecure is taken to mean "I want HTTPS but don't bother checking the certs against a CA."
	hasCA := len(config.CAFile) != 0 || len(config.CAData) != 0
	hasCert := len(config.CertFile) != 0 || len(config.CertData) != 0
	defaultTLS := hasCA || hasCert || config.Insecure
	host := config.Host
	if host == "" {
		host = "localhost"
	}

	hostURL, _, err := rest.DefaultServerURL(host, config.APIPath, schema.GroupVersion{}, defaultTLS)
	return hostURL, err
}
