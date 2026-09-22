# MyKeymap Semantic Sync Diff Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the raw Git-only configuration comparison with a Chinese, semantic, two-column comparison while retaining a redacted raw JSON diff for advanced inspection.

**Architecture:** Parse and redact both complete configuration documents on the backend, compare MyKeymap keymaps/actions with stable identities, and return typed semantic differences plus the existing safe raw diff from `GET /sync/diff`. The Vue client only renders that response, grouping changed items into 快捷键映射、CapsLock 缩写、普通设置和其他. Conflict detection and whole-file resolution remain unchanged.

**Tech Stack:** Go 1.25 standard library, Gin, existing Git executor, Vue 3, TypeScript, Vuetify, Vite.

**Workspace:** Execute directly on the current `main` branch as requested. Do not create a worktree. Preserve existing D-drive backups and do not modify `data/config.json` during build or deployment.

---

## File structure

- Create `config-server/internal/sync/semantic_diff.go`: typed response records, stable keymap/action comparison, action descriptions, generic option comparison.
- Create `config-server/internal/sync/semantic_diff_test.go`: unit tests for matching, descriptions, ordering, redaction inputs and safe parse failure.
- Modify `config-server/internal/sync/http.go`: call the semantic comparator and return `differences` plus `rawDiff`.
- Modify `config-server/internal/sync/http_test.go`: verify the HTTP contract, malformed JSON failure and secret redaction.
- Modify `config-ui/src/store/server.ts`: define the structured response types.
- Modify `config-ui/src/components/GitHubSync.vue`: render grouped Chinese two-column differences and a collapsed copyable raw diff.
- Modify deployment files under `D:\apps\MyKeymap-2.0-beta33` only after all source tests/builds pass and MyKeymap is confirmed stopped.

### Task 1: Define semantic difference types and action descriptions

**Files:**
- Create: `config-server/internal/sync/semantic_diff.go`
- Test: `config-server/internal/sync/semantic_diff_test.go`

- [ ] **Step 1: Write failing action-description tests**

```go
package sync

import (
	"strings"
	"testing"
)

func TestDescribeActions(t *testing.T) {
	tests := []struct {
		name    string
		actions []map[string]any
		want    string
	}{
		{"program", []map[string]any{{"actionTypeID": float64(1), "comment": "Typora", "target": `shortcuts\\Typora.lnk`}}, "打开程序：Typora"},
		{"remap", []map[string]any{{"actionTypeID": float64(5), "remapToKey": "F10"}}, "映射到 F10"},
		{"keys", []map[string]any{{"actionTypeID": float64(6), "keysToSend": "^c"}}, "发送按键：^c"},
		{"ahk", []map[string]any{{"actionTypeID": float64(8), "comment": "切换 QQ", "ahkCode": "secret implementation"}}, "切换 QQ"},
		{"unknown", []map[string]any{{"actionTypeID": float64(88), "value": "x"}}, "未知动作（类型 88）"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeActions(tt.actions); !strings.Contains(got, tt.want) {
				t.Fatalf("describeActions() = %q, want substring %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test and verify RED**

Run from `config-server` using the portable Go executable:

```powershell
& "$env:TEMP\mykeymap-diff-fix-go-1.25.12\go\bin\go.exe" test ./internal/sync -run TestDescribeActions -count=1 -v
```

Expected: compilation fails because `describeActions` is undefined.

- [ ] **Step 3: Add the response types and action formatter**

Add these public types and the formatter to `semantic_diff.go`:

```go
package sync

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type DifferenceKind string

const (
	DifferenceAdded    DifferenceKind = "added"
	DifferenceRemoved  DifferenceKind = "removed"
	DifferenceModified DifferenceKind = "modified"
)

type SemanticDifference struct {
	Group  string         `json:"group"`
	Path   string         `json:"path"`
	Kind   DifferenceKind `json:"kind"`
	Local  string         `json:"local"`
	Remote string         `json:"remote"`
}

