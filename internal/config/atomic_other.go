//go:build !windows

package config

import (
	"fmt"
	"os"
)

// atomicWriteFile writes data to targetPath atomically on non-Windows platforms.
func atomicWriteFile(targetPath string, data []byte, perm os.FileMode) error {
	tmpPath := targetPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("failed to create atomic temp file %s: %w", tmpPath, err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write atomic temp file %s: %w", tmpPath, err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to sync atomic temp file %s: %w", tmpPath, err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close atomic temp file %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to atomically replace %s: %w", targetPath, err)
	}

	return nil
}
