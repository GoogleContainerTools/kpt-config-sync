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

package migrate

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/cmd/nomos/status"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	migrateDir       = "nomos-migrate"
	rootSyncYamlFile = "root-sync.yaml"
	cmOrigYAMLFile   = "cm-original.yaml"
	cmMultiYAMLFile  = "cm-multi.yaml"

	updatingConfigManagement    = "Updating the ConfigManagement object ..."
	waitingForRootSyncCRD       = "Waiting for the RootSync CRD to be established ..."
	creatingRootSync            = "Creating the RootSync object ..."
	waitingForReconcilerManager = "Waiting for the reconciler-manager Pod to be ready ..."
	waitingForRootReconciler    = "Waiting for the root-reconciler Pod to be ready ..."
	migrationSuccess            = "The migration process is done. Please check the sync status with `nomos status`"

	defaultWaitTimeout = 10 * time.Minute
)

var dryRun bool
var waitTimeout time.Duration

func init() {
	Cmd.Flags().StringSliceVar(&flags.Contexts, "contexts", nil,
		`Accepts a comma-separated list of contexts to use in multi-cluster environments. Defaults to the current context. Use "all" for all contexts.`)
	Cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		`If enabled, only prints the migration output.`)
	Cmd.Flags().DurationVar(&flags.ClientTimeout, "connect-timeout", flags.DefaultClusterClientTimeout, "Timeout for connecting to each cluster")
	Cmd.Flags().DurationVar(&waitTimeout, "wait-timeout", defaultWaitTimeout, "Timeout for waiting for condition to be true")
}

// Cmd performs the migration from mono-repo to multi-repo for all the provided contexts.
var Cmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrates to the new Config Sync architecture by enabling the multi-repo mode.",
	Long:  "Migrates to the new Config Sync architecture by enabling the multi-repo mode. It provides you with additional features and gives you the flexibility to sync to a single repository, or multiple repositories.",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Don't show usage on error, as argument validation passed.
		cmd.SilenceUsage = true

		var contexts []string
		if len(flags.Contexts) == 0 {
			currentContext, err := restconfig.CurrentContextName()
			if err != nil {
				return fmt.Errorf("failed to get current context name with err: %v", errors.Cause(err))
			}
			contexts = append(contexts, currentContext)
		} else if len(flags.Contexts) != 1 || flags.Contexts[0] != "all" {
			contexts = flags.Contexts
		}

		clientMap, err := status.ClusterClients(cmd.Context(), contexts)
		if err != nil {
			return err
		}

		for context, c := range clientMap {
			fmt.Println()
			fmt.Println(util.Separator)
			fmt.Printf("Enabling the multi-repo mode on cluster %q ...\n", context)
			cs := &status.ClusterState{Ref: context}
			if !c.IsInstalled(cmd.Context(), cs) || !c.IsConfigured(cmd.Context(), cs) {
				printError(cs.Error)
				continue
			}
			isMulti, err := c.ConfigManagement.IsMultiRepo(cmd.Context())
			if err != nil {
				printError(err)
				continue
			}
			if isMulti != nil && *isMulti {
				printNotice("The cluster is already running in the multi-repo mode. No migration is needed")
				continue
			}

			rootSync, rsYamlFile, err := saveRootSyncYAML(cmd.Context(), c.ConfigManagement, context)
			if err != nil {
				printError(err)
				continue
			}
			cm, cmYamlFile, err := saveConfigManagementYAML(cmd.Context(), c.ConfigManagement, context)
			if err != nil {
				printError(err)
				continue
			}

			printHint(`Resources for the multi-repo mode have been saved in a temp folder. If the migration process is terminated, it can be recovered manually by running the following commands:
  kubectl apply -f %s && \
  kubectl wait --for condition=established crd rootsyncs.configsync.gke.io && \
  kubectl apply -f %s`, cmYamlFile, rsYamlFile)

			if dryRun {
				dryrun()
			} else if err := migrate(cmd.Context(), c, cm, rootSync); err != nil {
				printError(err)
			}
		}

		fmt.Println("\nFinished migration on all the contexts. Please check the sync status with `nomos status`.")
		return nil
	},
}

func printError(err interface{}) {
	fmt.Printf("%s%sError: %s.%s\n", util.Bullet, util.ColorRed, err, util.ColorDefault)
}

func printNotice(format string, a ...interface{}) {
	fmt.Printf(fmt.Sprintf("%s%sNotice: %s.%s\n", util.Bullet, util.ColorYellow, format, util.ColorDefault), a...)
}

func printInfo(format string, a ...interface{}) {
	fmt.Printf(fmt.Sprintf("%s%s.\n", util.Bullet, format), a...)
}

func printHint(format string, a ...interface{}) {
	fmt.Printf(fmt.Sprintf("%s%s%s.%s\n", util.Bullet, util.ColorCyan, format, util.ColorDefault), a...)
}

