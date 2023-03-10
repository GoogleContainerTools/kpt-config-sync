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
	"strings"

	"go.uber.org/multierr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util"
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
	localPort int
}

func (ns *NotificationServer) install(nt *NT) error {
	nt.T.Helper()

	objs := notificationWebhookServer()

	for _, o := range objs {
		err := nt.Create(o)
		if err != nil {
			return fmt.Errorf("installing %v %s: %v", o.GetObjectKind().GroupVersionKind(),
				client.ObjectKey{Name: o.GetName(), Namespace: o.GetNamespace()}, err)
		}
	}

	return WatchForCurrentStatus(nt, kinds.Deployment(), testNotificationWebhookServer, testNotificationWebhookNamespace)
}

func (ns *NotificationServer) uninstall(nt *NT) error {
	namespace := notificationWebhookNamespace()
	err := nt.Delete(namespace)
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (ns *NotificationServer) portForward(nt *NT) error {
	nt.T.Helper()

	podName, err := notificationWebhookPodName(nt)
	if err != nil {
		return err
	}

	if ns.localPort == 0 {
		port, err := nt.ForwardToFreePort(
			testNotificationWebhookNamespace,
			podName,
			fmt.Sprintf(":%d", TestNotificationWebhookPort),
		)
		if err != nil {
			return err
		}
		ns.localPort = port
	}
	return nil
}

func (ns *NotificationServer) url() string {
	return fmt.Sprintf("http://localhost:%d/", ns.localPort)
}

// DoGet performs a GET request to the in-cluster notification server
func (ns *NotificationServer) DoGet() (nr *NotificationRecords, retErr error) {
	resp, err := http.Get(ns.url())
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
	req, err := http.NewRequest(http.MethodDelete, ns.url(), nil)
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

func notificationWebhookPodName(nt *NT) (string, error) {
	podList := &corev1.PodList{}
	err := nt.List(podList, client.InNamespace(testNotificationWebhookNamespace))
	if err != nil {
		return "", err
	}
	if nPods := len(podList.Items); nPods != 1 {
		podsJSON, err := json.MarshalIndent(podList, "", "  ")
		if err != nil {
			return "", err
		}
		nt.T.Log(string(podsJSON))
		return "", fmt.Errorf("got len(podList.Items) = %d, want 1", nPods)
	}
	podName := podList.Items[0].Name
	return podName, nil
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

const NotificationConfigMapRef = "test-notification-cm"
const NotificationSecretRef = "test-notification-secret"

// SubscribeAnnotationKey forms the subscription annotation key
func SubscribeAnnotationKey(trigger, service string) string {
	return fmt.Sprintf("%s/subscribe.%s.%s", util.AnnotationsPrefix, trigger, service)
}

// SubscribeRepoSyncNotification modifies the RepoSync in-place to enable notifications
func SubscribeRepoSyncNotification(repoSync *v1beta1.RepoSync, trigger, service string) {
	annotations := repoSync.GetAnnotations()
	annotations[SubscribeAnnotationKey(trigger, service)] = ""
	repoSync.Spec.NotificationConfig = &v1beta1.NotificationConfig{
		ConfigMapRef: &v1beta1.ConfigMapReference{Name: NotificationConfigMapRef},
		SecretRef:    &v1beta1.SecretReference{Name: NotificationSecretRef},
	}
}

// UnsubscribeRepoSyncNotification modifies the RepoSync in-place to disable notifications
func UnsubscribeRepoSyncNotification(repoSync *v1beta1.RepoSync) {
	annotations := repoSync.GetAnnotations()
	for a := range annotations {
		if strings.HasPrefix(a, util.AnnotationsPrefix) {
			delete(annotations, a)
		}
	}
	repoSync.Spec.NotificationConfig = nil
}

// SubscribeRootSyncNotification modifies the RootSync in-place to enable notifications
func SubscribeRootSyncNotification(rootSync *v1beta1.RootSync, trigger, service string) {
	annotations := rootSync.GetAnnotations()
	subscribeAnnotation := SubscribeAnnotationKey("on-sync-synced", "local")
	annotations[subscribeAnnotation] = ""
	rootSync.Spec.NotificationConfig = &v1beta1.NotificationConfig{
		ConfigMapRef: &v1beta1.ConfigMapReference{Name: NotificationConfigMapRef},
		SecretRef:    &v1beta1.SecretReference{Name: NotificationSecretRef},
	}
}

// UnsubscribeRootSyncNotification modifies the RootSync in-place to disable notifications
func UnsubscribeRootSyncNotification(rootSync *v1beta1.RootSync) {
	annotations := rootSync.GetAnnotations()
	for a := range annotations {
		if strings.HasPrefix(a, util.AnnotationsPrefix) {
			delete(annotations, a)
		}
	}

	rootSync.Spec.NotificationConfig = nil
}

// NotificationConfigMap creates a notification ConfigMap with the provided data
func NotificationConfigMap(nt *NT, ns string, mutators ...ConfigMapMutator) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	cm.Name = NotificationConfigMapRef
	cm.Namespace = ns
	cm.Data = map[string]string{}
	for _, mut := range mutators {
		mut(cm.Data)
	}
	err := nt.Create(cm)
	nt.T.Cleanup(func() {
		if err := nt.Delete(cm); err != nil && !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	})
	return cm, err
}

// NotificationSecret creates a notification Secret with the provided data
func NotificationSecret(nt *NT, ns string, mutators ...SecretMutator) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	secret.Name = NotificationSecretRef
	secret.Namespace = ns
	secret.Data = map[string][]byte{}
	for _, mut := range mutators {
		mut(secret.Data)
	}
	err := nt.Create(secret)
	nt.T.Cleanup(func() {
		if err := nt.Delete(secret); err != nil && !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	})
	return secret, err
}
