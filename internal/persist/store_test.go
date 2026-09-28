package persist

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCreatesVersionOneState(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")

	store, err := Open(root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if got := store.Snapshot().SchemaVersion; got != CurrentSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", got, CurrentSchemaVersion)
	}
	assertMode(t, root, 0o700)
	assertMode(t, filepath.Join(root, "state.json"), 0o600)
}

func TestUpdateWritesAtomicallyWith0600Permissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}

	err = store.Update(func(state *State) error {
		state.SetupComplete = true
		state.Server = ServerSettings{Listen: ":8080"}
		return nil
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	reopened, err := Open(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got := reopened.Snapshot()
	if !got.SetupComplete || got.Server.Listen != ":8080" {
		t.Fatalf("persisted state = %#v", got)
	}
	assertMode(t, filepath.Join(root, "state.json"), 0o600)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".state-") {
			t.Fatalf("temporary file was not removed: %s", entry.Name())
		}
	}
}

func TestOpenRejectsNewerSchemaWithoutChangingFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "state.json")
	want := []byte(`{"schema_version":999,"setup_complete":true}`)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(root); !errors.Is(err, ErrNewerSchema) {
		t.Fatalf("Open() error = %v, want ErrNewerSchema", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("newer state changed: got %q want %q", got, want)
	}
}

func TestOpenRejectsOversizedAndTruncatedState(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "oversized", data: bytes.Repeat([]byte("x"), int(MaxStateBytes+1))},
		{name: "truncated", data: []byte(`{"schema_version":1`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "data")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "state.json")
			if err := os.WriteFile(path, test.data, 0o600); err != nil {
				t.Fatal(err)
			}

			if _, err := Open(root); err == nil {
				t.Fatal("Open() error = nil")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, test.data) {
				t.Fatal("invalid state was modified")
			}
		})
	}
}

func TestInjectedAtomicWriteFailuresLeaveCompleteOldOrNewState(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	oldData := []byte(`{"schema_version":1,"setup_complete":false}`)
	newData := []byte(`{"schema_version":1,"setup_complete":true}`)
	if err := os.WriteFile(path, oldData, 0o600); err != nil {
		t.Fatal(err)
	}

	injected := errors.New("injected failure")
	err := writeAtomicWithHook(path, newData, 0o600, func(stage atomicStage) error {
		if stage == stageBeforeRename {
			return injected
		}
		return nil
	})
	if !errors.Is(err, injected) {
		t.Fatalf("before-rename error = %v", err)
	}
	assertFileEqualsOneOf(t, path, oldData)

	err = writeAtomicWithHook(path, newData, 0o600, func(stage atomicStage) error {
		if stage == stageAfterRename {
			return injected
		}
		return nil
	})
	if !errors.Is(err, injected) {
		t.Fatalf("after-rename error = %v", err)
	}
	assertFileEqualsOneOf(t, path, newData)
}

func TestSnapshotReturnsDeepClone(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(state *State) error {
		state.Integrations = []Integration{{
			ID: "plex-main", Type: "plex", Config: map[string]any{"url": "http://plex:32400"},
			SecretRefs: map[string]SecretRef{"token": {InstanceID: "plex-main", Key: "token", Revision: "rev1"}},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	copy := store.Snapshot()
	copy.Integrations[0].Config["url"] = "changed"
	delete(copy.Integrations[0].SecretRefs, "token")

	got := store.Snapshot().Integrations[0]
	if got.Config["url"] != "http://plex:32400" || len(got.SecretRefs) != 1 {
		t.Fatalf("snapshot mutation escaped clone: %#v", got)
	}
}

func TestOpenKeepsAtMostThreeMigrationBackups(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
		legacy := []byte(`{"schema_version":0,"setup_complete":false}`)
		if err := os.WriteFile(filepath.Join(root, "state.json"), legacy, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(root); err != nil {
			t.Fatalf("migration %d: %v", index, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("backup count = %d, want 3", len(entries))
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}

func assertFileEqualsOneOf(t *testing.T, path string, choices ...[]byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, choice := range choices {
		if bytes.Equal(got, choice) {
			return
		}
	}
	t.Fatalf("%s contains partial or unexpected data: %q", path, got)
}
