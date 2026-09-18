# GitHub Configuration Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add safe bidirectional GitHub synchronization for MyKeymap's `data/config.json`.

**Architecture:** A Go `sync` package owns mirror setup, explicit Git subprocesses, metadata hashes, backup creation, and conflict state. Gin exposes it through `/sync` endpoints, while Vue renders a Settings-card workflow and never handles credentials.

**Tech Stack:** Go 1.20+, Gin, Vue 3, TypeScript, Vuetify, local Git over SSH.

---

### Task 1: Build and test the sync domain service

**Files:**
- Create: `config-server/internal/sync/service.go`
- Create: `config-server/internal/sync/service_test.go`
- Create: `config-server/internal/sync/types.go`

- [ ] Write failing table-driven tests for `DetectState(localHash, remoteHash, state)` covering synchronized, local-only, remote-only, and conflict results.
- [ ] Run `go test ./internal/sync` and confirm the package fails before implementation.
- [ ] Implement `Config`, `State`, `Status`, `DetectState`, SHA-256 hashing, atomic metadata writes, timestamped backups, and explicit Git command execution through `exec.CommandContext`.
- [ ] Run `go test ./internal/sync` and confirm all cases pass.

### Task 2: Add local Git-repository operations

**Files:**
- Modify: `config-server/internal/sync/service.go`
- Modify: `config-server/internal/sync/service_test.go`

- [ ] Write failing integration tests using a temporary bare repository for clone/init, push, pull, and conflict backup creation.
- [ ] Run `go test ./internal/sync -run TestRepository` and confirm failure before repository operations exist.
- [ ] Implement mirror initialization, `fetch`, config-file read/copy, timestamped commit creation, `push`, pull application, and explicit local/remote conflict-resolution operations.
- [ ] Run `go test ./internal/sync` and confirm unit and temporary-repository tests pass.

### Task 3: Expose sync operations through the settings server

**Files:**
- Modify: `config-server/cmd/settings/main.go`
- Create: `config-server/internal/sync/http.go`
- Test: `config-server/internal/sync/http_test.go`

- [ ] Write handler tests for `GET /sync/status`, `PUT /sync/settings`, `POST /sync/pull`, `POST /sync/push`, `GET /sync/diff`, and `POST /sync/resolve`.
- [ ] Run `go test ./internal/sync -run TestHTTP` and confirm failure before handlers exist.
- [ ] Register endpoints and map malformed inputs, Git failures, and unresolved conflicts to structured JSON responses without exposing secrets.
- [ ] Run `go test ./...` from `config-server` and confirm the complete server suite passes.

### Task 4: Add the Settings UI

**Files:**
- Modify: `config-ui/src/store/server.ts`
- Create: `config-ui/src/components/GitHubSync.vue`
- Modify: `config-ui/src/views/Settings.vue`

- [ ] Add typed client calls for all sync endpoints and write component tests for synchronized, busy, error, and conflict states if the existing frontend test toolchain supports them.
- [ ] Implement the sync card with repository URL, branch, status, Pull, Push, diff preview, and explicit “keep local” / “use remote” resolution controls.
- [ ] Mount the component in the existing Settings page and disable destructive actions while a request is active or a conflict is unresolved.
- [ ] Run the frontend's available build or test command from `config-ui` and confirm it completes successfully.

### Task 5: Package verification and documentation

**Files:**
- Modify: `readme.md`
- Modify: `readme.en.md`

- [ ] Document SSH prerequisites, initial push of an existing configuration, pull/push flow, and conflict-resolution guarantees.
- [ ] Run `go test ./...` in `config-server` and the frontend build/test command in `config-ui`.
- [ ] Review `git diff --check` and `git status --short`; commit the implementation with `feat: add GitHub config sync`.
