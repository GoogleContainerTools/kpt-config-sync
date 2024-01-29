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

package testutils

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/pkg/api/configmanagement"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestCrossProjectFleetProjectID is the fleet host project.
const TestCrossProjectFleetProjectID = "cs-dev-hub"

// RegisterCluster registers a cluster in a fleet.
func RegisterCluster(nt *nomostest.NT, fleetMembership, fleetProject, gkeURI string) error {
	_, err := nt.Shell.ExecWithDebug("gcloud", "container", "fleet",
		"memberships", "register", fleetMembership, "--project", fleetProject,
		"--gke-uri", gkeURI, "--enable-workload-identity")
	return err
}

// unregisterCluster unregisters a cluster from a fleet.
func unregisterCluster(nt *nomostest.NT, fleetMembership, fleetProject, gkeURI string) error {
	_, err := nt.Shell.ExecWithDebug("gcloud", "container", "fleet",
		"memberships", "unregister", fleetMembership, "--quiet",
		"--project", fleetProject, "--gke-uri", gkeURI)
	return err
}

// describeMembership describes the membership resource in the GCP project.
func describeMembership(nt *nomostest.NT, fleetMembership, fleetProject string) ([]byte, error) {
	return nt.Shell.ExecWithDebug("gcloud", "container", "fleet",
		"memberships", "describe", fleetMembership,
		"--project", fleetProject)
}

// deleteMembership deletes the membership from the cluster.
func deleteMembership(nt *nomostest.NT, fleetMembership, fleetProject string) error {
	_, err := nt.Shell.ExecWithDebug("gcloud", "container", "fleet",
		"memberships", "delete", fleetMembership, "--quiet",
		"--project", fleetProject)
	return err
}

// FleetHasMembership checks whether the membership exists in the fleet project.
func FleetHasMembership(nt *nomostest.NT, fleetMembership, fleetProject string) (bool, error) {
	bytes, err := describeMembership(nt, fleetMembership, fleetProject)
	out := string(bytes)
	if err != nil {
		// There are different error messages returned from the fleet:
		// - ERROR: (gcloud.container.fleet.memberships.describe) Membership xxx not found in the fleet.
		// - ERROR: (gcloud.container.fleet.memberships.describe) No memberships available in the fleet.
		// - ERROR: (gcloud.container.fleet.memberships.describe) NOT_FOUND: Resource 'projects/gcpProject/locations/global/memberships/fleetMembership' was not found
		if strings.Contains(out, fmt.Sprintf("Membership %s not found in the fleet", fleetMembership)) ||
			strings.Contains(out, "No memberships available in the fleet") ||
			strings.Contains(out, "NOT_FOUND") {
			return false, nil
		}
		return false, fmt.Errorf("failed to describe the membership %s in project %s (out: %s\nerror: %w)", fleetMembership, fleetProject, out, err)
	}

	return true, nil
}

// ClearMembershipInfo deletes the membership by unregistering the cluster.
// It also deletes the reconciler-manager to ensure the membership watch is not set up.
func ClearMembershipInfo(nt *nomostest.NT, fleetMembership, fleetProject, gkeURI string) {
	exists, err := FleetHasMembership(nt, fleetMembership, fleetProject)
	if err != nil {
		nt.T.Fatalf("Unable to check the membership: %v", err)
	}
	if !exists {
		return
	}

	nt.T.Logf("The membership exits, unregistering the cluster from project %q to clean up the membership", fleetProject)
	if err = unregisterCluster(nt, fleetMembership, fleetProject, gkeURI); err != nil {
		nt.T.Logf("Failed to unregister the cluster: %v", err)
		if err = deleteMembership(nt, fleetMembership, fleetProject); err != nil {
			nt.T.Logf("Failed to delete the membership %q: %v", fleetMembership, err)
		}
		exists, err = FleetHasMembership(nt, fleetMembership, fleetProject)
		if err != nil {
			nt.T.Fatalf("Unable to check if membership is deleted: %v", err)
		}
		if exists {
			nt.T.Fatalf("The membership wasn't deleted")
		}
	}
	// b/226383057: DeletePodByLabel deletes the current reconciler-manager Pod so that new Pod
	// is guaranteed to have no membership watch setup.
	// This is to ensure consistent behavior when the membership is not cleaned up from previous runs.
	// The underlying reconciler may or may not be restarted depending on the membership existence.
	// If membership exists before the reconciler-manager is deployed (test leftovers), the reconciler will be updated.
	// If membership doesn't exist (normal scenario), the reconciler should remain the same after the reconciler-manager restarts.
	nt.T.Logf("Restart the reconciler-manager to ensure the membership watch is not set up")
	nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName, false)
	nomostest.Wait(nt.T, "wait for FWI credentials to be absent", nt.DefaultWaitTimeout, func() error {
		return ReconcilerPodMissingFWICredsAnnotation(nt, nomostest.DefaultRootReconcilerName)
	})
}

// ReconcilerPodHasFWICredsAnnotation checks whether the reconciler Pod has the
// FWI credentials annotation.
func ReconcilerPodHasFWICredsAnnotation(nt *nomostest.NT, reconcilerName, gsaEmail string) error {
	var podList = &corev1.PodList{}
	if err := nt.KubeClient.List(podList, client.InNamespace(configmanagement.ControllerNamespace), client.MatchingLabels{metadata.ReconcilerLabel: reconcilerName}); err != nil {
		return err
	}
	if len(podList.Items) != 1 {
		return fmt.Errorf("expected only 1 Pod for the reconciler %s, but got %d", reconcilerName, len(podList.Items))
	}

	pod := podList.Items[0]
	creds, found := pod.GetAnnotations()[metadata.FleetWorkloadIdentityCredentials]
	if !found {
		return fmt.Errorf("object %s/%s does not have annotation %q", pod.GetNamespace(), pod.GetName(), metadata.FleetWorkloadIdentityCredentials)
	}
	membership := &hubv1.Membership{}
	if err := nt.KubeClient.Get("membership", "", membership); err != nil {
		return err
	}
	expectedCreds, err := controllers.BuildFWICredsContent(membership.Spec.WorkloadIdentityPool, membership.Spec.IdentityProvider, gsaEmail)
	if err != nil {
		return err
	}
	if creds != expectedCreds {
		return fmt.Errorf("expected FWI creds to equal %s, but got %s", expectedCreds, creds)
	}
	return nil
}

// ReconcilerPodMissingFWICredsAnnotation checks whether the reconciler Pod does
// not contain annotation with the injected FWI credentials.
func ReconcilerPodMissingFWICredsAnnotation(nt *nomostest.NT, reconcilerName string) error {
	var podList = &corev1.PodList{}
	if err := nt.KubeClient.List(podList, client.InNamespace(configmanagement.ControllerNamespace), client.MatchingLabels{metadata.ReconcilerLabel: reconcilerName}); err != nil {
		return err
	}
	if len(podList.Items) != 1 {
		return fmt.Errorf("expected only 1 Pod for the reconciler %s, but got %d", reconcilerName, len(podList.Items))
	}
	pod := podList.Items[0]
	if _, found := pod.GetAnnotations()[metadata.FleetWorkloadIdentityCredentials]; found {
		return fmt.Errorf("object %s/%s has annotation %q", pod.GetNamespace(), pod.GetName(), metadata.FleetWorkloadIdentityCredentials)
	}
	return nil
}
