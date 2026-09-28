package persist

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretStoreRejectsUnsafeIdentifiers(t *testing.T) {
	store, err := OpenSecrets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		instance string
		key      string
	}{
		{"", "token"},
		{"Plex", "token"},
		{"../plex", "token"},
		{"plex/main", "token"},
		{"plex-main", ""},
		{"plex-main", "../token"},
		{strings.Repeat("a", 65), "token"},
	}
	for _, test := range tests {
		if _, _, err := store.Stage(test.instance, test.key, []byte("secret")); err == nil {
			t.Errorf("Stage(%q, %q) error = nil", test.instance, test.key)
		}
	}
}

func TestStagedRevisionIsNotUsedUntilStateReferencesIt(t *testing.T) {
	store, err := OpenSecrets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldRef, _, err := store.Stage("plex-main", "token", []byte("old-secret"))
	if err != nil {
		t.Fatal(err)
	}
	newRef, _, err := store.Stage("plex-main", "token", []byte("new-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if oldRef == newRef {
		t.Fatal("Stage() reused an immutable revision")
	}

	activeState := State{SchemaVersion: CurrentSchemaVersion, Integrations: []Integration{{
		ID: "plex-main", SecretRefs: map[string]SecretRef{"token": oldRef},
	}}}
	active, err := store.Read(activeState.Integrations[0].SecretRefs["token"])
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "old-secret" {
		t.Fatalf("active secret = %q", active)
	}
	staged, err := store.Read(newRef)
	if err != nil || string(staged) != "new-secret" {
		t.Fatalf("staged secret = %q, error = %v", staged, err)
	}
}

func TestDiscardPreservesExistingRevision(t *testing.T) {
	store, err := OpenSecrets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldRef, _, err := store.Stage("plex-main", "token", []byte("old-secret"))
	if err != nil {
		t.Fatal(err)
	}
	newRef, discard, err := store.Stage("plex-main", "token", []byte("bad-replacement"))
	if err != nil {
		t.Fatal(err)
	}
	if err := discard(); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if err := discard(); err != nil {
		t.Fatalf("second discard must be idempotent: %v", err)
	}
	oldValue, err := store.Read(oldRef)
	if err != nil || string(oldValue) != "old-secret" {
		t.Fatalf("old secret = %q, error = %v", oldValue, err)
	}
	if _, err := store.Read(newRef); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("discarded Read() error = %v, want os.ErrNotExist", err)
	}
}

func TestCollectRemovesOnlyUnreferencedRevisions(t *testing.T) {
	store, err := OpenSecrets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	liveRef, _, err := store.Stage("plex-main", "token", []byte("live"))
	if err != nil {
		t.Fatal(err)
	}
	deadRef, _, err := store.Stage("plex-main", "token", []byte("dead"))
	if err != nil {
		t.Fatal(err)
	}
	otherRef, _, err := store.Stage("weather-main", "api-key", []byte("weather"))
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Collect(map[SecretRef]struct{}{liveRef: {}, otherRef: {}}); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, ref := range []SecretRef{liveRef, otherRef} {
		if _, err := store.Read(ref); err != nil {
			t.Fatalf("live Read(%#v) error = %v", ref, err)
		}
	}
	if _, err := store.Read(deadRef); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dead Read() error = %v, want os.ErrNotExist", err)
	}
}

func TestSecretStoreUses0600Files(t *testing.T) {
	root := t.TempDir()
	store, err := OpenSecrets(root)
	if err != nil {
		t.Fatal(err)
	}
	ref, _, err := store.Stage("plex-main", "token", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	assertMode(t, filepath.Join(root, "secrets"), 0o700)
	assertMode(t, filepath.Join(root, "secrets", ref.InstanceID), 0o700)
	assertMode(t, filepath.Join(root, "secrets", ref.InstanceID, ref.Key), 0o700)
	assertMode(t, filepath.Join(root, "secrets", ref.InstanceID, ref.Key, ref.Revision), 0o600)
}

func TestSecretValuesNeverMarshalThroughState(t *testing.T) {
	store, err := OpenSecrets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref, _, err := store.Stage("plex-main", "token", []byte("must-never-marshal"))
	if err != nil {
		t.Fatal(err)
	}
	state := State{SchemaVersion: CurrentSchemaVersion, Integrations: []Integration{{
		ID: "plex-main", Type: "plex", SecretRefs: map[string]SecretRef{"token": ref},
	}}}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "must-never-marshal") {
		t.Fatalf("state contains secret: %s", encoded)
	}
}

func TestSecretStoreRejectsEmptyAndOversizedValues(t *testing.T) {
	store, err := OpenSecrets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range [][]byte{nil, {}, make([]byte, MaxSecretBytes+1)} {
		if _, _, err := store.Stage("plex-main", "token", value); err == nil {
			t.Fatalf("Stage(%d bytes) error = nil", len(value))
		}
	}
}

func TestSecretStoreRejectsSymlinkedPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "secrets", "plex-main")); err != nil {
		t.Fatal(err)
	}
	store, err := OpenSecrets(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Stage("plex-main", "token", []byte("secret")); err == nil {
		t.Fatal("Stage() followed an instance symlink")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("secret escaped root: %#v", entries)
	}
}
