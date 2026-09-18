package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDetectState(t *testing.T) {
	state := State{LocalHash: "local-before", RemoteHash: "remote-before"}

	tests := []struct {
		name       string
		localHash  string
		remoteHash string
		state      State
		want       Status
	}{
		{"synchronized when both hashes match metadata", "local-before", "remote-before", state, StatusSynchronized},
		{"local only when only local hash changed", "local-after", "remote-before", state, StatusLocalOnly},
		{"remote only when only remote hash changed", "local-before", "remote-after", state, StatusRemoteOnly},
		{"conflict when both hashes changed", "local-after", "remote-after", state, StatusConflict},
		{"synchronized without metadata when content matches", "same", "same", State{}, StatusSynchronized},
		{"conflict without metadata when content differs", "local", "remote", State{}, StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectState(tt.localHash, tt.remoteHash, tt.state); got != tt.want {
				t.Fatalf("DetectState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHashFileUsesSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"key\": \"value\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const want = "844d7743b13e1bdd66b003c29ebe5184dcf985434dde9f125952595cd533213e"
	if got != want {
		t.Fatalf("HashFile() = %q, want %q", got, want)
	}
}

func TestWriteAndReadStateAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "sync-state.json")
	want := State{
		LocalHash:  "local-hash",
		RemoteHash: "remote-hash",
		SyncedAt:   time.Date(2026, time.September, 2, 3, 4, 5, 0, time.UTC),
	}

	if err := WriteStateAtomically(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadState() = %#v, want %#v", got, want)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary state file %q was not cleaned up", entry.Name())
		}
	}
}

func TestWriteStateAtomicallyReplacesExistingState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync-state.json")
	if err := WriteStateAtomically(path, State{LocalHash: "old-local", RemoteHash: "old-remote"}); err != nil {
		t.Fatal(err)
	}
	want := State{
		LocalHash:  "new-local",
		RemoteHash: "new-remote",
		SyncedAt:   time.Date(2026, time.September, 2, 3, 4, 5, 0, time.UTC),
	}

	if err := WriteStateAtomically(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadState() after replacement = %#v, want %#v", got, want)
	}
}

func TestBackupFileCreatesTimestampedCopy(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "config.json")
	contents := []byte("{\"profiles\": []}\n")
	if err := os.WriteFile(source, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := BackupFile(source, filepath.Join(root, "backups"), "local", time.Date(2026, time.September, 2, 3, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(backup) != "config.local.20260902T030405Z.json" {
		t.Fatalf("backup name = %q", filepath.Base(backup))
	}
	got, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, contents) {
		t.Fatalf("backup content = %q, want %q", got, contents)
	}
}

func TestBackupFileCreatesDistinctCopiesForSameTimestamp(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "config.json")
	contents := []byte("{\"profiles\": []}\n")
	if err := os.WriteFile(source, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	backupDirectory := filepath.Join(root, "backups")
	at := time.Date(2026, time.September, 2, 3, 4, 5, 0, time.UTC)

	first, err := BackupFile(source, backupDirectory, "local", at)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BackupFile(source, backupDirectory, "local", at)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("backup paths must differ, both were %q", first)
	}
	for _, backup := range []string{first, second} {
		got, err := os.ReadFile(backup)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, contents) {
			t.Fatalf("backup content = %q, want %q", got, contents)
		}
	}
}

func TestGitExecutorBuildsCommandsWithArgumentArray(t *testing.T) {
	dir := t.TempDir()
	executor := GitExecutor{Binary: "git"}
	cmd := executor.Command(context.Background(), dir, "status", "--porcelain=v1")

	if cmd.Dir != dir {
		t.Fatalf("command dir = %q, want %q", cmd.Dir, dir)
	}
	wantArgs := []string{"git", "status", "--porcelain=v1"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("command args = %#v, want %#v", cmd.Args, wantArgs)
	}
}

