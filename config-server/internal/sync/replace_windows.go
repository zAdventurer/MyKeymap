//go:build windows

package sync

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var replaceFileW = syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW")

// replaceFileAtomically uses ReplaceFileW when a state file already exists.
// Unlike deleting the destination before renaming, ReplaceFileW keeps a valid
// destination visible throughout replacement.
func replaceFileAtomically(replacementPath, destinationPath string) error {
	if _, err := os.Stat(destinationPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.Rename(replacementPath, destinationPath)
		}
		return err
	}

	destination, err := syscall.UTF16PtrFromString(destinationPath)
	if err != nil {
		return fmt.Errorf("encode replacement destination: %w", err)
	}
	replacement, err := syscall.UTF16PtrFromString(replacementPath)
	if err != nil {
		return fmt.Errorf("encode replacement source: %w", err)
	}

	result, _, callErr := replaceFileW.Call(
		uintptr(unsafe.Pointer(destination)),
		uintptr(unsafe.Pointer(replacement)),
		0,
		0,
		0,
		0,
	)
	if result == 0 {
		return fmt.Errorf("replace existing state file: %w", callErr)
	}
	return nil
}
