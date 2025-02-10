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

package nomostest

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	rbacv1beta1 "k8s.io/api/rbac/v1beta1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/clusters"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	configmanagementv1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	configsyncv1alpha1 "kpt.dev/configsync/pkg/api/configsync/v1alpha1"
	configsyncv1beta1 "kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	resourcegroupv1alpha1 "kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/client/restconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newTestClient(nt *NT) *testkubeclient.KubeClient {
	nt.T.Helper()
	nt.Logger.Info("Creating Client")
	kubeClient, err := client.NewWithWatch(nt.Config, client.Options{
		// The Scheme is client-side, but this automatically fetches the RestMapper
		// from the cluster.
		Scheme: nt.Scheme,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	return &testkubeclient.KubeClient{
		Context: nt.Context,
		Client:  kubeClient,
		Logger:  nt.Logger,
	}
}

// newScheme creates a new Scheme to use to map Go types to types on a
// Kubernetes cluster.
func newScheme(t testing.NTB) *runtime.Scheme {
	t.Helper()

	// TODO: use core.Scheme?
	s := runtime.NewScheme()

	// It is always safe to add new schemes so long as there are no GVK or struct
	// collisions.
	//
	// We have no tests which require configuring this in incompatible ways, so if
	// you need new types then add them here.
	builders := []runtime.SchemeBuilder{
		admissionv1.SchemeBuilder,
		apiextensionsv1beta1.SchemeBuilder,
		apiextensionsv1.SchemeBuilder,
		appsv1.SchemeBuilder,
		corev1.SchemeBuilder,
		batchv1.SchemeBuilder,
		configmanagementv1.SchemeBuilder,
		configsyncv1alpha1.SchemeBuilder,
		configsyncv1beta1.SchemeBuilder,
		policyv1beta1.SchemeBuilder,
		networkingv1.SchemeBuilder,
		rbacv1.SchemeBuilder,
		rbacv1beta1.SchemeBuilder,
		resourcegroupv1alpha1.SchemeBuilder.SchemeBuilder,
		apiregistrationv1.SchemeBuilder,
		hubv1.SchemeBuilder,
	}
	for _, b := range builders {
		err := b.AddToScheme(s)
		if err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// Cluster is an interface for a target k8s cluster used by the e2e tests
type Cluster interface {
	// Exists whether the cluster exists
	Exists() (bool, error)
	// Create the k8s cluster
	Create() error
	// Delete the k8s cluster
	Delete() error
	// WaitForReady waits until the k8s cluster is ready to use
	WaitForReady() error
	// Connect to the k8s cluster (generate kubeconfig)
	Connect() error
	// Hash returns the cluster hash.
	Hash() (string, error)
}

// upsertCluster creates the cluster if it does not exist
// returns whether the cluster was created
func upsertCluster(t testing.NTB, cluster Cluster, opts *ntopts.New) (bool, error) {
	exists, err := cluster.Exists()
	if err != nil {
		return false, err
	}
	if exists && *e2e.CreateClusters != e2e.CreateClustersLazy {
		return false, fmt.Errorf("cluster %s already exists and create-clusters=%s", opts.ClusterName, *e2e.CreateClusters)
	}
	if exists && *e2e.CreateClusters == e2e.CreateClustersLazy {
		t.Logf("cluster %s already exists, adopting (create-clusters=%s)", opts.ClusterName, *e2e.CreateClusters)
		if err := cluster.WaitForReady(); err != nil {
			return false, err
		}
		return false, nil
	}
	createStart := time.Now()
	t.Logf("Creating %s cluster %s at %s",
		*e2e.TestCluster, opts.ClusterName, createStart.Format(time.RFC3339))
	if err := cluster.Create(); err != nil {
		return true, err
	}
	if err := cluster.WaitForReady(); err != nil {
		return true, err
	}
	t.Logf("took %s to create %s cluster %s",
		time.Since(createStart), *e2e.TestCluster, opts.ClusterName)
	return true, nil
}

// RestConfig sets up the config for creating a Client connection to a K8s cluster.
// If --test-cluster=kind, it creates a Kind cluster.
// If --test-cluster=kubeconfig, it uses the context specified in kubeconfig.
func RestConfig(t testing.NTB, opts *ntopts.New) {
	opts.KubeconfigPath = filepath.Join(opts.TmpDir, clusters.Kubeconfig)
	t.Logf("kubeconfig will be created at %s", opts.KubeconfigPath)
	t.Logf("Connect to %s cluster using:\nexport KUBECONFIG=%s", *e2e.TestCluster, opts.KubeconfigPath)
	opts.ClusterName = opts.Name
	var cluster Cluster
	switch strings.ToLower(*e2e.TestCluster) {
	case e2e.Kind:
		cluster = &clusters.KindCluster{
			T:                 t,
			Name:              opts.ClusterName,
			KubeConfigPath:    opts.KubeconfigPath,
			TmpDir:            opts.TmpDir,
			KubernetesVersion: *e2e.KubernetesVersion,
		}
	case e2e.GKE:
		cluster = &clusters.GKECluster{
			T:              t,
			Name:           opts.ClusterName,
			KubeConfigPath: opts.KubeconfigPath,
		}
	default:
		t.Fatalf("unsupported test cluster config %s. Allowed values are %s and %s.", *e2e.TestCluster, e2e.GKE, e2e.Kind)
	}
	switch *e2e.DestroyClusters {
	case e2e.DestroyClustersEnabled:
		opts.IsEphemeralCluster = true
	case e2e.DestroyClustersDisabled:
		opts.IsEphemeralCluster = false
	case e2e.DestroyClustersAuto:
		// set initial value for auto, but this may change below if cluster is created
		opts.IsEphemeralCluster = false
	default:
		// this should never be reached due to earlier parameter validation
		t.Fatalf("unrecognized option for destroy-clusters: %v", *e2e.DestroyClusters)
	}
	t.Cleanup(func() {
		if !opts.IsEphemeralCluster {
			t.Logf("[WARNING] Skipping deletion of %s cluster %s (--destroy-clusters=%s)",
				*e2e.TestCluster, opts.ClusterName, *e2e.DestroyClusters)
			return
		} else if t.Failed() && *e2e.Debug {
			t.Logf("[WARNING] Skipping deletion of %s cluster %s (tests failed with --debug)",
				*e2e.TestCluster, opts.ClusterName)
			return
		}
		deleteStart := time.Now()
		t.Logf("Deleting %s cluster %s at %s",
			*e2e.TestCluster, opts.ClusterName, deleteStart.Format(time.RFC3339))
		defer func() {
			t.Logf("took %s to delete %s cluster %s",
				time.Since(deleteStart), *e2e.TestCluster, opts.ClusterName)
		}()
		if err := cluster.Delete(); err != nil {
			t.Error(err)
		}
	})
	if *e2e.CreateClusters != e2e.CreateClustersDisabled {
		createdCluster, err := upsertCluster(t, cluster, opts)
		if *e2e.DestroyClusters == e2e.DestroyClustersAuto {
			// if the cluster was created with --destroy-clusters=auto, the cluster
			// will be cleaned up at the end of the test execution
			opts.IsEphemeralCluster = createdCluster
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("Connecting to %s cluster %s",
		*e2e.TestCluster, opts.ClusterName)
	if err := cluster.Connect(); err != nil {
		t.Fatal(err)
	}

	restConfig, err := restconfig.NewFromConfigFile(opts.KubeconfigPath)
	if err != nil {
		t.Fatalf("building rest.Config: %v", err)
	}
	opts.RESTConfig = restConfig

	clusterHash, err := cluster.Hash()
	if err != nil {
		t.Fatalf("getting the GKE cluster hash: %v", err)
	}
	opts.ClusterHash = clusterHash
}
