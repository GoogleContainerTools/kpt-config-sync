// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

import (
	"flag"
	"fmt"
	"net/http"

	"cloud.google.com/go/compute/metadata"
	"k8s.io/klog/v2/klogr"
	"kpt.dev/configsync/pkg/askpass"
	"kpt.dev/configsync/pkg/util"
	utillog "kpt.dev/configsync/pkg/util/log"
)

// all the flags and their usage.
var flHelp = flag.Bool("help",
	false,
	"print help and usage information")

var flPort = flag.Int("port",
	util.EnvInt("ASKPASS_PORT", 9102),
	"port to listen on")

var flGsaEmail = flag.String("email",
	util.EnvString("GSA_EMAIL", ""),
	"Google Service Account for authentication")

var flErrorFile = flag.String("error-file",
	util.EnvString("ASKPASS_ERROR_FILE", ""),
	"the name of a file into which errors will be written defaults to \"\", disabling error reporting")

var flRoot = flag.String("root",
	util.EnvString("ASKPASS_ROOT", util.EnvString("HOME", "")+"/askpass"),
	"the root directory for askpass")

// main function is designed only to deal with the environment. i.e. parse
// user input and make sure we can set up a server.  All the logic outside
// of OS and network interractions should be in the package with the logic
// for askpass
func main() {
	// if people are looking for help we are not going to launch anything
	// assuming that they didn't mean to start the askpass process.
	if *flHelp {
		flag.Usage()
		return
	}

	utillog.Setup()
	log := utillog.NewLogger(klogr.New(), *flRoot, *flErrorFile)

	log.Info("starting askpass with arguments", "--port", *flPort,
		"--email", *flGsaEmail, "--error-file", *flErrorFile, "--root", *flRoot)

	if *flPort == 0 {
		utillog.HandleError(log, true,
			"ERROR: port can not be zero")
	}

	if *flRoot == "" {
		utillog.HandleError(log, true, "root cannot be empty")
	}

	var gsaEmail string
	var err error
	// for getting the GSA email we have several scenarios
	// the first one is that the user provides it to us.
	// the second scenario is that it's not provided but we can get the
	// Compute Engine default service account from the metadata server.
	if *flGsaEmail != "" {
		gsaEmail = *flGsaEmail
	} else if metadata.OnGCE() {
		gsaEmail, err = metadata.Email("")
		if err != nil {
			utillog.HandleError(log, false, "error in http.ListenAndServe: %v", err)
		}
	} else {
		utillog.HandleError(log, true,
			"ERROR: GSA email can not be empty")
	}

	aps := &askpass.Server{
		Email: gsaEmail,
	}
	http.HandleFunc("/git_askpass", aps.GitAskPassHandler)

	if err := http.ListenAndServe(fmt.Sprintf(":%d", *flPort), nil); err != nil {
		utillog.HandleError(log, false, "error in http.ListenAndServe: %v", err)
	}
}