func describeActions(actions []map[string]any) string {
	if len(actions) == 0 {
		return "未配置"
	}
	descriptions := make([]string, 0, len(actions))
	for index, action := range actions {
		description := describeAction(action)
		if len(actions) > 1 {
			description = fmt.Sprintf("%d. %s", index+1, description)
		}
		descriptions = append(descriptions, description)
	}
	return strings.Join(descriptions, "\n")
}

func describeAction(action map[string]any) string {
	typeID := intValue(action["actionTypeID"])
	comment := cleanComment(stringValue(action["comment"]))
	switch typeID {
	case 0:
		return "无动作"
	case 1:
		name := comment
		if name == "" {
			name = displayTarget(stringValue(action["target"]))
		}
		return joinDescription("打开程序", name, stringValue(action["args"]))
	case 2:
		return valueActionDescription("系统动作", action, comment)
	case 3:
		return valueActionDescription("窗口动作", action, comment)
	case 4:
		return valueActionDescription("鼠标动作", action, comment)
	case 5:
		return joinDescription("映射到", stringValue(action["remapToKey"]))
	case 6:
		return joinDescription("发送按键", stringValue(action["keysToSend"]), comment)
	case 7:
		return valueActionDescription("输入文本", action, comment)
	case 8:
		if comment != "" {
			return comment
		}
		return "执行 AHK 代码"
	case 9:
		return valueActionDescription("MyKeymap 内置动作", action, comment)
	default:
		return fmt.Sprintf("未知动作（类型 %d）", typeID)
	}
}

func valueActionDescription(prefix string, action map[string]any, comment string) string {
	if comment != "" {
		return joinDescription(prefix, comment)
	}
	return fmt.Sprintf("%s（编号 %d）", prefix, intValue(action["actionValueID"]))
}

func joinDescription(prefix string, values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			parts = append(parts, strings.TrimSpace(value))
		}
	}
	if len(parts) == 0 {
		return prefix
	}
	return prefix + "：" + strings.Join(parts, " ")
}

func cleanComment(comment string) string {
	if strings.HasPrefix(comment, "label:") {
		return ""
	}
	return strings.TrimSpace(comment)
}

func displayTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	base := filepath.Base(strings.ReplaceAll(target, `\\`, string(filepath.Separator)))
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		parsed, _ := strconv.Atoi(typed.String())
		return parsed
	default:
		return 0
	}
}
```

- [ ] **Step 4: Format and run the focused test**

```powershell
& "$env:TEMP\mykeymap-diff-fix-go-1.25.12\go\bin\gofmt.exe" -w internal/sync/semantic_diff.go internal/sync/semantic_diff_test.go
& "$env:TEMP\mykeymap-diff-fix-go-1.25.12\go\bin\go.exe" test ./internal/sync -run TestDescribeActions -count=1 -v
```

Expected: `PASS`.

- [ ] **Step 5: Commit the formatter**

```powershell
git add config-server/internal/sync/semantic_diff.go config-server/internal/sync/semantic_diff_test.go
git commit -m "feat: describe MyKeymap sync actions"
```

### Task 2: Compare keymaps and ordinary settings semantically

**Files:**
- Modify: `config-server/internal/sync/semantic_diff.go`
- Modify: `config-server/internal/sync/semantic_diff_test.go`

- [ ] **Step 1: Add failing comparison tests**

Append tests that pass complete JSON documents to the public comparator:

```go
func TestBuildSemanticDiff(t *testing.T) {
	local := []byte(`{"keymaps":[{"id":5,"name":"CapsLock 缩写","hotkey":"capslockAbbr","parentID":0,"enable":true,"delay":0,"disableAt":"","hotkeys":{"ty":[{"actionTypeID":1,"comment":"Typora","target":"shortcuts\\Typora.lnk"}]}}],"options":{"startup":true,"language":"zh"}}`)
	remote := []byte(`{"keymaps":[{"id":5,"name":"CapsLock 缩写","hotkey":"capslockAbbr","parentID":0,"enable":true,"delay":0,"disableAt":"","hotkeys":{"ty":[{"actionTypeID":8,"comment":"运行 testy 脚本","ahkCode":"Run testy"}],"gb":[{"actionTypeID":1,"comment":"Git Bash","target":"C:\\Program Files\\Git\\git-bash.exe"}]}}],"options":{"startup":false,"language":"zh"}}`)

	differences, err := BuildSemanticDiff(local, remote)
	if err != nil {
		t.Fatal(err)
	}
	want := []SemanticDifference{
		{Group: "CapsLock 缩写", Path: "CapsLock 缩写 → gb", Kind: DifferenceAdded, Local: "未配置", Remote: "打开程序：Git Bash"},
		{Group: "CapsLock 缩写", Path: "CapsLock 缩写 → ty", Kind: DifferenceModified, Local: "打开程序：Typora", Remote: "运行 testy 脚本"},
		{Group: "普通设置", Path: "启动时运行", Kind: DifferenceModified, Local: "启用", Remote: "禁用"},
	}
	if !reflect.DeepEqual(differences, want) {
		t.Fatalf("BuildSemanticDiff() = %#v, want %#v", differences, want)
	}
}

