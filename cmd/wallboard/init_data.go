package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func initializeDataDirectory(path string, uid, gid int) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean == string(filepath.Separator) {
		return errors.New("data path must be an explicit absolute directory")
	}
	if uid < 0 || gid < 0 {
		return errors.New("uid and gid must not be negative")
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return fmt.Errorf("inspect data directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("data path must be a real directory")
	}
	if err := os.Chown(clean, uid, gid); err != nil {
		return fmt.Errorf("set data directory owner: %w", err)
	}
	if err := os.Chmod(clean, 0o700); err != nil {
		return fmt.Errorf("secure data directory: %w", err)
	}
	return nil
}
