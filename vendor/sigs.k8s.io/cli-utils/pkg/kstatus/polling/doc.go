// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

// Package polling is a library for computing the status of kubernetes resources
// based on polling of resource state from a cluster. It can keep polling until
// either some condition is met, or until it is cancelled through the provided
// context. Updates on the status of resources are streamed back to the caller
// through a channel.
//
// This package provides a simple interface based on the built-in
// features. But the actual code is in the subpackages and there
// are several interfaces that can be implemented to support custom
// behavior.
//
// # Polling Resources
//
// In order to poll a set of resources, create a StatusPoller
// and pass in the list of ResourceIdentifiers to the Poll function.
//
//	import (
//	  "sigs.k8s.io/cli-utils/pkg/kstatus/polling"
//	)
//
//	identifiers := []prune.ObjMetadata{
//	  {
//	    GroupKind: schema.GroupKind{
//	      Group: "apps",
//	      Kind: "Deployment",
//	    },
//	    Name: "dep",
//	    Namespace: "default",
//	  }
//	}
//
//	poller := polling.NewStatusPoller(reader, mapper, true)
//	eventsChan := poller.Poll(context.Background(), identifiers, polling.PollOptions{})
//	for e := range eventsChan {
//	   // Handle event
//	}
package polling