func TestBuildSemanticDiffIgnoresKeymapAndObjectOrder(t *testing.T) {
	local := []byte(`{"keymaps":[{"id":8,"hotkey":"*j","parentID":0,"hotkeys":{"*b":[{"actionTypeID":5,"remapToKey":"Left"}],"*a":[{"actionTypeID":5,"remapToKey":"Right"}]}}],"options":{"language":"zh","startup":true}}`)
	remote := []byte(`{"options":{"startup":true,"language":"zh"},"keymaps":[{"parentID":0,"hotkeys":{"*a":[{"remapToKey":"Right","actionTypeID":5}],"*b":[{"remapToKey":"Left","actionTypeID":5}]},"hotkey":"*j","id":8}]}`)
	differences, err := BuildSemanticDiff(local, remote)
	if err != nil {
		t.Fatal(err)
	}
	if len(differences) != 0 {
		t.Fatalf("ordering-only change produced differences: %#v", differences)
	}
}

func TestBuildSemanticDiffRejectsMalformedJSON(t *testing.T) {
	if _, err := BuildSemanticDiff([]byte(`{"keymaps":`), []byte(`{}`)); err == nil {
		t.Fatal("BuildSemanticDiff() error = nil, want malformed JSON error")
	}
}
```

Add `reflect` to the test imports.

- [ ] **Step 2: Run and verify RED**

```powershell
& "$env:TEMP\mykeymap-diff-fix-go-1.25.12\go\bin\go.exe" test ./internal/sync -run 'TestBuildSemanticDiff' -count=1 -v
```

Expected: compilation fails because `BuildSemanticDiff` is undefined.

- [ ] **Step 3: Implement stable semantic comparison**

Add these internal document shapes and comparison entry point:

```go
type semanticConfig struct {
	Keymaps []semanticKeymap `json:"keymaps"`
	Options map[string]any   `json:"options"`
}

type semanticKeymap struct {
	ID        int                         `json:"id"`
	Name      string                      `json:"name"`
	Enable    bool                        `json:"enable"`
	Hotkey    string                      `json:"hotkey"`
	ParentID  int                         `json:"parentID"`
	Delay     int                         `json:"delay"`
	DisableAt string                      `json:"disableAt"`
	Hotkeys   map[string][]map[string]any `json:"hotkeys"`
}

