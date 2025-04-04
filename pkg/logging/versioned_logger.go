// Copyright 2025 Google LLC
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

package logging

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/status"
)

const (
	version             = "v1"
	configSyncLogPrefix = "Config Sync versioned log:"

	// ReconcileStageFetch represents sync stage for the fetch
	ReconcileStageFetch = "fetch"
	// ReconcileStageRender represents sync stage for the render
	ReconcileStageRender = "render"
	// ReconcileStageRead represents sync stage for the read
	ReconcileStageRead = "read"
	// ReconcileStageParse represents sync stage for the parse
	ReconcileStageParse = "parse"
	// ReconcileStageUpdate represents sync stage for the update
	ReconcileStageUpdate = "update"

	// SyncSucceeded represents sync result succeeded
	SyncSucceeded = "Sync Succeeded"
	// SyncFailed represents sync result failed
	SyncFailed = "Sync Failed"
)

// SyncMetadata contains metadata about the RootSync/RepoSync
type SyncMetadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind"`
}

// NewSyncMetadata creates a SyncMetadata instance from common resource attributes.
func NewSyncMetadata(name, namespace, kind string) SyncMetadata {
	return SyncMetadata{
		Name:      name,
		Namespace: namespace,
		Kind:      kind,
	}
}

// String returns a string representation of SyncResourceInfo.
func (s SyncMetadata) String() string {
	if s.Namespace != "" {
		return fmt.Sprintf("%s/%s/%s", s.Kind, s.Namespace, s.Name)
	}
	return fmt.Sprintf("%s/%s", s.Kind, s.Name)
}

// VersionedLog represents a structured log entry
type VersionedLog struct {
	Version          string       `json:"version"`
	Status           string       `json:"status"`
	SyncResourceInfo SyncMetadata `json:"syncResourceInfo,omitempty"`
	Message          string       `json:"message,omitempty"`
	Commit           string       `json:"commit,omitempty"`
	Error            string       `json:"error,omitempty"`
}

// String returns a string representation of the VersionedLog.
func (v VersionedLog) String() string {
	var fields []string

	fields = append(fields, configSyncLogPrefix)
	fields = append(fields, fmt.Sprintf("version=%s", v.Version))
	fields = append(fields, fmt.Sprintf("status=%s", v.Status))

	if v.Commit != "" {
		fields = append(fields, fmt.Sprintf("commit=%s", v.Commit))
	}
	if !reflect.ValueOf(v.SyncResourceInfo).IsZero() {
		fields = append(fields, fmt.Sprintf("syncResourceInfo=%s", v.SyncResourceInfo.String()))
	}
	if v.Message != "" {
		fields = append(fields, fmt.Sprintf("message=%s", v.Message))
	}
	if v.Error != "" {
		fields = append(fields, fmt.Sprintf("error=%s", v.Error))
	}

	return strings.Join(fields, " ")
}

// Logger provides versioned logging capabilities
type Logger struct {
	component string
	lastLog   VersionedLog
	logMux    sync.RWMutex
}

// NewLogger initializes a new Logger instance with the specified component name.
func NewLogger(component string) *Logger {
	return &Logger{
		component: component,
	}
}

// LogEvent logs a versioned event if it's different from the last log entry.
// It returns the logged VersionedLog entry.
func (l *Logger) LogEvent(status string, resource SyncMetadata, message, commit string, err status.MultiError) VersionedLog {
	l.logMux.Lock()
	defer l.logMux.Unlock()

	entry := NewVersionedLogEntry(status, resource, message, commit, err)

	if entry == l.lastLog {
		return entry
	}
	l.lastLog = entry

	if err != nil {
		klog.Error(entry.String())
	} else {
		klog.Info(entry.String())
	}
	return entry
}

// NewVersionedLogEntry creates a new VersionedLog entry with the provided parameters.
func NewVersionedLogEntry(status string, resource SyncMetadata, message, commit string, err status.MultiError) VersionedLog {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}

	return VersionedLog{
		Version:          version,
		Status:           status,
		SyncResourceInfo: resource,
		Message:          message,
		Commit:           commit,
		Error:            errStr,
	}
}

// GenerateSyncResultMessage generates a message for the reconcile result based on success/failure and stage.
func GenerateSyncResultMessage(success bool, stage string) string {
	status := SyncSucceeded
	if !success {
		status = SyncFailed
	}
	return fmt.Sprintf("%s at stage %s", status, stage)
}
