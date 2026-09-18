package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	repositoryConfigPath = "data/config.json"
	mirrorMarkerName     = ".mykeymap-sync-mirror"
	mirrorMarkerContent  = "mykeymap-sync-mirror-v1\n"
)

var (
	// ErrConflict requires an explicit keep-local or use-remote choice before
	// either configuration is overwritten.
	ErrConflict = errors.New("local and remote configurations conflict")
	// ErrLocalChanged prevents a pull from discarding an unsynchronized local
	// configuration.
	ErrLocalChanged = errors.New("local configuration changed; resolve the conflict explicitly")
	// ErrRemoteChanged prevents a push from replacing an unsynchronized remote
	// configuration.
	ErrRemoteChanged = errors.New("remote configuration changed; resolve the conflict explicitly")
)

// DetectState compares current content hashes with the hashes from the last
// successful synchronization. Without prior metadata, differing content is a
// conflict because neither copy is safe to overwrite automatically.
func DetectState(localHash, remoteHash string, state State) Status {
	if localHash == state.LocalHash && remoteHash == state.RemoteHash {
		return StatusSynchronized
	}
	if localHash == remoteHash {
		return StatusSynchronized
	}
	if state.LocalHash == "" || state.RemoteHash == "" {
		return StatusConflict
	}

	localChanged := localHash != state.LocalHash
	remoteChanged := remoteHash != state.RemoteHash
	switch {
	case localChanged && remoteChanged:
		return StatusConflict
	case localChanged:
		return StatusLocalOnly
	case remoteChanged:
		return StatusRemoteOnly
	default:
		// State says the files have not changed but their hashes differ. Treat
		// the inconsistent metadata conservatively.
		return StatusConflict
	}
}

// HashFile returns the lowercase hexadecimal SHA-256 hash of a file.
func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for hashing: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash file: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ReadState loads synchronization metadata from path.
func ReadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, fmt.Errorf("read sync state: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode sync state: %w", err)
	}
	return state, nil
}

// WriteStateAtomically persists synchronization metadata without exposing a
// partially written state file to a concurrent reader.
func WriteStateAtomically(path string, state State) (err error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create sync state directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sync state: %w", err)
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("create temporary sync state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("set sync state permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary sync state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("flush temporary sync state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary sync state: %w", err)
	}
	if err := replaceFileAtomically(temporaryPath, path); err != nil {
		return fmt.Errorf("replace sync state atomically: %w", err)
	}
	return nil
}

// BackupFile makes an exclusive, timestamped copy of sourcePath. label
// differentiates backups such as "local" and "remote" during conflict
// resolution.
func BackupFile(sourcePath, backupDirectory, label string, at time.Time) (string, error) {
	if label == "" || label == "." || label == ".." || strings.ContainsAny(label, `\\/`) {
		return "", errors.New("backup label must be a non-empty filename component")
	}
	if err := os.MkdirAll(backupDirectory, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}

	base := filepath.Base(sourcePath)
	extension := filepath.Ext(base)
	stem := strings.TrimSuffix(base, extension)
	timestamp := at.UTC().Format("20060102T150405Z")
	source, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("open source for backup: %w", err)
	}
	defer source.Close()

	for sequence := 0; ; sequence++ {
		name := stem + "." + label + "." + timestamp
		if sequence > 0 {
			name += fmt.Sprintf(".%d", sequence)
		}
		backupPath := filepath.Join(backupDirectory, name+extension)
		backup, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create backup: %w", err)
		}

		if _, err := io.Copy(backup, source); err != nil {
			backup.Close()
			_ = os.Remove(backupPath)
			return "", fmt.Errorf("copy backup: %w", err)
		}
		if err := backup.Sync(); err != nil {
			backup.Close()
			_ = os.Remove(backupPath)
			return "", fmt.Errorf("flush backup: %w", err)
		}
		if err := backup.Close(); err != nil {
			_ = os.Remove(backupPath)
			return "", fmt.Errorf("close backup: %w", err)
		}
		return backupPath, nil
	}
}

// GitExecutor executes Git without a shell. Arguments are passed directly to
// exec.CommandContext, preventing shell interpolation of paths or repository
// values.
type GitExecutor struct {
	Binary string
}

// Command builds a Git command for callers that need to inspect or customize
// it before execution.
func (executor GitExecutor) Command(ctx context.Context, directory string, arguments ...string) *exec.Cmd {
	binary := executor.Binary
	if binary == "" {
		binary = "git"
	}
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Dir = directory
	return command
}

