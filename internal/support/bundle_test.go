package support

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
	"example.com/nas-wallboard/internal/persist"
	"filippo.io/age"
)

func TestRedactedBundleContainsOnlyFixedSafeFiles(t *testing.T) {
	input := BundleInput{
		Version: "v1.2.3", GeneratedAt: time.Unix(100, 0),
		State: persist.State{SchemaVersion: 1, SetupComplete: true, Integrations: []persist.Integration{{
			ID: "truenas-main", Type: "truenas", Enabled: true,
			Config:     map[string]any{"url": "wss://nas.local/api/current", "password": "must-not-escape"},
			SecretRefs: map[string]persist.SecretRef{"api_key": {InstanceID: "truenas-main", Key: "api_key", Revision: "private-revision"}},
		}}},
		Runtime: model.Snapshot{Version: "v1.2.3", System: model.Module[model.SystemStatus]{Data: model.SystemStatus{Hostname: "atlas", Version: "25.10"}}},
	}
	var output bytes.Buffer
	if err := WriteRedactedBundle(&output, input); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, file := range archive.File {
		if strings.Contains(file.Name, "..") || strings.HasPrefix(file.Name, "/") {
			t.Fatalf("unsafe ZIP name %q", file.Name)
		}
		names = append(names, file.Name)
		reader, _ := file.Open()
		body, _ := io.ReadAll(reader)
		_ = reader.Close()
		for _, secret := range []string{"must-not-escape", "private-revision", "api_key"} {
			if bytes.Contains(body, []byte(secret)) {
				t.Fatalf("%s leaked in %s", secret, file.Name)
			}
		}
	}
	want := `["overview.json","runtime.json","state-redacted.json"]`
	encoded, _ := json.Marshal(names)
	if string(encoded) != want {
		t.Fatalf("files = %s, want %s", encoded, want)
	}
}

func TestEncryptedBackupRoundTripAndWrongPassword(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte(`{"safe":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secrets", "token"), []byte("private-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	var encrypted bytes.Buffer
	if err := WriteEncryptedBackup(&encrypted, root, "a separate backup password"); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted.Bytes(), []byte("private-value")) {
		t.Fatal("backup is not encrypted")
	}
	plain, err := DecryptBackup(bytes.NewReader(encrypted.Bytes()), "a separate backup password")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(plain), int64(len(plain)))
	if err != nil || len(archive.File) != 2 {
		t.Fatalf("backup archive = %v / %v", archive, err)
	}
	if _, err := DecryptBackup(bytes.NewReader(encrypted.Bytes()), "wrong password"); err == nil {
		t.Fatal("wrong password decrypted backup")
	}
}

func TestDecryptBackupRejectsOversizedPlaintext(t *testing.T) {
	var encrypted bytes.Buffer
	recipient, err := age.NewScryptRecipient("a separate backup password")
	if err != nil {
		t.Fatal(err)
	}
	writer, err := age.Encrypt(&encrypted, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(writer, zeroReader{}, maximumBundleBytes+1); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptBackup(bytes.NewReader(encrypted.Bytes()), "a separate backup password"); err == nil {
		t.Fatal("oversized decrypted backup accepted")
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}

func TestFactoryResetRequiresExactPhraseAndSafeDataRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "unrelated-link")); err != nil {
		t.Fatal(err)
	}
	if err := FactoryReset(root, "wrong"); err == nil {
		t.Fatal("wrong confirmation accepted")
	}
	if err := FactoryReset(root, ResetConfirmation); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("state survived reset: %v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
		t.Fatalf("outside target changed: %q / %v", data, err)
	}

	realRoot := t.TempDir()
	link := filepath.Join(t.TempDir(), "data")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	if err := FactoryReset(link, ResetConfirmation); err == nil {
		t.Fatal("symlinked data root accepted")
	}
}
