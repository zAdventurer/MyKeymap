// Package sync contains the file and Git primitives used to synchronize
// data/config.json. It deliberately has no HTTP dependencies so handlers can
// compose these operations safely.
package sync

import "time"

const (
	DefaultRepositoryURL = "git@github.com:zAdventurer/MyKeymap.git"
	DefaultBranch        = "main"
)

// Config identifies the files and repository used by a synchronization
// operation. Paths are supplied by the caller so this package does not depend
// on the process working directory.
type Config struct {
	RepositoryURL string `json:"repositoryUrl"`
	Branch        string `json:"branch"`
	ConfigPath    string `json:"configPath"`
	MirrorPath    string `json:"mirrorPath"`
	StatePath     string `json:"statePath"`
	BackupPath    string `json:"backupPath"`
}

// State records the hashes observed after the last successful synchronization.
type State struct {
	LocalHash  string    `json:"localHash"`
	RemoteHash string    `json:"remoteHash"`
	SyncedAt   time.Time `json:"syncedAt"`
}

// Status describes how the current local and remote files relate to State.
type Status string

const (
	StatusSynchronized Status = "synchronized"
	StatusLocalOnly    Status = "local-only"
	StatusRemoteOnly   Status = "remote-only"
	StatusConflict     Status = "conflict"
)

// ConflictResolution names the explicit user choice that resolves divergent
// local and remote configuration files.
type ConflictResolution string

const (
	ResolutionKeepLocal ConflictResolution = "keep-local"
	ResolutionUseRemote ConflictResolution = "use-remote"
)

// Repository owns the local Git working copy used to synchronize one
// data/config.json file. It is intentionally independent of HTTP so callers
// can choose their own transport and UI.
type Repository struct {
	Config Config
	Git    GitExecutor
	Now    func() time.Time
}