func printSuccess(format string, a ...interface{}) {
	fmt.Printf(fmt.Sprintf("%s%s%s.%s\n", util.Bullet, util.ColorGreen, format, util.ColorDefault), a...)
}

func dryrun() {
	printInfo(updatingConfigManagement)
	printInfo(waitingForRootSyncCRD)
	printInfo(creatingRootSync)
	printInfo(waitingForReconcilerManager)
	printInfo(waitingForRootReconciler)
	printSuccess(migrationSuccess)
}

func migrate(ctx context.Context, sc *status.ClusterClient, cm *unstructured.Unstructured, rs *v1beta1.RootSync) error {
	printInfo(updatingConfigManagement)
	if err := sc.ConfigManagement.UpdateConfigManagement(ctx, cm); err != nil {
		return err
	}
	printInfo(waitingForRootSyncCRD)
	if err := waitForRootSyncCRDToBeEstablished(ctx, sc.Client); err != nil {
		return err
	}
	printInfo("The RootSync CRD has been established")

	printInfo(creatingRootSync)
	if err := sc.Client.Create(ctx, rs); err != nil {
		return err
	}

	printInfo(waitingForReconcilerManager)
	if err := waitForPodToBeRunning(ctx, sc.K8sClient, configmanagement.ControllerNamespace, "app=reconciler-manager"); err != nil {
		return err
	}
	printInfo("The reconciler-manager Pod is running")

	printInfo(waitingForRootReconciler)
	if err := waitForPodToBeRunning(ctx, sc.K8sClient, configmanagement.ControllerNamespace, "configsync.gke.io/reconciler=root-reconciler"); err != nil {
		return err
	}
	printInfo("The root-reconciler Pod is running")

	printSuccess(migrationSuccess)
	return nil
}

func recheck(fn func() error) error {
	return retry.OnError(backoff(), func(error) bool { return true }, func() error {
		return fn()
	})
}

func backoff() wait.Backoff {
	return wait.Backoff{
		Duration: time.Second,
		Steps:    int(waitTimeout / time.Second),
	}
}

func waitForPodToBeRunning(ctx context.Context, k8sclient *kubernetes.Clientset, ns string, labelSelector string) error {
	return recheck(func() error {
		pods, err := k8sclient.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			printError(err)
			return err
		}
		if status.HasRunningPod(pods.Items) {
			return nil
		}
		errMsg := fmt.Sprintf("%sHaven't detected running Pods with the label selector %q", util.Indent, labelSelector)
		printInfo(errMsg)
		return fmt.Errorf(errMsg)
	})
}

func waitForRootSyncCRDToBeEstablished(ctx context.Context, c client.Client) error {
	return recheck(func() error {
		rootSyncCRD := &apiextensionsv1.CustomResourceDefinition{}
		if err := c.Get(ctx, client.ObjectKey{Name: "rootsyncs.configsync.gke.io"}, rootSyncCRD); err != nil {
			if apierrors.IsNotFound(err) {
				printInfo("%s%s", util.Indent, err)
			} else {
				printError(err)
			}
			return err
		}
		for _, cond := range rootSyncCRD.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				return nil
			}
		}
		errMsg := "The RootSync CRD has not been established yet."
		printInfo("%s%s", util.Indent, errMsg)
		return errors.New(errMsg)
	})
}

