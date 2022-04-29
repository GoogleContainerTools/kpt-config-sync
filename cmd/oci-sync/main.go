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
	"strconv"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/v1/google"
	"k8s.io/klog/klogr"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/oci"
	utillog "kpt.dev/configsync/pkg/util/log"
)

var flImage = flag.String("image", envString("OCI_SYNC_IMAGE", ""),
	"the OCI image repository for the package")
var flAuth = flag.String("auth", envString("OCI_SYNC_AUTH", configsync.GitSecretNone),
	fmt.Sprintf("the authentication type for access to the OCI package. Must be one of %s, %s, or %s. Defaults to %s",
		configsync.GitSecretGCPServiceAccount, configsync.GitSecretGCENode, configsync.GitSecretNone, configsync.GitSecretNone))
var flRoot = flag.String("root", envString("OCI_SYNC_ROOT", envString("HOME", "")+"/oci"),
	"the root directory for oci-sync operations, under which --dest will be created")
var flDest = flag.String("dest", envString("OCI_SYNC_DEST", ""),
	"the path (absolute or relative to --root) at which to create a symlink to the directory holding the retrieved files (defaults to the leaf dir of --image)")
var flErrorFile = flag.String("error-file", envString("OCI_SYNC_ERROR_FILE", ""),
	"the name of a file into which errors will be written under --root (defaults to \"\", disabling error reporting)")
var flWait = flag.Float64("wait", envFloat("OCI_SYNC_WAIT", 1),
	"the number of seconds between syncs")
var flSyncTimeout = flag.Int("timeout", envInt("OCI_SYNC_TIMEOUT", 120),
	"the max number of seconds allowed for a complete sync")
var flOneTime = flag.Bool("one-time", envBool("OCI_SYNC_ONE_TIME", false),
	"exit after the first sync")
var flMaxSyncFailures = flag.Int("max-sync-failures", envInt("OCI_SYNC_MAX_SYNC_FAILURES", 0),
	"the number of consecutive failures allowed before aborting (the first sync must succeed, -1 will retry forever after the initial sync)")

func envString(key, def string) string {
	if env := os.Getenv(key); env != "" {
		return env
	}
	return def
}

func envBool(key string, def bool) bool {
	if env := os.Getenv(key); env != "" {
		res, err := strconv.ParseBool(env)
		if err != nil {
			return def
		}

		return res
	}
	return def
}

func envInt(key string, def int) int {
	if env := os.Getenv(key); env != "" {
		val, err := strconv.ParseInt(env, 0, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: invalid env value (%v): using default, key=%s, val=%q, default=%d\n", err, key, env, def)
			return def
		}
		return int(val)
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if env := os.Getenv(key); env != "" {
		val, err := strconv.ParseFloat(env, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: invalid env value (%v): using default, key=%s, val=%q, default=%f\n", err, key, env, def)
			return def
		}
		return val
	}
	return def
}

func main() {
	utillog.Setup()
	log := oci.NewLogger(klogr.New(), *flRoot, *flErrorFile)

	log.Info("pulling OCI image with arguments", "--image", *flImage,
		"--auth", *flAuth, "--root", *flRoot, "--dest", *flDest, "--wait", *flWait,
		"--error-file", *flErrorFile, "--timeout", *flSyncTimeout,
		"--one-time", *flOneTime, "--max-sync-failures", *flMaxSyncFailures)

	if *flImage == "" {
		handleError(log, true, "ERROR: --image must be specified")
	}

	if *flRoot == "" {
		handleError(log, true, "ERROR: --root must be specified")
	}

	if *flDest == "" {
		parts := strings.Split(strings.Trim(*flImage, "/"), "/")
		*flDest = parts[len(parts)-1]
	}

	if *flWait < 0 {
		handleError(log, true, "ERROR: --wait must be greater than or equal to 0")
	}

	if *flSyncTimeout < 0 {
		handleError(log, true, "ERROR: --timeout must be greater than 0")
	}

	var auth authn.Authenticator
	switch *flAuth {
	case configsync.GitSecretNone:
		auth = authn.Anonymous
	case configsync.GitSecretGCPServiceAccount, configsync.GitSecretGCENode:
		a, err := google.NewEnvAuthenticator()
		if err != nil {
			handleError(log, true, "ERROR: failed to get the authentication with type %q: %v", *flAuth, err)
		}
		auth = a
	default:
		handleError(log, true, "ERROR: unsupported authentication type %q", *flAuth)
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
			log.Info("waiting before retrying", "waitTime", waitTime(*flWait))
			cancel()
			time.Sleep(waitTime(*flWait))
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
		log.Info("next sync", "wait_time", waitTime(*flWait))
		cancel()
		time.Sleep(waitTime(*flWait))
	}

}

func waitTime(seconds float64) time.Duration {
	return time.Duration(int(seconds*1000)) * time.Millisecond
}

// handleError prints the error to the standard error, prints the usage if the `printUsage` flag is true,
// exports the error to the error file and exits the process with the exit code.
func handleError(log *oci.Logger, printUsage bool, format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	fmt.Fprintln(os.Stderr, s)
	if printUsage {
		flag.Usage()
	}
	log.ExportError(s)
	os.Exit(1)
}
