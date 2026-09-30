package support

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"example.com/nas-wallboard/internal/auth"
	"example.com/nas-wallboard/internal/persist"
)

func configuredBackupRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	store, err := persist.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Update(func(s *persist.State) error { s.SetupComplete = true; return nil }); err != nil {
		t.Fatal(err)
	}
	credentials, err := auth.Open(filepath.Join(root, "auth.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = credentials.SetInitialCredentials("admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestBackupIgnoresAtomicResidueAndUnownedFilesAndPreparesRestore(t *testing.T) {
	root := configuredBackupRoot(t)
	store, err := persist.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := persist.OpenSecrets(root)
	if err != nil {
		t.Fatal(err)
	}
	ref, _, err := secrets.Stage("source", "api_key", []byte("fixture-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(s *persist.State) error {
		s.Integrations = []persist.Integration{{ID: "source", Type: "truenas", SecretRefs: map[string]persist.SecretRef{"api_key": ref}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".state-123.tmp", "setup.pending.json", "notes.txt", "secrets/.token-123.tmp", "backups/.state-456.tmp", ".wallboard-stage-left/state.json"} {
		target := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("residue"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var encrypted bytes.Buffer
	if err := WriteEncryptedBackup(&encrypted, root, "separate backup password"); err != nil {
		t.Fatal(err)
	}
	plain, err := DecryptBackup(bytes.NewReader(encrypted.Bytes()), "separate backup password")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(plain), int64(len(plain)))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 3 {
		t.Fatalf("backup included residue: got %d entries, want state, auth and referenced secret", len(archive.File))
	}
	stage, err := PrepareRestore(root, plain)
	if err != nil {
		t.Fatalf("own successful backup cannot prepare restore: %v", err)
	}
	defer os.RemoveAll(stage)
}

func TestBackupCountAndUncompressedLimitsAcceptExactBoundary(t *testing.T) {
	for _, kind := range []string{"count", "total"} {
		t.Run(kind, func(t *testing.T) {
			root := configuredBackupRoot(t)
			if err := os.Mkdir(filepath.Join(root, "backups"), 0700); err != nil {
				t.Fatal(err)
			}
			count, remaining := 2046, 2046
			if kind == "total" {
				count, remaining = 8, 16<<20
				for _, name := range []string{"state.json", "auth.json"} {
					data, err := os.ReadFile(filepath.Join(root, name))
					if err != nil {
						t.Fatal(err)
					}
					remaining -= len(data)
				}
			}
			for i := 0; i < count; i++ {
				size := 1
				if kind == "total" {
					size = 2 << 20
					if size > remaining {
						size = remaining
					}
				}
				if err := os.WriteFile(filepath.Join(root, "backups", fmt.Sprintf("entry-%d", i)), bytes.Repeat([]byte("x"), size), 0600); err != nil {
					t.Fatal(err)
				}
				remaining -= size
			}
			plain, err := buildBackupArchive(root)
			if err != nil {
				t.Fatalf("exact %s boundary rejected: %v", kind, err)
			}
			stage, err := PrepareRestore(root, plain)
			if err != nil {
				t.Fatalf("exact %s boundary cannot restore: %v", kind, err)
			}
			defer os.RemoveAll(stage)
		})
	}
}

func TestBackupRejectsZIPOverheadBeyondArchiveLimit(t *testing.T) {
	root := configuredBackupRoot(t)
	remaining := 16 << 20
	for _, name := range []string{"state.json", "auth.json"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		remaining -= len(data)
	}
	if err := os.Mkdir(filepath.Join(root, "backups"), 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; remaining > 0; i++ {
		size := 2 << 20
		if size > remaining {
			size = remaining
		}
		data := make([]byte, size)
		if _, err := rand.Read(data); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "backups", fmt.Sprintf("entry-%d", i)), data, 0600); err != nil {
			t.Fatal(err)
		}
		remaining -= size
	}
	if _, err := buildBackupArchive(root); err == nil {
		t.Fatal("incompressible data at total boundary produced ZIP beyond 16MiB")
	}
}

func TestBackupRejectsCountAndUncompressedTotalLimits(t *testing.T) {
	for _, kind := range []string{"count", "uncompressed total", "single file"} {
		t.Run(kind, func(t *testing.T) {
			root := configuredBackupRoot(t)
			if err := os.Mkdir(filepath.Join(root, "secrets"), 0700); err != nil {
				t.Fatal(err)
			}
			count, size := 2047, 1 // state + auth + secrets exceeds 2048.
			if kind == "uncompressed total" {
				count, size = 8, 2<<20
			}
			if kind == "single file" {
				count, size = 1, (2<<20)+1
			}
			data := bytes.Repeat([]byte("x"), size)
			for i := 0; i < count; i++ {
				if err := os.WriteFile(filepath.Join(root, "secrets", fmt.Sprintf("entry-%d", i)), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := buildBackupArchive(root); err == nil {
				t.Fatalf("export accepted %s beyond restore limits", kind)
			}
		})
	}
}

func TestRestoreRejectsAtomicResidueAndSharedLimits(t *testing.T) {
	for _, kind := range []string{"temporary", "count", "total", "file", "archive"} {
		t.Run(kind, func(t *testing.T) {
			root := configuredBackupRoot(t)
			plain, err := buildBackupArchive(root)
			if err != nil {
				t.Fatal(err)
			}
			original, err := zip.NewReader(bytes.NewReader(plain), int64(len(plain)))
			if err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			writer := zip.NewWriter(&b)
			for _, file := range original.File {
				if err := writer.Copy(file); err != nil {
					t.Fatal(err)
				}
			}
			count, size := 1, 1
			switch kind {
			case "count":
				count = 2047
			case "total":
				count, size = 8, 2<<20
			case "file":
				size = (2 << 20) + 1
			case "archive":
				count, size = 8, 2<<20
			}
			for i := 0; i < count; i++ {
				name := fmt.Sprintf("secrets/entry-%d", i)
				if kind == "temporary" {
					name = "secrets/.token-123.tmp"
				}
				header := &zip.FileHeader{Name: name, Method: zip.Deflate}
				if kind == "archive" {
					header.Method = zip.Store
				}
				entry, err := writer.CreateHeader(header)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write(bytes.Repeat([]byte("x"), size)); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if stage, err := PrepareRestore(root, b.Bytes()); err == nil {
				os.RemoveAll(stage)
				t.Fatalf("restore accepted %s", kind)
			}
		})
	}
}
