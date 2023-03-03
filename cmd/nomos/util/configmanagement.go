// Copyright 2022 Google LLC
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

package util

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"kpt.dev/configsync/pkg/api/configmanagement"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ConfigManagementName is the name of the ConfigManagement object.
	ConfigManagementName = "config-management"
	// ConfigManagementResource is the config management resource
	ConfigManagementResource = "configmanagements"
	// ConfigManagementVersionName is the field name that indicates the ConfigManagement version.
	ConfigManagementVersionName = "configManagementVersion"
	// ACMOperatorDeployment is the name of ACM Operator Deployment
	ACMOperatorDeployment = "config-management-operator"
	// rootSyncCRDName is the name of RootSync CRD
	rootSyncCRDName = "rootsyncs.configsync.gke.io"
	// ConfigSyncName is the name of the ConfigSync object for OSS installation.
	ConfigSyncName = "config-sync"
	// ReconcilerManagerName is the name of reconciler-manger
	ReconcilerManagerName = "reconciler-manager"
)

// DynamicClient obtains a client based on the supplied REST config.  Can be overridden in tests.
var DynamicClient = dynamic.NewForConfig

// ConfigManagementClient wraps a dynamic resource interface for reading ConfigManagement resources.
type ConfigManagementClient struct {
	resInt dynamic.ResourceInterface
}

// NewConfigManagementClient returns a new ConfigManagementClient.
func NewConfigManagementClient(cfg *rest.Config) (*ConfigManagementClient, error) {
	cl, err := DynamicClient(cfg)
	if err != nil {
		return nil, err
	}
	gvr := v1.SchemeGroupVersion.WithResource(ConfigManagementResource)
	return &ConfigManagementClient{cl.Resource(gvr).Namespace("")}, nil
}

// NestedInt returns the integer value specified by the given path of field names.
// Returns false if a value is not found and an error if value is not a bool.
func (c *ConfigManagementClient) NestedInt(ctx context.Context, fields ...string) (int, error) {
	unstr, err := c.resInt.Get(ctx, ConfigManagementName, metav1.GetOptions{}, "")
	if err != nil {
		return 0, err
	}

	val, _, err := unstructured.NestedInt64(unstr.UnstructuredContent(), fields...)
	if err != nil {
		return 0, errors.Wrap(err, "internal error parsing ConfigManagement")
	}

	return int(val), nil
}

// EnableMultiRepo removes the spec.git field and sets spec.enableMultiRepo to true.
// It returns both the original and the updated ConfigManagement objects.
func (c *ConfigManagementClient) EnableMultiRepo(ctx context.Context) (*unstructured.Unstructured, *unstructured.Unstructured, error) {
	unstr, err := c.resInt.Get(ctx, ConfigManagementName, metav1.GetOptions{}, "")
	if err != nil {
		return nil, nil, err
	}
	orig := unstr.DeepCopy()
	if err := unstructured.SetNestedField(unstr.UnstructuredContent(), true, "spec", "enableMultiRepo"); err != nil {
		return orig, nil, err
	}
	unstructured.RemoveNestedField(unstr.UnstructuredContent(), "spec", "git")
	return orig, unstr, nil
}

// UpdateConfigManagement updates the ConfigManagement object in the API server.
func (c *ConfigManagementClient) UpdateConfigManagement(ctx context.Context, obj *unstructured.Unstructured) error {
	_, err := c.resInt.Update(ctx, obj, metav1.UpdateOptions{}, "")
	return err
}

// NestedBool returns the boolean value specified by the given path of field names.
// Returns false if a value is not found and an error if value is not a bool.
func (c *ConfigManagementClient) NestedBool(ctx context.Context, fields ...string) (bool, error) {
	unstr, err := c.resInt.Get(ctx, ConfigManagementName, metav1.GetOptions{}, "")
	if err != nil {
		return false, err
	}

	val, _, err := unstructured.NestedBool(unstr.UnstructuredContent(), fields...)
	if err != nil {
		return false, errors.Wrap(err, "internal error parsing ConfigManagement")
	}

	return val, nil
}

// NestedString returns the string value specified by the given path of field names.
// Returns empty string if a value is not found and an error if value is not a string.
func (c *ConfigManagementClient) NestedString(ctx context.Context, fields ...string) (string, error) {
	unstr, err := c.resInt.Get(ctx, ConfigManagementName, metav1.GetOptions{}, "")
	if err != nil {
		return "", err
	}

	val, _, err := unstructured.NestedString(unstr.UnstructuredContent(), fields...)
	if err != nil {
		return "", errors.Wrap(err, "internal error parsing ConfigManagement")
	}

	return val, nil
}

// NestedStringSlice returns the string slice specified by the given path of field names.
// Returns nil if a value is not found and an error if value is not a string slice.
func (c *ConfigManagementClient) NestedStringSlice(ctx context.Context, fields ...string) ([]string, error) {
	unstr, err := c.resInt.Get(ctx, ConfigManagementName, metav1.GetOptions{}, "")
	if err != nil {
		return nil, err
	}

	vals, _, err := unstructured.NestedStringSlice(unstr.UnstructuredContent(), fields...)
	if err != nil {
		return nil, errors.Wrap(err, "internal error parsing ConfigManagement")
	}

	return vals, nil
}

// Version returns the version of the ConfigManagement objects.
func (c *ConfigManagementClient) Version(ctx context.Context) (string, error) {
	cmVersion, err := c.NestedString(ctx, "status", ConfigManagementVersionName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return NotInstalledMsg, nil
		}
		return ErrorMsg, err
	}
	if cmVersion == "" {
		cmVersion = UnknownMsg
	}
	return cmVersion, nil
}

// IsMultiRepo returns if the enableMultiRepoMode is true in the ConfigManagement objects.
func (c *ConfigManagementClient) IsMultiRepo(ctx context.Context) (*bool, error) {
	isMulti, err := c.NestedBool(ctx, "spec", "enableMultiRepo")
	if err != nil {
		return nil, err
	}
	return &isMulti, nil
}

// IsOssInstallation will check for the existence of ConfigManagement object, Operator deployment, and RootSync CRD
// If RootSync CRD exist but ConfigManagement and Operator doesn't, it indicates an OSS installation
func IsOssInstallation(ctx context.Context, c *ConfigManagementClient, cl client.Client, ck *kubernetes.Clientset) (bool, error) {
	v, cmErr := c.Version(ctx)
	if cmErr != nil {
		return false, fmt.Errorf("Failed to get the ConfigManagment version: %v", cmErr)
	}
	_, operatorDepErr := ck.AppsV1().Deployments(configmanagement.ControllerNamespace).Get(ctx, ACMOperatorDeployment, metav1.GetOptions{})
	if operatorDepErr != nil && !apierrors.IsNotFound(operatorDepErr) {
		return false, fmt.Errorf("Failed to get the Operator Deployment: %v", operatorDepErr)
	}

	if v != NotInstalledMsg && operatorDepErr == nil {
		return false, nil
	}

	rootSyncCRDErr := cl.Get(ctx, client.ObjectKey{Name: rootSyncCRDName}, &apiextensionsv1.CustomResourceDefinition{})
	if rootSyncCRDErr == nil {
		return true, nil
	}
	err := fmt.Errorf("Failed to get the RootSync CRD: %v", rootSyncCRDErr)
	return false, err
}
