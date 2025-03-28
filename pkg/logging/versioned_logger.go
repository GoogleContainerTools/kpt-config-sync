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
	Version             = "v1"
	ConfigSyncLogPrefix = "Config Sync versioned log:"

	ReconcileStageFetch  = "fetch"
	ReconcileStageRender = "render"
	ReconcileStageRead   = "read"
	ReconcileStageParse  = "parse"
	ReconcileStageUpdate = "update"

	SyncSucceeded = "Sync Succeeded"
	SyncFailed    = "Sync Failed"
)

// SyncMetadata contains metadata about the RootSync/RepoSync
type SyncMetadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind"`
}

// NewSyncMetadata creates a ResourceInfo from common resource attributes
func NewSyncMetadata(name, namespace, kind string) SyncMetadata {
	return SyncMetadata{
		Name:      name,
		Namespace: namespace,
		Kind:      kind,
	}
}

// String returns a string representation of SyncResourceInfo
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

// String returns a string representation of the VersionedLog
func (v VersionedLog) String() string {
	var fields []string

	fields = append(fields, ConfigSyncLogPrefix)
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

func NewLogger(component string) *Logger {
	return &Logger{
		component: component,
	}
}

// LogEvent logs a versioned event if it's different from the last log
func (l *Logger) LogEvent(status string, resource SyncMetadata, message, commit string, err status.MultiError) VersionedLog {
	l.logMux.Lock()
	defer l.logMux.Unlock()

	entry := NewVersionedLogEntry(status, resource, message, commit, err)

	if entry == l.lastLog {
		return entry
	}
	l.lastLog = entry

	klog.InfoS(entry.String())
	return entry
}

func NewVersionedLogEntry(status string, resource SyncMetadata, message, commit string, err status.MultiError) VersionedLog {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}

	return VersionedLog{
		Version:          Version,
		Status:           status,
		SyncResourceInfo: resource,
		Message:          message,
		Commit:           commit,
		Error:            errStr,
	}
}

// GenerateReconcileResultMessage generates a message for the reconcile result based on success/failure and stage.
func GenerateReconcileResultMessage(success bool, stage string) string {
	status := SyncSucceeded
	if !success {
		status = SyncFailed
	}
	return fmt.Sprintf("%s at stage %s", status, stage)
}
