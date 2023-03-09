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

package nomostest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go.uber.org/multierr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/e2e/nomostest/portforwarder"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestNotificationWebhookPort is the port exposed by the notification-webhook server
const TestNotificationWebhookPort = 8080
const testNotificationWebhookServer = "test-notification-webhook"
const testNotificationWebhookNamespace = "notification-system-test"
const testNotificationWebhookImage = "gcr.io/stolos-dev/test-notification-webhook:v1.0.0"

// NotificationRecord represents a notification delivery record.
// This contains data used for validation in the e2e tests
type NotificationRecord struct {
	// Message contains the notification payload
	Message string `json:"message,omitempty"`
	// Auth contains auth header from the request. These should be contrived auth
	// values used by test cases and not real credentials.
	Auth string `json:"auth,omitempty"`
}

// NotificationRecords represents a list of NotificationRecord which were delivered
// over some time interval
type NotificationRecords struct {
	// Records is a list of notification deliveries that were recorded
	Records []NotificationRecord `json:"records,omitempty"`
}

// NotificationServer is an in-cluster test component used for capturing notifications.
// It receives notifications from the Config Sync notification controller and
// can be queried by the test framework.
type NotificationServer struct {
	portForwarder *portforwarder.PortForwarder
}

func (ns *NotificationServer) install(nt *NT) error {
	nt.T.Helper()

	objs := notificationWebhookServer()

	for _, o := range objs {
		err := nt.KubeClient.Create(o)
		if err != nil {
			return fmt.Errorf("installing %v %s: %v", o.GetObjectKind().GroupVersionKind(),
				client.ObjectKey{Name: o.GetName(), Namespace: o.GetNamespace()}, err)
		}
	}

	return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), testNotificationWebhookServer, testNotificationWebhookNamespace)
}

func (ns *NotificationServer) uninstall(nt *NT) error {
	namespace := notificationWebhookNamespace()
	err := nt.KubeClient.Delete(namespace)
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (ns *NotificationServer) portForward(nt *NT) error {
	nt.T.Helper()

	ns.portForwarder = nt.newPortForwarder(
		testNotificationWebhookNamespace,
		testNotificationWebhookServer,
		fmt.Sprintf(":%d", TestNotificationWebhookPort),
	)
	nt.startPortForwarder(
		testNotificationWebhookNamespace,
		testNotificationWebhookServer,
		ns.portForwarder,
	)
	return nil
}

func (ns *NotificationServer) url() (string, error) {
	localPort, err := ns.portForwarder.LocalPort()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://localhost:%d/", localPort), nil
}

// DoGet performs a GET request to the in-cluster notification server
func (ns *NotificationServer) DoGet() (nr *NotificationRecords, retErr error) {
	url, err := ns.url()
	if err != nil {
		return nil, err
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			retErr = multierr.Append(retErr, fmt.Errorf("error closing GET response body: %v", err))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code. want %d, got %d", http.StatusOK, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading body: %v", err)
	}
	records := NotificationRecords{}
	if err := json.Unmarshal(body, &records); err != nil {
		return nil, fmt.Errorf("error unmarshalling response: %v", err)
	}
	return &records, nil
}

// DoDelete performs a DELETE request to the in-cluster notification server
func (ns *NotificationServer) DoDelete() (retErr error) {
	httpClient := &http.Client{}
	url, err := ns.url()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("error constructing request: %v", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error performing Delete request: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			retErr = multierr.Append(retErr, fmt.Errorf("error closing DELETE response body: %v", err))
		}
	}()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code. want %d, got %d", http.StatusNoContent, resp.StatusCode)
	}
	return nil
}

func testNotificationWebhookServerSelector() map[string]string {
	// Note that maps are copied by reference into objects.
	// If this were just a variable, then concurrent usages by Clients may result
	// in concurrent map writes (and thus flaky test panics).
	return map[string]string{"app": testNotificationWebhookServer}
}

func notificationWebhookServer() []client.Object {
	objs := []client.Object{
		notificationWebhookNamespace(),
		notificationWebhookDeployment(),
		notificationWebhookService(),
	}
	return objs
}

func notificationWebhookNamespace() *corev1.Namespace {
	return fake.NamespaceObject(testNotificationWebhookNamespace)
}

func notificationWebhookDeployment() *appsv1.Deployment {
	deployment := fake.DeploymentObject(core.Name(testNotificationWebhookServer),
		core.Namespace(testNotificationWebhookNamespace),
		core.Labels(testNotificationWebhookServerSelector()),
	)
	deployment.Spec = appsv1.DeploymentSpec{
		MinReadySeconds: 2,
		Strategy:        appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
		Selector:        &v1.LabelSelector{MatchLabels: testNotificationWebhookServerSelector()},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: v1.ObjectMeta{
				Labels: testNotificationWebhookServerSelector(),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  testNotificationWebhookServer,
						Image: testNotificationWebhookImage,
						Ports: []corev1.ContainerPort{{ContainerPort: TestNotificationWebhookPort}},
					},
				},
			},
		},
	}
	return deployment
}

func notificationWebhookService() *corev1.Service {
	service := fake.ServiceObject(
		core.Name(testNotificationWebhookServer),
		core.Namespace(testNotificationWebhookNamespace),
	)
	service.Spec.Selector = testNotificationWebhookServerSelector()
	service.Spec.Ports = []corev1.ServicePort{
		{Name: "http", Port: TestNotificationWebhookPort},
	}
	return service
}
