package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestInitDataSecuresOnlyExplicitDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"init-data", "--path", root, "--uid", strconv.Itoa(os.Getuid()), "--gid", strconv.Itoa(os.Getgid())}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("data directory permissions = %o", info.Mode().Perm())
	}

	link := filepath.Join(t.TempDir(), "data")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"init-data", "--path", link, "--uid", strconv.Itoa(os.Getuid()), "--gid", strconv.Itoa(os.Getgid())}); err == nil {
		t.Fatal("symlinked data directory accepted")
	}
	if err := run([]string{"init-data", "--path", "/", "--uid", "0", "--gid", "0"}); err == nil {
		t.Fatal("filesystem root accepted")
	}
}
