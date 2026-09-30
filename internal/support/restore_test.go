package support

import (
	"archive/zip"
	"bytes"
	"errors"
	"example.com/nas-wallboard/internal/persist"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreArchiveRejectsTraversalDuplicatesAndUnexpectedFiles(t *testing.T) {
	for _, names := range [][]string{{"../state.json"}, {"state.json", "state.json"}, {"config.yaml"}, {"secrets/../../auth.json"}} {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		for _, name := range names {
			w, _ := z.Create(name)
			_, _ = w.Write([]byte(`{}`))
		}
		_ = z.Close()
		root := t.TempDir()
		_, _ = persist.Open(root)
		if stage, err := PrepareRestore(root, b.Bytes()); err == nil {
			_ = os.RemoveAll(stage)
			t.Fatalf("accepted %v", names)
		}
	}
}

func TestInterruptedReplacementRollsBackOnStartup(t *testing.T) {
	root := t.TempDir()
	original, _ := persist.Open(root)
	_ = original.Update(func(s *persist.State) error { s.SetupComplete = true; return nil })
	stage, err := os.MkdirTemp(root, ".wallboard-stage-")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = persist.Open(stage)
	tx, err := BeginReplacement(root, stage, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx // simulate process interruption before Commit
	if err := RecoverReplacement(root); err != nil {
		t.Fatal(err)
	}
	loaded, err := persist.Open(root)
	if err != nil || !loaded.Snapshot().SetupComplete {
		t.Fatal("original configuration was not recovered")
	}
	if _, err := os.Stat(filepath.Join(root, ".wallboard-restore.json")); !os.IsNotExist(err) {
		t.Fatal("journal survived recovery")
	}
}

func TestFailedRollbackRetainsRecoveryData(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires permission denial")
	}
	root := t.TempDir()
	original, _ := persist.Open(root)
	_ = original.Update(func(s *persist.State) error { s.SetupComplete = true; return nil })
	stage, _ := os.MkdirTemp(root, ".wallboard-stage-")
	_, _ = persist.Open(stage)
	tx, err := BeginReplacement(root, stage, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	failure := tx.Rollback()
	_ = os.Chmod(root, 0o700)
	if failure == nil {
		t.Fatal("rollback unexpectedly succeeded without write permission")
	}
	if err = RecoverReplacement(root); err != nil {
		t.Fatal(err)
	}
	restored, err := persist.Open(root)
	if err != nil || !restored.Snapshot().SetupComplete {
		t.Fatal("failure destroyed recovery data")
	}
}

func TestJournalSyncFailurePreservesRecoveryDirectories(t *testing.T) {
	root := t.TempDir()
	original, _ := persist.Open(root)
	_ = original.Update(func(s *persist.State) error { s.SetupComplete = true; return nil })
	stage, _ := os.MkdirTemp(root, ".wallboard-stage-")
	_, _ = persist.Open(stage)
	tx, err := beginReplacement(root, stage, false, func(path string, data []byte, mode os.FileMode) error {
		if err := persist.WriteAtomic(path, data, mode); err != nil {
			return err
		}
		return errors.New("injected directory sync failure after journal installation")
	})
	if err == nil || tx == nil {
		t.Fatal("installed journal lost its transaction")
	}
	if err = RecoverReplacement(root); err != nil {
		t.Fatal(err)
	}
	restored, err := persist.Open(root)
	if err != nil || !restored.Snapshot().SetupComplete {
		t.Fatal("journal failure destroyed original data")
	}
}
