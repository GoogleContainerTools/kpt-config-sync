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

package migrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/cmd/nomos/status"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func migrateConfigManagement(ctx context.Context, cc *status.ClusterClient, kubeCtx string) error {
	isOSS, err := util.IsOssInstallation(ctx, cc.ConfigManagement, cc.Client, cc.K8sClient)
	if err != nil {
		return err
	}
	if isOSS {
		printNotice("The cluster is already running as an OSS installation.")
		return nil
	}
	if isHNCEnabled, err := cc.ConfigManagement.IsHNCEnabled(ctx); err != nil {
		return err
	} else if isHNCEnabled {
		return fmt.Errorf("hierarchy Controller is enabled on the ConfigManagement object. It must be disabled before migrating")
	}

	fmt.Printf("Removing ConfigManagement on cluster %q ...\n", kubeCtx)
	cmYamlFile, err := saveConfigManagementOperatorYaml(ctx, cc, kubeCtx)
	if err != nil {
		return err
	}
	printHint(`Resources for the ConfigManagement operator have been saved in a temp folder. If the migration process is terminated, it can be recovered manually by running the following commands:
  kubectl delete deployment -n config-management-system config-management-operator --ignore-not-found --cascade=foreground && \
  (kubectl patch configmanagement config-management --type="merge" -p '{"metadata":{"finalizers":[]}}' ; \
  kubectl delete configmanagement config-management --cascade=orphan --ignore-not-found) && \
  kubectl delete -f %s --ignore-not-found`, cmYamlFile)

	if dryRun {
		dryrunConfigManagement()
		return nil
	}
	if err := executeConfigManagementMigration(ctx, cc); err != nil {
		return err
	}
	return nil
}

func dryrunConfigManagement() {
	printInfo(deletingConfigManagement)
}

// getOperatorObjects returns a list of objects associated with the ConfigManagement
// operator installation. The objects are returned in the order in which they
// should be applied. They should be deleted in the reverse order.
func getOperatorObjects() []*unstructured.Unstructured {
	var objs []*unstructured.Unstructured

	crd := &unstructured.Unstructured{}
	crd.SetGroupVersionKind(kinds.CustomResourceDefinitionV1())
	crd.SetName(util.ConfigManagementCRDName)
	objs = append(objs, crd)

	cm := &unstructured.Unstructured{}
	cm.SetGroupVersionKind(kinds.ConfigManagement())
	cm.SetName(util.ConfigManagementName)
	objs = append(objs, cm)

	sa := &unstructured.Unstructured{}
	sa.SetGroupVersionKind(kinds.ServiceAccount())
	sa.SetName(util.ACMOperatorDeployment)
	sa.SetNamespace(configmanagement.ControllerNamespace)
	objs = append(objs, sa)

	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(kinds.ClusterRole())
	cr.SetName(util.ACMOperatorDeployment)
	objs = append(objs, cr)

	crb := &unstructured.Unstructured{}
	crb.SetGroupVersionKind(kinds.ClusterRoleBinding())
	crb.SetName(util.ACMOperatorDeployment)
	objs = append(objs, crb)

	dep := &unstructured.Unstructured{}
	dep.SetGroupVersionKind(kinds.Deployment())
	dep.SetName(util.ACMOperatorDeployment)
	dep.SetNamespace(configmanagement.ControllerNamespace)
	objs = append(objs, dep)

	return objs
}

// executeConfigManagementMigration performs the migration from ConfigManagement
// installation to a standalone OSS Config Sync installation. It's assumed that
// the ConfigManagement installation is already running in the multi-repo mode.
func executeConfigManagementMigration(ctx context.Context, sc *status.ClusterClient) error {
	printInfo(waitingForConfigSyncCRDs)
	if err := waitForMultiRepoCRDsToBeEstablished(ctx, sc.Client); err != nil {
		return err
	}
	printInfo("The following CRDs have been established: %s", configSyncCRDs)

	printInfo(waitingForReconcilerManager)
	if err := waitForPodToBeRunning(ctx, sc.K8sClient, configmanagement.ControllerNamespace, "app=reconciler-manager"); err != nil {
		return err
	}
	printInfo("The reconciler-manager Pod is running")

	printInfo(waitingForRGManager)
	if err := waitForPodToBeRunning(ctx, sc.K8sClient, configmanagement.RGControllerNamespace, "configsync.gke.io/deployment-name=resource-group-controller-manager"); err != nil {
		return err
	}
	printInfo("The resource-group-controller-manager Pod is running")

	// Delete the config-management-operator objects in the reverse order in which
	// they must be established.
	printInfo("Deleting the ConfigManagement operator")
	operatorObjects := getOperatorObjects()
	for idx := range operatorObjects {
		obj := operatorObjects[len(operatorObjects)-1-idx]
		printInfo("deleting object: %s %s/%s",
			obj.GetObjectKind().GroupVersionKind(), obj.GetNamespace(), obj.GetName())
		propPolicy := client.PropagationPolicy(metav1.DeletePropagationForeground)
		if obj.GetObjectKind().GroupVersionKind() == kinds.ConfigManagement() {
			// The operator adds ownerReferences pointing to the ConfigManagement object,
			// so "Orphan" must be specified to avoid deleting the managed objects.
			propPolicy = client.PropagationPolicy(metav1.DeletePropagationOrphan)
			if err := sc.ConfigManagement.RemoveFinalizers(ctx); err != nil {
				return err
			}
		}
		if err := sc.Client.Delete(ctx, obj, propPolicy); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
		}
		key := client.ObjectKeyFromObject(obj)
		// wait for object to be not found
		err := recheck(func() error {
			err := sc.Client.Get(ctx, key, obj)
			if apierrors.IsNotFound(err) {
				return nil
			} else if err == nil {
				return fmt.Errorf("expected %s object %s to be NotFound, but still exists",
					obj.GroupVersionKind(), key)
			}
			return err
		})
		if err != nil {
			return err
		}
	}
	printInfo("The ConfigManagement Operator is deleted")
	return nil
}

// saveConfigManagementOperatorYaml saves the original config-management-operator
// yaml to a file. The migration will uninstall the operator, so this will enable
// users to reinstall if they want to revert the migration.
func saveConfigManagementOperatorYaml(ctx context.Context, sc *status.ClusterClient, context string) (string, error) {
	dir := filepath.Join(os.TempDir(), configManagementMigrateDir, context)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return "", err
	}
	operatorObjects := getOperatorObjects()
	// Fetch original cluster objects, so they can be saved to a file
	for _, obj := range operatorObjects {
		key := client.ObjectKeyFromObject(obj)
		if err := sc.Client.Get(ctx, key, obj); err != nil {
			return "", fmt.Errorf("reading object %s from cluster: %w", key, err)
		}
	}

	var content []byte
	for idx, uObj := range operatorObjects {
		if idx != 0 { // concatenate yaml using "---" separator
			content = append(content, []byte("---\n")...)
		}
		yamlObj, err := yaml.Marshal(uObj)
		if err != nil {
			return "", err
		}
		content = append(content, yamlObj...)
	}
	yamlFile := filepath.Join(dir, cmOperatorYAMLFile)
	if err := os.WriteFile(yamlFile, content, 0644); err != nil {
		return yamlFile, err
	}
	printInfo("The original ConfigManagement Operator objects are saved in %q", yamlFile)
	return yamlFile, nil
}