// Run executes Git and returns combined standard output and standard error.
func (executor GitExecutor) Run(ctx context.Context, directory string, arguments ...string) (string, error) {
	command := executor.Command(ctx, directory, arguments...)
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		return text, fmt.Errorf("git command failed: %w", err)
	}
	return text, nil
}

// EnsureMirror clones the configured repository into MirrorPath on first use
// and makes sure the existing working copy still points at RepositoryURL.
func (repository *Repository) EnsureMirror(ctx context.Context) error {
	if err := repository.validate(ctx); err != nil {
		return err
	}
	if err := repository.rejectContainingWorktree(ctx); err != nil {
		return err
	}

	_, err := os.Stat(repository.Config.MirrorPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(repository.Config.MirrorPath), 0o700); err != nil {
			return fmt.Errorf("create mirror parent directory: %w", err)
		}
		if _, err := repository.Git.Run(ctx, "", "clone", "--no-checkout", repository.Config.RepositoryURL, repository.Config.MirrorPath); err != nil {
			return fmt.Errorf("clone sync repository: %w", err)
		}
		if err := repository.writeMirrorMarker(); err != nil {
			return err
		}
	case err != nil:
		return fmt.Errorf("inspect sync mirror: %w", err)
	}
	if err := repository.validateMirror(ctx); err != nil {
		return err
	}

	remotes, err := repository.Git.Run(ctx, repository.Config.MirrorPath, "remote")
	if err != nil {
		return fmt.Errorf("list sync mirror remotes: %w", err)
	}
	if hasLine(remotes, "origin") {
		_, err = repository.Git.Run(ctx, repository.Config.MirrorPath, "remote", "set-url", "origin", repository.Config.RepositoryURL)
	} else {
		_, err = repository.Git.Run(ctx, repository.Config.MirrorPath, "remote", "add", "origin", repository.Config.RepositoryURL)
	}
	if err != nil {
		return fmt.Errorf("configure sync mirror remote: %w", err)
	}
	return nil
}

// Fetch updates the mirror and checks out the configured branch. It reports
// false, nil for a repository that has no such branch yet, which is the normal
// first-push state for an empty repository.
func (repository *Repository) Fetch(ctx context.Context) (bool, error) {
	if err := repository.EnsureMirror(ctx); err != nil {
		return false, err
	}
	refs, err := repository.Git.Run(ctx, repository.Config.MirrorPath, "ls-remote", "--heads", "origin", repository.branchRef())
	if err != nil {
		return false, fmt.Errorf("check remote branch: %w", err)
	}
	if strings.TrimSpace(refs) == "" {
		if _, err := repository.Git.Run(ctx, repository.Config.MirrorPath, "checkout", "-B", repository.Config.Branch); err != nil {
			return false, fmt.Errorf("create mirror branch: %w", err)
		}
		return false, nil
	}
	if _, err := repository.Git.Run(ctx, repository.Config.MirrorPath, "fetch", "origin", repository.branchRef()); err != nil {
		return false, fmt.Errorf("fetch remote branch: %w", err)
	}
	if _, err := repository.Git.Run(ctx, repository.Config.MirrorPath, "checkout", "-B", repository.Config.Branch, "FETCH_HEAD"); err != nil {
		return false, fmt.Errorf("checkout fetched branch: %w", err)
	}
	return true, nil
}

// Status fetches the remote configuration and compares it with the last saved
// synchronization state. An empty remote branch is local-only so an existing
// local file can seed the repository with Push.
func (repository *Repository) Status(ctx context.Context) (Status, error) {
	hasRemote, err := repository.Fetch(ctx)
	if err != nil {
		return "", err
	}
	localHash, err := HashFile(repository.Config.ConfigPath)
	if err != nil {
		return "", fmt.Errorf("hash local configuration: %w", err)
	}
	if !hasRemote {
		return StatusLocalOnly, nil
	}
	remoteHash, err := HashFile(repository.mirrorConfigPath())
	if err != nil {
		return "", fmt.Errorf("hash remote configuration: %w", err)
	}
	state, err := ReadState(repository.Config.StatePath)
	if errors.Is(err, os.ErrNotExist) {
		state = State{}
	} else if err != nil {
		return "", err
	}
	return DetectState(localHash, remoteHash, state), nil
}

