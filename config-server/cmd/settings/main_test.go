package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestExecCmdReturnsLaunchError(t *testing.T) {
	result := reflect.ValueOf(execCmd).Call([]reflect.Value{reflect.ValueOf(filepath.Join(t.TempDir(), "missing.exe"))})
	if len(result) != 1 {
		t.Fatalf("execCmd returned %d values, want an error", len(result))
	}
	if result[0].IsNil() {
		t.Fatal("execCmd returned nil for a missing executable")
	}
}
