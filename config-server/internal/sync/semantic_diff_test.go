package sync

import (
	"reflect"
	"strings"
	"testing"
)

func TestDescribeActions(t *testing.T) {
	tests := []struct {
		name    string
		actions []map[string]any
		want    string
	}{
		{"program", []map[string]any{{"actionTypeID": float64(1), "comment": "Typora", "target": `shortcuts\Typora.lnk`}}, "打开程序：Typora"},
		{"remap", []map[string]any{{"actionTypeID": float64(5), "remapToKey": "F10"}}, "映射到 F10"},
		{"keys", []map[string]any{{"actionTypeID": float64(6), "keysToSend": "^c"}}, "发送按键：^c"},
		{"ahk", []map[string]any{{"actionTypeID": float64(8), "comment": "切换 QQ", "ahkCode": "implementation"}}, "切换 QQ"},
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

func TestBuildSemanticDiffIncludesKeymapMetadataAndOtherConfiguration(t *testing.T) {
	local := []byte(`{"keymaps":[{"id":8,"name":"数字层","hotkey":"*3","enable":false,"delay":150,"disableAt":"","hotkeys":{}}],"options":{},"schemaVersion":1}`)
	remote := []byte(`{"keymaps":[{"id":8,"name":"数字层","hotkey":"*3","enable":true,"delay":0,"disableAt":"","hotkeys":{}}],"options":{},"schemaVersion":2}`)

	differences, err := BuildSemanticDiff(local, remote)
	if err != nil {
		t.Fatal(err)
	}
	want := []SemanticDifference{
		{Group: "快捷键映射", Path: "数字层 → 延迟", Kind: DifferenceModified, Local: "150", Remote: "0"},
		{Group: "快捷键映射", Path: "数字层 → 是否启用", Kind: DifferenceModified, Local: "禁用", Remote: "启用"},
		{Group: "其他", Path: "schemaVersion", Kind: DifferenceModified, Local: "1", Remote: "2"},
	}
	if !reflect.DeepEqual(differences, want) {
		t.Fatalf("BuildSemanticDiff() = %#v, want %#v", differences, want)
	}
}