func BuildSemanticDiff(localData, remoteData []byte) ([]SemanticDifference, error) {
	var local, remote semanticConfig
	if err := json.Unmarshal(localData, &local); err != nil {
		return nil, fmt.Errorf("parse local configuration: %w", err)
	}
	if err := json.Unmarshal(remoteData, &remote); err != nil {
		return nil, fmt.Errorf("parse remote configuration: %w", err)
	}

	differences := compareKeymaps(local.Keymaps, remote.Keymaps)
	differences = append(differences, compareValues("普通设置", "", local.Options, remote.Options)...)
	groupRank := map[string]int{"快捷键映射": 0, "CapsLock 缩写": 1, "普通设置": 2, "其他": 3}
	sort.SliceStable(differences, func(i, j int) bool {
		if groupRank[differences[i].Group] != groupRank[differences[j].Group] {
			return groupRank[differences[i].Group] < groupRank[differences[j].Group]
		}
		return differences[i].Path < differences[j].Path
	})
	return differences, nil
}

func compareKeymaps(local, remote []semanticKeymap) []SemanticDifference {
	localByKey, remoteByKey := indexKeymaps(local), indexKeymaps(remote)
	keys := unionSortedKeys(localByKey, remoteByKey)
	var result []SemanticDifference
	for _, key := range keys {
		left, leftOK := localByKey[key]
		right, rightOK := remoteByKey[key]
		group := keymapGroup(left, right)
		label := keymapLabel(left, right)
		if !leftOK || !rightOK {
			kind := DifferenceAdded
			localText, remoteText := "未配置", describeKeymap(right)
			if leftOK {
				kind, localText, remoteText = DifferenceRemoved, describeKeymap(left), "未配置"
			}
			result = append(result, SemanticDifference{group, label, kind, localText, remoteText})
			continue
		}
		result = append(result, compareKeymapFields(group, label, left, right)...)
		result = append(result, compareHotkeys(group, label, left.Hotkeys, right.Hotkeys)...)
	}
	return result
}

func keymapIdentity(keymap semanticKeymap) string {
	if keymap.ID != 0 {
		return fmt.Sprintf("id:%d", keymap.ID)
	}
	return fmt.Sprintf("fallback:%d:%s", keymap.ParentID, keymap.Hotkey)
}

func indexKeymaps(keymaps []semanticKeymap) map[string]semanticKeymap {
	indexed := make(map[string]semanticKeymap, len(keymaps))
	for _, keymap := range keymaps {
		indexed[keymapIdentity(keymap)] = keymap
	}
	return indexed
}

func keymapGroup(left, right semanticKeymap) string {
	hotkey := left.Hotkey
	if hotkey == "" {
		hotkey = right.Hotkey
	}
	if hotkey == "capslockAbbr" {
		return "CapsLock 缩写"
	}
	return "快捷键映射"
}

func keymapLabel(left, right semanticKeymap) string {
	candidate := left
	if candidate.Hotkey == "" {
		candidate = right
	}
	if candidate.Hotkey == "capslockAbbr" {
		return "CapsLock 缩写"
	}
	if strings.TrimSpace(candidate.Name) != "" {
		return candidate.Name
	}
	return "快捷键层 " + candidate.Hotkey
}

func compareHotkeys(group, prefix string, local, remote map[string][]map[string]any) []SemanticDifference {
	var result []SemanticDifference
	for _, hotkey := range unionSortedKeys(local, remote) {
		left, leftOK := local[hotkey]
		right, rightOK := remote[hotkey]
		if leftOK && rightOK && reflect.DeepEqual(left, right) {
			continue
		}
		kind := DifferenceModified
		if !leftOK {
			kind = DifferenceAdded
		}
		if !rightOK {
			kind = DifferenceRemoved
		}
		result = append(result, SemanticDifference{
			Group: group, Path: prefix + " → " + hotkey, Kind: kind,
			Local: describeActions(left), Remote: describeActions(right),
		})
	}
	return result
}
```

Also add focused helpers in the same file:

```go
func compareKeymapFields(group, prefix string, local, remote semanticKeymap) []SemanticDifference {
	checks := []struct{ name string; local, remote any }{
		{"是否启用", local.Enable, remote.Enable},
		{"延迟", local.Delay, remote.Delay},
		{"禁用条件", local.DisableAt, remote.DisableAt},
	}
	var result []SemanticDifference
	for _, check := range checks {
		if reflect.DeepEqual(check.local, check.remote) {
			continue
		}
		result = append(result, SemanticDifference{group, prefix + " → " + check.name, DifferenceModified, formatValue(check.local), formatValue(check.remote)})
	}
	return result
}

