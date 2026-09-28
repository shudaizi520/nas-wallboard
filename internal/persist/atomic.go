package persist

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type atomicStage uint8

const (
	stageBeforeRename atomicStage = iota + 1
	stageAfterRename
)

func WriteAtomic(path string, data []byte, mode fs.FileMode) error {
	return writeAtomicWithHook(path, data, mode, nil)
}

func writeAtomicWithHook(path string, data []byte, mode fs.FileMode, hook func(atomicStage) error) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("secure directory: %w", err)
	}
	prefix := "." + strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + "-"
	temporary, err := os.CreateTemp(directory, prefix+"*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()

	if err := temporary.Chmod(mode.Perm()); err != nil {
		return fmt.Errorf("set temporary permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	closed = true
	if hook != nil {
		if err := hook(stageBeforeRename); err != nil {
			return err
		}
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	if hook != nil {
		if err := hook(stageAfterRename); err != nil {
			return err
		}
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}

func ReadLimited(path string, maximum int64) ([]byte, error) {
	if maximum < 0 {
		return nil, errors.New("maximum must not be negative")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("file exceeds %d bytes", maximum)
	}
	return data, nil
}
