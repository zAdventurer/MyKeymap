package sync

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
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
	var localRoot, remoteRoot map[string]any
	if err := json.Unmarshal(localData, &local); err != nil {
		return nil, fmt.Errorf("parse local configuration: %w", err)
	}
	if err := json.Unmarshal(localData, &localRoot); err != nil {
		return nil, fmt.Errorf("parse local configuration: %w", err)
	}
	if err := json.Unmarshal(remoteData, &remote); err != nil {
		return nil, fmt.Errorf("parse remote configuration: %w", err)
	}
	if err := json.Unmarshal(remoteData, &remoteRoot); err != nil {
		return nil, fmt.Errorf("parse remote configuration: %w", err)
	}
	delete(localRoot, "keymaps")
	delete(localRoot, "options")
	delete(remoteRoot, "keymaps")
	delete(remoteRoot, "options")

	differences := compareKeymaps(local.Keymaps, remote.Keymaps)
	differences = append(differences, compareValues("普通设置", "", local.Options, remote.Options)...)
	differences = append(differences, compareValues("其他", "", localRoot, remoteRoot)...)
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
			result = append(result, SemanticDifference{Group: group, Path: label, Kind: kind, Local: localText, Remote: remoteText})
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

func compareKeymapFields(group, prefix string, local, remote semanticKeymap) []SemanticDifference {
	checks := []struct {
		name          string
		local, remote any
	}{
		{"是否启用", local.Enable, remote.Enable},
		{"延迟", local.Delay, remote.Delay},
		{"禁用条件", local.DisableAt, remote.DisableAt},
	}
	var result []SemanticDifference
	for _, check := range checks {
		if reflect.DeepEqual(check.local, check.remote) {
			continue
		}
		result = append(result, SemanticDifference{
			Group: group, Path: prefix + " → " + check.name, Kind: DifferenceModified,
			Local: formatValue(check.local), Remote: formatValue(check.remote),
		})
	}
	return result
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
	return []SemanticDifference{{Group: group, Path: path, Kind: kind, Local: formatValue(local), Remote: formatValue(remote)}}
}

func optionLabel(key string) string {
	labels := map[string]string{
		"startup":        "启动时运行",
		"language":       "界面语言",
		"keyboardLayout": "键盘布局",
		"hideMatrix":     "隐藏键盘矩阵",
	}
	if label := labels[key]; label != "" {
		return label
	}
	return key
}

func formatValue(value any) string {
	if value == nil {
		return "未配置"
	}
	if boolean, ok := value.(bool); ok {
		if boolean {
			return "启用"
		}
		return "禁用"
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func describeKeymap(keymap semanticKeymap) string {
	state := "禁用"
	if keymap.Enable {
		state = "启用"
	}
	return fmt.Sprintf("%s；%d 个映射", state, len(keymap.Hotkeys))
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
		target := stringValue(action["remapToKey"])
		if target == "" {
			return "按键映射"
		}
		return "映射到 " + target
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
	base := filepath.Base(strings.ReplaceAll(target, `\`, string(filepath.Separator)))
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

func unionSortedKeys[V any](left, right map[string]V) []string {
	set := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		set[key] = struct{}{}
	}
	for key := range right {
		set[key] = struct{}{}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