func createRootSync(ctx context.Context, cm *util.ConfigManagementClient) (*v1beta1.RootSync, error) {
	proxyConfig, err := cm.NestedString(ctx, "spec", "git", "httpProxy")
	if err != nil {
		return nil, err
	}
	httpsProxy, err := cm.NestedString(ctx, "spec", "git", "httpsProxy")
	if err != nil {
		return nil, err
	}
	desiredScheme := "http"
	if httpsProxy != "" {
		// Legacy behavior entailed using the HTTPS proxy if both were set.
		proxyConfig = httpsProxy
		desiredScheme = "https"
	}
	if proxyConfig != "" {
		parsedURL, err := url.Parse(proxyConfig)
		if err != nil {
			return nil, errors.Wrapf(err, "malformed proxy config %s", proxyConfig)
		}
		if parsedURL.Hostname() == "" {
			return nil, errors.Errorf("malformed proxy config %s missing hostname", proxyConfig)
		}
		if parsedURL.Scheme != desiredScheme {
			return nil, errors.Errorf("scheme for %s proxy %s needs to be %s", desiredScheme, proxyConfig, desiredScheme)
		}
	}

	sourceFormat, err := cm.NestedString(ctx, "spec", "sourceFormat")
	if err != nil {
		return nil, err
	}

	syncRepo, err := cm.NestedString(ctx, "spec", "git", "syncRepo")
	if err != nil {
		return nil, err
	}
	if syncRepo == "" {
		return nil, errors.Errorf("Git sync repo is empty")
	}

	var secretRefName string
	var gcpServiceAccountEmail string

	secretType, err := cm.NestedString(ctx, "spec", "git", "secretType")
	if err != nil {
		return nil, err
	}

	switch secretType {
	case "ssh", "cookiefile", "token":
		// Update SecretRef name only when secretType is one of "ssh","cookiefile" or "token".
		secretRefName = "git-creds"
	case "gcpserviceaccount":
		// Update GCPServiceAccountEmail when secretType is "gcpserviceaccount".
		gcpServiceAccountEmail, err = cm.NestedString(ctx, "spec", "git", "gcpServiceAccountEmail")
		if err != nil {
			return nil, err
		}
		if gcpServiceAccountEmail == "" {
			return nil, errors.Errorf("gcpServiceAccountEmail not present, but is required when secretType is %s", secretType)
		}
	case "none":
		// no secretRef is used when secretType is "none".
	default:
		return nil, errors.Errorf("%v is an unknown secret type", secretType)
	}

	syncRev, err := cm.NestedString(ctx, "spec", "git", "syncRev")
	if err != nil {
		return nil, err
	}
	if syncRev == "" {
		syncRev = controllers.DefaultSyncRev
	}

	syncBranch, err := cm.NestedString(ctx, "spec", "git", "syncBranch")
	if err != nil {
		return nil, err
	}
	if syncBranch == "" {
		syncBranch = controllers.DefaultSyncBranch
	}

	syncDir, err := cm.NestedString(ctx, "spec", "git", "policyDir")
	if err != nil {
		return nil, err
	}
	if syncDir == "" {
		syncDir = controllers.DefaultSyncDir
	}

	syncWaitSeconds, err := cm.NestedInt(ctx, "spec", "git", "syncWait")
	if err != nil {
		return nil, err
	}
	if syncWaitSeconds == 0 {
		syncWaitSeconds = controllers.DefaultSyncWaitSecs
	}

	return &v1beta1.RootSync{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RootSync",
			APIVersion: v1beta1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "root-sync",
			Namespace: configmanagement.ControllerNamespace,
		},
		Spec: v1beta1.RootSyncSpec{
			SourceFormat: sourceFormat,
			Git: &v1beta1.Git{
				Repo:                   syncRepo,
				Revision:               syncRev,
				Branch:                 syncBranch,
				Dir:                    syncDir,
				Period:                 metav1.Duration{Duration: time.Duration(syncWaitSeconds) * time.Second},
				Auth:                   secretType,
				Proxy:                  proxyConfig,
				GCPServiceAccountEmail: gcpServiceAccountEmail,
				SecretRef: v1beta1.SecretReference{
					Name: secretRefName,
				},
				NoSSLVerify: false,
			},
		},
	}, nil
}

func saveRootSyncYAML(ctx context.Context, cm *util.ConfigManagementClient, context string) (*v1beta1.RootSync, string, error) {
	rs, err := createRootSync(ctx, cm)
	if err != nil {
		return rs, "", err
	}
	content, err := yaml.Marshal(rs)
	if err != nil {
		return rs, "", err
	}

	dir := filepath.Join(os.TempDir(), migrateDir, context)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return rs, "", err
	}

	yamlFile := filepath.Join(dir, rootSyncYamlFile)
	if err := ioutil.WriteFile(yamlFile, content, 0644); err != nil {
		return rs, yamlFile, err
	}
	printInfo("A RootSync object is generated and saved in %q", yamlFile)
	return rs, yamlFile, nil
}

func saveConfigManagementYAML(ctx context.Context, cm *util.ConfigManagementClient, context string) (*unstructured.Unstructured, string, error) {
	dir := filepath.Join(os.TempDir(), migrateDir, context)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return nil, "", err
	}

	cmOrig, cmMulti, err := cm.EnableMultiRepo(ctx)
	if err != nil {
		return cmMulti, "", err
	}
	content, err := yaml.Marshal(cmOrig)
	if err != nil {
		return cmMulti, "", err
	}
	yamlFile := filepath.Join(dir, cmOrigYAMLFile)
	if err := ioutil.WriteFile(yamlFile, content, 0644); err != nil {
		return cmMulti, yamlFile, err
	}
	printInfo("The original ConfigManagement object is saved in %q", yamlFile)

	content, err = yaml.Marshal(cmMulti)
	if err != nil {
		return cmMulti, "", err
	}
	yamlFile = filepath.Join(dir, cmMultiYAMLFile)
	if err := ioutil.WriteFile(yamlFile, content, 0644); err != nil {
		return cmMulti, yamlFile, err
	}
	printInfo("The ConfigManagement object is updated and saved in %q", yamlFile)
	return cmMulti, yamlFile, nil
}
