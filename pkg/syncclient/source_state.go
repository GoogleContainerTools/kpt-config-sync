package syncclient

import "kpt.dev/configsync/pkg/importer/filesystem/cmpath"

// SourceState contains all state read from the mounted source repo.
type SourceState struct {
	// Spec is the source specification as read from the FileSource.
	// This cache avoids re-generating the Spec every time the status is updated.
	Spec SourceSpec
	// Commit is the Commit read from the source of truth.
	Commit string
	// SyncPath is the absolute path to the sync directory that includes the configurations.
	SyncPath cmpath.Absolute
	// Files is the list of all observed Files in the sync directory (recursively).
	Files []cmpath.Absolute
}
