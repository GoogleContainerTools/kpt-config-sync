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
	"os"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/v1/google"
	"k8s.io/klog/v2/klogr"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/oci"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/util"
	utillog "kpt.dev/configsync/pkg/util/log"
)

var flImage = flag.String("image", util.EnvString(reconcilermanager.OciSyncImage, ""),
	"the OCI image repository for the package")
var flAuth = flag.String("auth", util.EnvString(reconcilermanager.OciSyncAuth, string(configsync.AuthNone)),
	fmt.Sprintf("the authentication type for access to the OCI package. Must be one of %s, %s, or %s. Defaults to %s",
		configsync.AuthGCPServiceAccount, configsync.AuthGCENode, configsync.AuthNone, configsync.AuthNone))
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

func main() {
	utillog.Setup()
	log := utillog.NewLogger(klogr.New(), *flRoot, *flErrorFile)

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

	var auth authn.Authenticator
	switch configsync.AuthType(*flAuth) {
	case configsync.AuthNone:
		auth = authn.Anonymous
	case configsync.AuthGCPServiceAccount, configsync.AuthGCENode:
		a, err := google.NewEnvAuthenticator()
		if err != nil {
			utillog.HandleError(log, true, "ERROR: failed to get the authentication with type %q: %v", *flAuth, err)
		}
		auth = a
	default:
		utillog.HandleError(log, true, "ERROR: unsupported authentication type %q", *flAuth)
	}

	initialSync := true
	failCount := 0
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*time.Duration(*flSyncTimeout))
		if err := oci.FetchPackage(ctx, *flImage, *flRoot, *flDest, auth); err != nil {
			if *flMaxSyncFailures != -1 && failCount >= *flMaxSyncFailures {
				// Exit after too many retries, maybe the error is not recoverable.
				log.Error(err, "too many failures, aborting", "failCount", failCount)
				os.Exit(1)
			}

			failCount++
			log.Error(err, "unexpected error fetching package, will retry")
			log.Info("waiting before retrying", "waitTime", util.WaitTime(*flWait))
			cancel()
			time.Sleep(util.WaitTime(*flWait))
			continue
		}

		if initialSync {
			if *flOneTime {
				log.DeleteErrorFile()
				os.Exit(0)
			}
			initialSync = false
		}

		failCount = 0
		log.DeleteErrorFile()
		log.Info("next sync", "wait_time", util.WaitTime(*flWait))
		cancel()
		time.Sleep(util.WaitTime(*flWait))
	}

}