// Push commits the current local configuration to the configured branch. It
// refuses to replace a changed remote file; callers must use ResolveConflict.
func (repository *Repository) Push(ctx context.Context) error {
	status, err := repository.Status(ctx)
	if err != nil {
		return err
	}
	switch status {
	case StatusConflict:
		return ErrConflict
	case StatusRemoteOnly:
		return ErrRemoteChanged
	}
	if err := repository.copyLocalToMirror(); err != nil {
		return err
	}
	if err := repository.commitAndPush(ctx); err != nil {
		return err
	}
	return repository.writeSynchronizedState()
}

// Pull applies the fetched remote configuration locally. It refuses to discard
// an unsynchronized local change; callers must use ResolveConflict instead.
func (repository *Repository) Pull(ctx context.Context) error {
	status, err := repository.Status(ctx)
	if err != nil {
		return err
	}
	switch status {
	case StatusConflict:
		return ErrConflict
	case StatusLocalOnly:
		return ErrLocalChanged
	}
	if err := copyFileAtomically(repository.mirrorConfigPath(), repository.Config.ConfigPath); err != nil {
		return fmt.Errorf("apply remote configuration: %w", err)
	}
	return repository.writeSynchronizedState()
}

// ResolveConflict saves both divergent versions, then either pushes the local
// version or applies the remote version according to resolution.
func (repository *Repository) ResolveConflict(ctx context.Context, resolution ConflictResolution) error {
	if resolution != ResolutionKeepLocal && resolution != ResolutionUseRemote {
		return fmt.Errorf("unknown conflict resolution %q", resolution)
	}
	status, err := repository.Status(ctx)
	if err != nil {
		return err
	}
	if status != StatusConflict {
		return fmt.Errorf("resolve conflict: current status is %q", status)
	}
	at := repository.now()
	if _, err := BackupFile(repository.Config.ConfigPath, repository.Config.BackupPath, "local", at); err != nil {
		return fmt.Errorf("back up local conflict version: %w", err)
	}
	if _, err := BackupFile(repository.mirrorConfigPath(), repository.Config.BackupPath, "remote", at); err != nil {
		return fmt.Errorf("back up remote conflict version: %w", err)
	}

	switch resolution {
	case ResolutionKeepLocal:
		if err := repository.copyLocalToMirror(); err != nil {
			return err
		}
		if err := repository.commitAndPush(ctx); err != nil {
			return err
		}
	case ResolutionUseRemote:
		if err := copyFileAtomically(repository.mirrorConfigPath(), repository.Config.ConfigPath); err != nil {
			return fmt.Errorf("apply remote conflict version: %w", err)
		}
	}
	return repository.writeSynchronizedState()
}

func (repository *Repository) validate(ctx context.Context) error {
	for name, value := range map[string]string{
		"repository URL":    repository.Config.RepositoryURL,
		"branch":            repository.Config.Branch,
		"local config path": repository.Config.ConfigPath,
		"mirror path":       repository.Config.MirrorPath,
		"state path":        repository.Config.StatePath,
		"backup path":       repository.Config.BackupPath,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("sync %s is required", name)
		}
	}
	if strings.HasPrefix(repository.Config.RepositoryURL, "-") {
		return errors.New("sync repository URL must not start with a dash")
	}
	if strings.HasPrefix(repository.Config.Branch, "-") {
		return errors.New("sync branch must not start with a dash")
	}
	if _, err := repository.Git.Run(ctx, "", "check-ref-format", "--branch", repository.Config.Branch); err != nil {
		return fmt.Errorf("validate sync branch: %w", err)
	}
	return nil
}

func (repository *Repository) mirrorConfigPath() string {
	return filepath.Join(repository.Config.MirrorPath, filepath.FromSlash(repositoryConfigPath))
}

func (repository *Repository) branchRef() string {
	return "refs/heads/" + repository.Config.Branch
}

func (repository *Repository) mirrorMarkerPath() string {
	return filepath.Join(repository.Config.MirrorPath, mirrorMarkerName)
}

func (repository *Repository) rejectContainingWorktree(ctx context.Context) error {
	probe, err := nearestExistingParent(repository.Config.MirrorPath)
	if err != nil {
		return fmt.Errorf("find sync mirror parent: %w", err)
	}
	root, err := repository.Git.Run(ctx, probe, "rev-parse", "--show-toplevel")
	if err != nil {
		// The nearest existing directory is not a Git worktree. A clone into a
		// child of it is safe, and validateMirror will check the clone itself.
		return nil
	}
	if sameFilesystemPath(root, repository.Config.MirrorPath) {
		return nil
	}
	return fmt.Errorf("sync mirror path must not be inside another Git worktree")
}

