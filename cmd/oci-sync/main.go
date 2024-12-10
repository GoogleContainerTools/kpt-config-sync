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

package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2/textlogger"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/auth"
	"kpt.dev/configsync/pkg/oci"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/util"
	utillog "kpt.dev/configsync/pkg/util/log"
)

var flImage = flag.String("image", util.EnvString(reconcilermanager.OciSyncImage, ""),
	"the OCI image repository for the package")
var flAuth = flag.String("auth", util.EnvString(reconcilermanager.OciSyncAuth, string(configsync.AuthNone)),
	fmt.Sprintf("the authentication type for access to the OCI package. Must be one of %s, %s, %s, or %s. Defaults to %s",
		configsync.AuthGCPServiceAccount, configsync.AuthK8sServiceAccount, configsync.AuthGCENode, configsync.AuthNone, configsync.AuthNone))
var flRoot = flag.String("root", util.EnvString("OCI_SYNC_ROOT", util.EnvString("HOME", "")+"/oci"),
	"the root directory for oci-sync operations, under which --dest will be created")
var flDest = flag.String("dest", util.EnvString("OCI_SYNC_DEST", ""),
	"the path (absolute or relative to --root) at which to create a symlink to the directory holding the retrieved files (defaults to the leaf dir of --image)")
var flErrorFile = flag.String("error-file", util.EnvString("OCI_SYNC_ERROR_FILE", ""),
	"the name of a file into which errors will be written under --root (defaults to \"\", disabling error reporting)")
var flWait = flag.Float64("wait", util.EnvFloat(reconcilermanager.OciSyncWait, 1),
	"the number of seconds between syncs")
var flSyncTimeout = flag.Int("timeout", util.EnvInt("OCI_SYNC_TIMEOUT", 120),
	"the max number of seconds allowed for a complete sync")
var flOneTime = flag.Bool("one-time", util.EnvBool("OCI_SYNC_ONE_TIME", false),
	"exit after the first sync")
var flMaxSyncFailures = flag.Int("max-sync-failures", util.EnvInt("OCI_SYNC_MAX_SYNC_FAILURES", 0),
	"the number of consecutive failures allowed before aborting (the first sync must succeed, -1 will retry forever after the initial sync)")

func errorBackoff() wait.Backoff {
	durationLimit := math.Max(*flWait, float64(util.MinimumSyncContainerBackoffCap))
	return util.BackoffWithDurationAndStepLimit(util.WaitTime(durationLimit), math.MaxInt32)
}

func main() {
	utillog.Setup()
	log := utillog.NewLogger(textlogger.NewLogger(textlogger.NewConfig()), *flRoot, *flErrorFile)

	log.Info("pulling OCI image with arguments", "--image", *flImage,
		"--auth", *flAuth, "--root", *flRoot, "--dest", *flDest, "--wait", *flWait,
		"--error-file", *flErrorFile, "--timeout", *flSyncTimeout,
		"--one-time", *flOneTime, "--max-sync-failures", *flMaxSyncFailures)

	if *flImage == "" {
		utillog.HandleError(log, true, "ERROR: --image must be specified")
	}

	if *flRoot == "" {
		utillog.HandleError(log, true, "ERROR: --root must be specified")
	}

	if *flDest == "" {
		parts := strings.Split(strings.Trim(*flImage, "/"), "/")
		*flDest = parts[len(parts)-1]
	}

	if *flWait < 0 {
		utillog.HandleError(log, true, "ERROR: --wait must be greater than or equal to 0")
	}

	if *flSyncTimeout < 0 {
		utillog.HandleError(log, true, "ERROR: --timeout must be greater than 0")
	}

	initialSync := true
	imageFromSpecHasDigest := oci.HasDigest(*flImage)
	failCount := 0
	backoff := errorBackoff()

	var authenticator authn.Authenticator
	switch configsync.AuthType(*flAuth) {
	case configsync.AuthNone:
		authenticator = authn.Anonymous
	case configsync.AuthGCPServiceAccount, configsync.AuthK8sServiceAccount, configsync.AuthGCENode:
		authenticator = &oci.CredentialAuthenticator{
			CredentialProvider: &auth.CachingCredentialProvider{
				Scopes: auth.OCISourceScopes(),
			},
		}
	default:
		utillog.HandleError(log, true, "ERROR: --auth type must be one of %#v, but found %q",
			[]configsync.AuthType{
				configsync.AuthNone,
				configsync.AuthGCPServiceAccount,
				configsync.AuthK8sServiceAccount,
				configsync.AuthGCENode,
			},
			*flAuth)
	}

	fetcher := &oci.Fetcher{
		Authenticator: authenticator,
	}

	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*time.Duration(*flSyncTimeout))
		if err := fetcher.FetchPackage(ctx, *flImage, *flRoot, *flDest); err != nil {
			if *flMaxSyncFailures != -1 && failCount >= *flMaxSyncFailures {
				// Exit after too many retries, maybe the error is not recoverable.
				log.Error(err, "too many failures, aborting", "failCount", failCount)
				os.Exit(1)
			}

			step := backoff.Step()

			failCount++
			log.Error(err, "unexpected error fetching package, will retry")
			log.Info("waiting before retrying", "waitTime", step)
			cancel()
			time.Sleep(step)
			continue
		}

		if initialSync {
			if *flOneTime {
				log.DeleteErrorFile()
				os.Exit(0)
			}
			// If the image declared in spec contains a digest, then this image should
			// never change. We can exit early to avoid redundant sync attempts.
			if imageFromSpecHasDigest {
				log.Info(oci.NoFurtherSyncsLog, "reason", "image was provided with digest")
				log.DeleteErrorFile()
				sleepForever()
				// If oci-sync is integrated as a k8s sidecar container, then this could exit with
				// a zero exit code. However since it's implemented as a regular container
				// exiting here would cause a crash loop.
				// See: https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/
				// TODO: Exit zero if oci-sync is integrated as a sidecar container
				//os.Exit(0)
			}
			initialSync = false
		}

		backoff = errorBackoff()
		failCount = 0
		log.DeleteErrorFile()
		log.Info("next sync", "wait_time", util.WaitTime(*flWait))
		cancel()
		time.Sleep(util.WaitTime(*flWait))
	}

}

func sleepForever() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	os.Exit(0)
}