func compareValues(group, path string, local, remote any) []SemanticDifference {
	localMap, localIsMap := local.(map[string]any)
	remoteMap, remoteIsMap := remote.(map[string]any)
	if localIsMap && remoteIsMap {
		var result []SemanticDifference
		for _, key := range unionSortedKeys(localMap, remoteMap) {
			childPath := optionLabel(key)
			if path != "" {
				childPath = path + " → " + childPath
			}
			result = append(result, compareValues(group, childPath, localMap[key], remoteMap[key])...)
		}
		return result
	}
	if reflect.DeepEqual(local, remote) {
		return nil
	}
	kind := DifferenceModified
	if local == nil {
		kind = DifferenceAdded
	}
	if remote == nil {
		kind = DifferenceRemoved
	}
	return []SemanticDifference{{group, path, kind, formatValue(local), formatValue(remote)}}
}

func optionLabel(key string) string {
	labels := map[string]string{"startup": "启动时运行", "language": "界面语言", "keyboardLayout": "键盘布局", "hideMatrix": "隐藏键盘矩阵"}
	if label := labels[key]; label != "" {
		return label
	}
	return key
}

func formatValue(value any) string {
	if value == nil { return "未配置" }
	if boolean, ok := value.(bool); ok {
		if boolean { return "启用" }
		return "禁用"
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func describeKeymap(keymap semanticKeymap) string {
	state := "禁用"
	if keymap.Enable { state = "启用" }
	return fmt.Sprintf("%s；%d 个映射", state, len(keymap.Hotkeys))
}

func unionSortedKeys[V any](left, right map[string]V) []string {
	set := make(map[string]struct{}, len(left)+len(right))
	for key := range left { set[key] = struct{}{} }
	for key := range right { set[key] = struct{}{} }
	keys := make([]string, 0, len(set))
	for key := range set { keys = append(keys, key) }
	sort.Strings(keys)
	return keys
}
```

Add `reflect` to `semantic_diff.go` imports. Keep action array order significant, but map and keymap array order insignificant.

- [ ] **Step 4: Run focused and package tests**

```powershell
& "$env:TEMP\mykeymap-diff-fix-go-1.25.12\go\bin\gofmt.exe" -w internal/sync/semantic_diff.go internal/sync/semantic_diff_test.go
& "$env:TEMP\mykeymap-diff-fix-go-1.25.12\go\bin\go.exe" test ./internal/sync -run 'TestBuildSemanticDiff|TestDescribeActions' -count=1 -v
& "$env:TEMP\mykeymap-diff-fix-go-1.25.12\go\bin\go.exe" test ./internal/sync -count=1
```

Expected: all tests pass.

- [ ] **Step 5: Commit semantic comparison**

```powershell
git add config-server/internal/sync/semantic_diff.go config-server/internal/sync/semantic_diff_test.go
git commit -m "feat: compare MyKeymap configs semantically"
```

### Task 3: Return structured differences from the sync API

**Files:**
- Modify: `config-server/internal/sync/http.go`
- Modify: `config-server/internal/sync/http_test.go`

- [ ] **Step 1: Add failing endpoint-contract and security tests**

Update the existing `GET /sync/diff` assertions to decode:

```go
var response struct {
	Status      Status               `json:"status"`
	Differences []SemanticDifference `json:"differences"`
	RawDiff     string               `json:"rawDiff"`
}
if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
	t.Fatal(err)
}
if len(response.Differences) == 0 {
	t.Fatal("GET /sync/diff returned no semantic differences")
}
if !strings.Contains(response.RawDiff, "diff --git") {
	t.Fatalf("rawDiff = %q, want unified diff", response.RawDiff)
}
```

Add a secret regression that uses `api_key`, `privateKey` and `cookie` in changed actions/options and asserts none of the three secret values occurs in either `response.Differences` after JSON marshaling or `response.RawDiff`. Retain the malformed JSON test and require HTTP 500 with only `sync_failed`.

- [ ] **Step 2: Run and verify RED**

```powershell
& "$env:TEMP\mykeymap-diff-fix-go-1.25.12\go\bin\go.exe" test ./internal/sync -run 'TestHTTP.*Diff' -count=1 -v
```

Expected: assertions fail because the endpoint still returns `diff` rather than `differences` and `rawDiff`.

- [ ] **Step 3: Build semantic differences from already-redacted documents**

In `HTTPHandler.diff`, after `localConfig` and `remoteConfig` have been read by `readRedactedConfig`, insert:

```go
differences, err := BuildSemanticDiff(localConfig, remoteConfig)
if err != nil {
	writeSyncError(c, err)
	return
}
```

Replace the final response with:

```go
c.JSON(http.StatusOK, gin.H{
	"status":      status,
	"differences": differences,
	"rawDiff":     string(output),
})
```

Do not parse unredacted bytes in `BuildSemanticDiff`; `readRedactedConfig` remains the single security boundary for both outputs.

- [ ] **Step 4: Run focused, package and full Go tests**

```powershell
& "$env:TEMP\mykeymap-diff-fix-go-1.25.12\go\bin\gofmt.exe" -w internal/sync/http.go internal/sync/http_test.go
& "$env:TEMP\mykeymap-diff-fix-go-1.25.12\go\bin\go.exe" test ./internal/sync -run 'TestHTTP.*Diff' -count=1 -v
& "$env:TEMP\mykeymap-diff-fix-go-1.25.12\go\bin\go.exe" test ./... -count=1
```

Expected: all Go tests pass; malformed JSON returns a generic structured failure; no secret text is returned.

- [ ] **Step 5: Commit the API contract**

```powershell
git add config-server/internal/sync/http.go config-server/internal/sync/http_test.go
git commit -m "feat: expose semantic configuration differences"
```

### Task 4: Render grouped local-versus-remote differences in Chinese

**Files:**
- Modify: `config-ui/src/store/server.ts`
- Modify: `config-ui/src/components/GitHubSync.vue`

- [ ] **Step 1: Change the TypeScript response contract**

Replace `SyncDiffResponse` with:

```ts
export type SyncDifferenceKind = 'added' | 'removed' | 'modified'

export interface SyncDifference {
  group: '快捷键映射' | 'CapsLock 缩写' | '普通设置' | '其他' | string
  path: string
  kind: SyncDifferenceKind
  local: string
  remote: string
}

export interface SyncDiffResponse {
  status: SyncStatus
  differences: SyncDifference[]
  rawDiff: string
}
```

- [ ] **Step 2: Replace raw-dialog state with structured state**

In `GitHubSync.vue`, import `SyncDifference`, then define:

```ts
const differences = ref<SyncDifference[]>([])
const rawDiff = ref('')
const rawExpanded = ref(false)
const copyMessage = ref('')

const groupedDifferences = computed(() => {
  const groups = new Map<string, SyncDifference[]>()
  for (const difference of differences.value) {
    const items = groups.get(difference.group) ?? []
    items.push(difference)
    groups.set(difference.group, items)
  }
  return Array.from(groups, ([name, items]) => ({ name, items }))
})

const kindPresentation: Record<SyncDifference['kind'], { label: string, color: string }> = {
  added: { label: '新增', color: 'success' },
  removed: { label: '删除', color: 'error' },
  modified: { label: '修改', color: 'warning' },
}
```

Update `viewDiff`:

```ts
async function viewDiff() {
  const response = await request('diffing', () => server.getSyncDiff())
  if (response) {
    status.value = response.status
    differences.value = response.differences ?? []
    rawDiff.value = response.rawDiff ?? ''
    rawExpanded.value = false
    copyMessage.value = ''
    showDiff.value = true
  }
}

async function copyRawDiff() {
  try {
    await navigator.clipboard.writeText(rawDiff.value)
    copyMessage.value = '已复制'
  } catch {
    copyMessage.value = '复制失败，请手动选择文本'
  }
}
```

- [ ] **Step 3: Replace the dialog template with grouped two-column cards**

Use this complete dialog body:

```vue
<v-dialog v-model="showDiff" max-width="1100">
  <v-card title="配置差异">
    <v-card-text class="diff-dialog-body">
      <v-alert v-if="differences.length === 0" type="info" variant="tonal">
        本地与远端没有可显示的配置差异。
      </v-alert>

      <section v-for="group in groupedDifferences" :key="group.name" class="mb-6">
        <h3 class="text-h6 mb-3">{{ group.name }}</h3>
        <v-card v-for="item in group.items" :key="`${item.path}:${item.kind}`" class="mb-3" variant="outlined">
          <v-card-title class="d-flex align-center flex-wrap ga-2 text-subtitle-1">
            <span>{{ item.path }}</span>
            <v-chip :color="kindPresentation[item.kind].color" size="small" label>
              {{ kindPresentation[item.kind].label }}
            </v-chip>
          </v-card-title>
          <v-card-text>
            <v-row>
              <v-col cols="12" md="6">
                <div class="text-caption text-medium-emphasis mb-1">本地</div>
                <div class="semantic-value">{{ item.local }}</div>
              </v-col>
              <v-col cols="12" md="6">
                <div class="text-caption text-medium-emphasis mb-1">远端</div>
                <div class="semantic-value">{{ item.remote }}</div>
              </v-col>
            </v-row>
          </v-card-text>
        </v-card>
      </section>

      <v-expansion-panels v-if="rawDiff" v-model="rawExpanded" class="mt-4">
        <v-expansion-panel :value="true" title="高级：原始 JSON 差异">
          <v-expansion-panel-text>
            <div class="d-flex align-center ga-2 mb-2">
              <v-btn class="text-none" size="small" variant="outlined" @click="copyRawDiff">复制</v-btn>
              <span class="text-caption">{{ copyMessage }}</span>
            </div>
            <pre class="sync-diff">{{ rawDiff }}</pre>
          </v-expansion-panel-text>
        </v-expansion-panel>
      </v-expansion-panels>
    </v-card-text>
    <v-card-actions class="justify-end">
      <v-btn class="text-none" color="primary" @click="showDiff = false">关闭</v-btn>
    </v-card-actions>
  </v-card>
</v-dialog>
```

Add styles:

```css
.diff-dialog-body { max-height: 75vh; overflow-y: auto; }
.semantic-value { white-space: pre-wrap; word-break: break-word; }
.sync-diff { max-height: 45vh; overflow: auto; padding: 12px; white-space: pre-wrap; word-break: break-word; background: #f5f5f5; border-radius: 4px; }
```

- [ ] **Step 4: Run frontend checks**

```powershell
npm run build
npx eslint src/store/server.ts src/components/GitHubSync.vue
```

Expected: Vite build and targeted ESLint pass. If `vue-tsc --noEmit` is run, document only the already-known pre-existing syntax errors in `src/store/config.ts:54`, `:66`, and `:67`; no new file may appear in its error list.

- [ ] **Step 5: Commit the UI**

```powershell
git add config-ui/src/store/server.ts config-ui/src/components/GitHubSync.vue
git commit -m "feat: render semantic sync differences"
```

### Task 5: Verify, deploy to the active D-drive installation, and clean temporary artifacts

**Files:**
- Build: `config-server/cmd/settings`
- Build: `config-ui/dist`
- Deploy: `D:\apps\MyKeymap-2.0-beta33\settings.exe`
- Deploy: `D:\apps\MyKeymap-2.0-beta33\site`

- [ ] **Step 1: Verify the source tree and tests from clean commands**

```powershell
git status --short
git diff --check
& "$env:TEMP\mykeymap-diff-fix-go-1.25.12\go\bin\go.exe" test ./... -count=1
npm run build
```

Run Go commands from `config-server` and npm commands from `config-ui`. Expected: no unstaged source edits, `git diff --check` exits 0, all Go tests pass, and Vite build succeeds.

- [ ] **Step 2: Record and protect the active configuration**

```powershell
$install = 'D:\apps\MyKeymap-2.0-beta33'
$beforeHash = (Get-FileHash -Algorithm SHA256 -LiteralPath "$install\data\config.json").Hash
Get-Process MyKeymap,settings -ErrorAction SilentlyContinue | Select-Object ProcessName,Id,Path
```

Expected: MyKeymap and settings processes are absent before deployment. If either process exists, stop and ask the user to exit; do not terminate it automatically.

- [ ] **Step 3: Build production artifacts outside the installation**

```powershell
$go = "$env:TEMP\mykeymap-diff-fix-go-1.25.12\go\bin\go.exe"
& $go build -o "$env:TEMP\mykeymap-semantic-diff-settings.exe" ./cmd/settings
npm run build
```

Expected: the executable and `config-ui/dist` are produced successfully.

- [ ] **Step 4: Back up and deploy only executable/site files**

```powershell
$install = 'D:\apps\MyKeymap-2.0-beta33'
$backup = "$install\.github-sync-semantic-diff-backup-$(Get-Date -Format yyyyMMdd-HHmmss)"
New-Item -ItemType Directory -LiteralPath $backup | Out-Null
Copy-Item -LiteralPath "$install\settings.exe" -Destination "$backup\settings.exe"
Copy-Item -LiteralPath "$install\site" -Destination "$backup\site" -Recurse
Copy-Item -LiteralPath "$env:TEMP\mykeymap-semantic-diff-settings.exe" -Destination "$install\settings.exe" -Force
Copy-Item -LiteralPath 'C:\Users\MSI\Desktop\MyKeymap\config-ui\dist' -Destination "$install\site" -Recurse -Force
```

Before recursive copy, resolve and verify `$backup` and `$install` both remain under `D:\apps\MyKeymap-2.0-beta33`. Do not remove existing `.github-sync-*` backups.

- [ ] **Step 5: Verify configuration integrity and live behavior**

```powershell
$afterHash = (Get-FileHash -Algorithm SHA256 -LiteralPath "$install\data\config.json").Hash
if ($beforeHash -ne $afterHash) { throw 'Deployment changed data/config.json' }
```

Start MyKeymap normally, open `http://localhost:12333`, then verify:

1. 设置页显示中文“GitHub 配置同步”。
2. “查看差异” groups changed items and shows local/remote columns.
3. The `ty` row describes the actual local and remote actions rather than JSON.
4. The advanced raw diff is collapsed initially and copy works.
5. Push, pull and whole-config conflict buttons are unchanged.

- [ ] **Step 6: Clean only task-owned temporary artifacts**

After all verification succeeds, delete these exact task-owned paths if present:

```powershell
Remove-Item -LiteralPath "$env:TEMP\mykeymap-semantic-diff-settings.exe" -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath "$env:TEMP\mykeymap-diff-fix-go-work" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath "$env:TEMP\mykeymap-diff-fix-go-modcache" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath "$env:TEMP\mykeymap-diff-fix-go-cache" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath "$env:TEMP\mykeymap-diff-fix-go-1.25.12" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath "$env:TEMP\mykeymap-diff-fix-go-1.25.12.windows-amd64.zip" -Force -ErrorAction SilentlyContinue
```

Do not delete the D-drive deployment backup or Git mirror/state directories.

- [ ] **Step 7: Final repository check**

```powershell
git status --short
git log -5 --oneline
```

Expected: working tree clean and the semantic diff commits appear on `main`. Do not push until the user explicitly asks to publish to GitHub.