func TestGitExecutorRunsGit(t *testing.T) {
	output, err := (GitExecutor{}).Run(context.Background(), "", "--version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output, "git version ") {
		t.Fatalf("git output = %q", output)
	}
}

func TestRepositoryPushInitializesMirrorAndPushesConfiguration(t *testing.T) {
	repository, config := newRepositoryTestFixture(t, `{"name":"initial"}`)

	if err := repository.Push(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(config.MirrorPath, ".git")); err != nil {
		t.Fatalf("mirror was not initialized: %v", err)
	}
	if got := readBareRepositoryConfig(t, config.RepositoryURL, config.Branch); got != `{"name":"initial"}` {
		t.Fatalf("remote config = %q", got)
	}
	state, err := ReadState(config.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.LocalHash == "" || state.LocalHash != state.RemoteHash {
		t.Fatalf("state = %#v, want matching non-empty hashes", state)
	}
}

func TestRepositoryPullAppliesRemoteOnlyChanges(t *testing.T) {
	repository, config := newRepositoryTestFixture(t, `{"name":"initial"}`)
	ctx := context.Background()
	if err := repository.Push(ctx); err != nil {
		t.Fatal(err)
	}
	writeRemoteRepositoryConfig(t, config.RepositoryURL, config.Branch, `{"name":"remote"}`)

	if err := repository.Pull(ctx); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(config.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(contents); got != `{"name":"remote"}` {
		t.Fatalf("local config = %q", got)
	}
}

func TestRepositoryResolveConflictCreatesBackupsBeforeApplyingChoice(t *testing.T) {
	tests := []struct {
		name       string
		resolution ConflictResolution
		wantLocal  string
		wantRemote string
	}{
		{"keep local", ResolutionKeepLocal, `{"name":"local"}`, `{"name":"local"}`},
		{"use remote", ResolutionUseRemote, `{"name":"remote"}`, `{"name":"remote"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository, config := newRepositoryTestFixture(t, `{"name":"initial"}`)
			ctx := context.Background()
			if err := repository.Push(ctx); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config.ConfigPath, []byte(`{"name":"local"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			writeRemoteRepositoryConfig(t, config.RepositoryURL, config.Branch, `{"name":"remote"}`)

			if err := repository.ResolveConflict(ctx, tt.resolution); err != nil {
				t.Fatal(err)
			}

			assertFileContents(t, config.ConfigPath, tt.wantLocal)
			if got := readBareRepositoryConfig(t, config.RepositoryURL, config.Branch); got != tt.wantRemote {
				t.Fatalf("remote config = %q, want %q", got, tt.wantRemote)
			}
			assertBackupContents(t, config.BackupPath, "local", `{"name":"local"}`)
			assertBackupContents(t, config.BackupPath, "remote", `{"name":"remote"}`)
		})
	}
}

func TestRepositoryEnsureMirrorRejectsUnrelatedWorktreeBeforeChangingOrigin(t *testing.T) {
	tests := []struct {
		name       string
		mirrorPath func(string) string
	}{
		{"worktree root", func(path string) string { return path }},
		{"worktree subdirectory", func(path string) string { return filepath.Join(path, "nested") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository, config := newRepositoryTestFixture(t, `{"name":"initial"}`)
			unrelated := filepath.Join(t.TempDir(), "unrelated")
			executor := GitExecutor{}
			ctx := context.Background()
			if _, err := executor.Run(ctx, "", "init", unrelated); err != nil {
				t.Fatal(err)
			}
			if _, err := executor.Run(ctx, unrelated, "remote", "add", "origin", "keep-this-origin"); err != nil {
				t.Fatal(err)
			}
			config.MirrorPath = tt.mirrorPath(unrelated)
			if config.MirrorPath != unrelated {
				if err := os.MkdirAll(config.MirrorPath, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			repository.Config = config

			if err := repository.EnsureMirror(ctx); err == nil {
				t.Fatal("EnsureMirror() unexpectedly accepted an unrelated worktree")
			}
			origin, err := executor.Run(ctx, unrelated, "remote", "get-url", "origin")
			if err != nil {
				t.Fatal(err)
			}
			if origin != "keep-this-origin" {
				t.Fatalf("unrelated origin = %q, want unchanged origin", origin)
			}
		})
	}
}

func TestRepositoryEnsureMirrorRejectsDashPrefixedRepositoryAndBranch(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{
			name: "repository URL",
			change: func(config *Config) {
				config.RepositoryURL = "-not-a-repository"
			},
		},
		{
			name: "branch",
			change: func(config *Config) {
				config.Branch = "-not-a-branch"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository, config := newRepositoryTestFixture(t, `{"name":"initial"}`)
			tt.change(&config)
			repository.Config = config

			if err := repository.EnsureMirror(context.Background()); err == nil {
				t.Fatal("EnsureMirror() unexpectedly accepted a dash-prefixed Git argument")
			}
			if _, err := os.Stat(config.MirrorPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("mirror path was created despite invalid input: %v", err)
			}
		})
	}
}

func TestRepositoryResolveConflictRejectsInvalidResolutionWithoutBackups(t *testing.T) {
	repository, config := newRepositoryTestFixture(t, `{"name":"initial"}`)
	ctx := context.Background()
	if err := repository.Push(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ConfigPath, []byte(`{"name":"local"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRemoteRepositoryConfig(t, config.RepositoryURL, config.Branch, `{"name":"remote"}`)

	if err := repository.ResolveConflict(ctx, ConflictResolution("not-a-resolution")); err == nil {
		t.Fatal("ResolveConflict() unexpectedly accepted an invalid resolution")
	}
	if _, err := os.Stat(config.BackupPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup directory was created for invalid resolution: %v", err)
	}
}

func newRepositoryTestFixture(t *testing.T, initialConfig string) (*Repository, Config) {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	executor := GitExecutor{}
	if _, err := executor.Run(context.Background(), "", "init", "--bare", remote); err != nil {
		t.Fatal(err)
	}
	config := Config{
		RepositoryURL: remote,
		Branch:        "main",
		ConfigPath:    filepath.Join(root, "local", "config.json"),
		MirrorPath:    filepath.Join(root, "mirror"),
		StatePath:     filepath.Join(root, "sync-state.json"),
		BackupPath:    filepath.Join(root, "backups"),
	}
	if err := os.MkdirAll(filepath.Dir(config.ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ConfigPath, []byte(initialConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Repository{Config: config, Git: executor}, config
}

func writeRemoteRepositoryConfig(t *testing.T, remote, branch, contents string) {
	t.Helper()
	writer := filepath.Join(t.TempDir(), "writer")
	executor := GitExecutor{}
	ctx := context.Background()
	if _, err := executor.Run(ctx, "", "clone", remote, writer); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Run(ctx, writer, "checkout", "-B", branch, "origin/"+branch); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(writer, "data", "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{
		{"add", "--", "data/config.json"},
		{"-c", "user.name=Sync Test", "-c", "user.email=sync-test@example.invalid", "commit", "-m", "remote change"},
		{"push", "origin", branch},
	} {
		if _, err := executor.Run(ctx, writer, arguments...); err != nil {
			t.Fatal(err)
		}
	}
}

func readBareRepositoryConfig(t *testing.T, remote, branch string) string {
	t.Helper()
	output, err := (GitExecutor{}).Run(context.Background(), "", "--git-dir", remote, "show", branch+":data/config.json")
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(contents); got != want {
		t.Fatalf("file %q = %q, want %q", path, got, want)
	}
}

func assertBackupContents(t *testing.T, directory, label, want string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(directory, "config."+label+".*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("%s backups = %#v, want exactly one", label, paths)
	}
	assertFileContents(t, paths[0], want)
}
