//go:build !windows

package config

import (
	"fmt"
	"os"
)

func ApplyDirectorySecurity(dir string) error {
	return os.Chmod(dir, 0700)
}

func VerifyDirectorySecurity(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("insecure directory permissions: %v", info.Mode().Perm())
	}
	return nil
}