func (repository *Repository) validateMirror(ctx context.Context) error {
	root, err := repository.Git.Run(ctx, repository.Config.MirrorPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("validate sync mirror: %w", err)
	}
	if !sameFilesystemPath(root, repository.Config.MirrorPath) {
		return errors.New("sync mirror path must be the Git worktree root")
	}
	marker, err := os.ReadFile(repository.mirrorMarkerPath())
	if err != nil {
		return fmt.Errorf("validate sync mirror ownership: %w", err)
	}
	if string(marker) != mirrorMarkerContent {
		return errors.New("sync mirror ownership marker is invalid")
	}
	return nil
}

func (repository *Repository) writeMirrorMarker() (err error) {
	marker, err := os.OpenFile(repository.mirrorMarkerPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create sync mirror ownership marker: %w", err)
	}
	defer func() {
		if closeErr := marker.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close sync mirror ownership marker: %w", closeErr)
		}
	}()
	if _, err := marker.WriteString(mirrorMarkerContent); err != nil {
		return fmt.Errorf("write sync mirror ownership marker: %w", err)
	}
	if err := marker.Sync(); err != nil {
		return fmt.Errorf("flush sync mirror ownership marker: %w", err)
	}
	return nil
}

func nearestExistingParent(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for {
		_, err := os.Stat(path)
		if err == nil {
			return path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", fmt.Errorf("no existing parent for %q", path)
		}
		path = parent
	}
}

func sameFilesystemPath(left, right string) bool {
	left, leftErr := filepath.Abs(left)
	right, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	left, leftErr = filepath.EvalSymlinks(left)
	right, rightErr = filepath.EvalSymlinks(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func (repository *Repository) copyLocalToMirror() error {
	if err := copyFileAtomically(repository.Config.ConfigPath, repository.mirrorConfigPath()); err != nil {
		return fmt.Errorf("copy local configuration into mirror: %w", err)
	}
	return nil
}

func (repository *Repository) commitAndPush(ctx context.Context) error {
	if _, err := repository.Git.Run(ctx, repository.Config.MirrorPath, "add", "--", repositoryConfigPath); err != nil {
		return fmt.Errorf("stage configuration: %w", err)
	}
	changes, err := repository.Git.Run(ctx, repository.Config.MirrorPath, "status", "--porcelain=v1", "--", repositoryConfigPath)
	if err != nil {
		return fmt.Errorf("inspect staged configuration: %w", err)
	}
	if changes == "" {
		return nil
	}
	message := "sync config " + repository.now().UTC().Format(time.RFC3339)
	if _, err := repository.Git.Run(ctx, repository.Config.MirrorPath,
		"-c", "user.name=MyKeymap Sync",
		"-c", "user.email=mykeymap-sync@localhost",
		"commit", "-m", message,
	); err != nil {
		return fmt.Errorf("commit configuration: %w", err)
	}
	ref := repository.branchRef()
	if _, err := repository.Git.Run(ctx, repository.Config.MirrorPath, "push", "-u", "origin", ref+":"+ref); err != nil {
		return fmt.Errorf("push configuration: %w", err)
	}
	return nil
}

func (repository *Repository) writeSynchronizedState() error {
	localHash, err := HashFile(repository.Config.ConfigPath)
	if err != nil {
		return fmt.Errorf("hash synchronized local configuration: %w", err)
	}
	if err := WriteStateAtomically(repository.Config.StatePath, State{
		LocalHash:  localHash,
		RemoteHash: localHash,
		SyncedAt:   repository.now(),
	}); err != nil {
		return err
	}
	return nil
}

func (repository *Repository) now() time.Time {
	if repository.Now != nil {
		return repository.Now()
	}
	return time.Now().UTC()
}

func copyFileAtomically(sourcePath, destinationPath string) (err error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer source.Close()

	directory := filepath.Dir(destinationPath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(destinationPath)+".tmp-")
	if err != nil {
		return fmt.Errorf("create temporary destination: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("set temporary destination permissions: %w", err)
	}
	if _, err := io.Copy(temporary, source); err != nil {
		temporary.Close()
		return fmt.Errorf("copy destination: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("flush temporary destination: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary destination: %w", err)
	}
	if err := replaceFileAtomically(temporaryPath, destinationPath); err != nil {
		return fmt.Errorf("replace destination atomically: %w", err)
	}
	return nil
}

func hasLine(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
