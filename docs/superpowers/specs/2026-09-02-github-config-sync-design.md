# GitHub Configuration Sync Design

## Goal

Add bidirectional GitHub configuration sync for MyKeymap. The local `data/config.json` can be pulled from and pushed to `git@github.com:zAdventurer/MyKeymap.git` on `main`.

## Scope

- Add a GitHub Sync section to Settings.
- Reuse the installed `git` executable and existing SSH credentials.
- Store only sync metadata locally; never store GitHub tokens or SSH keys.
- Support status, pull, push, and explicit conflict resolution.
- Allow the user's existing customized configuration to seed the fork on first push.

## Architecture

Vue calls new localhost Go endpoints. A Go sync service owns Git commands, local mirror management, state persistence, and conflict detection. Git is executed with explicit argument arrays, not shell command strings. The service synchronizes only `data/config.json`.

The service records the content hashes of the local and remote configuration after every successful sync. Before pull or push, it compares those hashes with the current versions to determine whether only one side changed or both sides changed.

## User Experience

Settings shows repository URL, branch, sync status, and **Pull**, **Push**, and **View differences** actions. Defaults are the user's fork and `main`.

Pull replaces the local configuration and restarts MyKeymap only if the local configuration is unchanged since the last sync. Push copies the local configuration into the local mirror, creates a timestamped commit, and pushes it to the configured branch.

## Conflict Handling

- If only one side changed, the requested pull or push proceeds.
- If neither side changed, the app reports that configuration is already synchronized.
- If both sides changed, the app overwrites neither side. It saves timestamped local and remote backups, presents a textual diff, and requires a choice: keep local then push, or use remote then pull.

Git failures return sanitized stderr to the UI. Authentication failures instruct users to fix Git/SSH outside MyKeymap.

## Verification

Go unit tests cover command construction, hash-based sync-state detection, no-op handling, one-sided changes, two-sided conflicts, and backup selection. An integration test with a temporary bare repository covers initial push, pull, and explicit conflict resolution without contacting GitHub.
