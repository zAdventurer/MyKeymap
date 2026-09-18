//go:build !windows

package sync

import "os"

func replaceFileAtomically(replacementPath, destinationPath string) error {
	return os.Rename(replacementPath, destinationPath)
}
