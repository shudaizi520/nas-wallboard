package support

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"example.com/nas-wallboard/internal/auth"
	"example.com/nas-wallboard/internal/persist"
)

const RestoreConfirmation = "恢复 NAS WALLBOARD"
const journalName = ".wallboard-restore.json"

var ownedData = []string{"state.json", "auth.json", "dashboard.json", "setup.pending.json", "secrets"}

// PrepareRestore extracts only application-owned regular files into a private
// staging directory. No live files are changed until all validation succeeds.
func PrepareRestore(root string, plain []byte) (stage string, err error) {
	root, err = validateDataRoot(root, true)
	if err != nil {
		return "", err
	}
	if len(plain) > maximumBundleBytes {
		return "", errors.New("backup too large")
	}
	archive, err := zip.NewReader(bytes.NewReader(plain), int64(len(plain)))
	if err != nil {
		return "", err
	}
	if len(archive.File) > maximumBackupFiles {
		return "", errors.New("too many backup files")
	}
	stage, err = os.MkdirTemp(root, ".wallboard-stage-")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(stage)
		}
	}()
	seen := map[string]bool{}
	total := uint64(0)
	for _, file := range archive.File {
		name := file.Name
		clean := path.Clean(name)
		if name != clean || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || seen[name] || !file.Mode().IsRegular() {
			return stage, errors.New("unsafe backup entry")
		}
		seen[name] = true
		if !allowedBackupPath(name) {
			return stage, errors.New("unexpected backup entry")
		}
		if file.UncompressedSize64 > maximumBackupFile || file.UncompressedSize64 > maximumBundleBytes-total {
			return stage, errors.New("backup exceeds size limit")
		}
		total += file.UncompressedSize64
		if strings.HasPrefix(name, "backups/") {
			continue
		} // keep current local migration backups
		destination := filepath.Join(stage, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return stage, err
		}
		reader, openErr := file.Open()
		if openErr != nil {
			return stage, openErr
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maximumBackupFile+1))
		_ = reader.Close()
		if readErr != nil || len(data) > maximumBackupFile {
			return stage, errors.New("invalid backup file")
		}
		if err = persist.WriteAtomic(destination, data, 0o600); err != nil {
			return stage, err
		}
	}
	if !seen["state.json"] {
		return stage, errors.New("backup state is missing")
	}
	store, err := persist.Open(stage)
	if err != nil {
		return stage, err
	}
	credentials, err := auth.Open(filepath.Join(stage, "auth.json"), nil)
	if err != nil {
		return stage, err
	}
	if !store.Snapshot().SetupComplete || !credentials.Configured() {
		return stage, errors.New("backup setup is incomplete")
	}
	secrets, err := persist.OpenSecrets(stage)
	if err != nil {
		return stage, err
	}
	for _, item := range store.Snapshot().Integrations {
		for _, ref := range item.SecretRefs {
			if _, err = secrets.Read(ref); err != nil {
				return stage, err
			}
		}
	}
	return stage, nil
}

type replacementJournal struct {
	Stage    string          `json:"stage"`
	Rollback string          `json:"rollback"`
	Original map[string]bool `json:"original"`
}
type DataReplacement struct {
	root    string
	journal replacementJournal
}

func BeginReset(root string) (*DataReplacement, error) {
	stage, err := os.MkdirTemp(root, ".wallboard-stage-")
	if err != nil {
		return nil, err
	}
	tx, err := BeginReplacement(root, stage, true)
	if err != nil && tx == nil {
		_ = os.RemoveAll(stage)
	}
	return tx, err
}

func BeginReplacement(root, stage string, reset bool) (*DataReplacement, error) {
	return beginReplacement(root, stage, reset, persist.WriteAtomic)
}

func beginReplacement(root, stage string, reset bool, writeJournal func(string, []byte, os.FileMode) error) (*DataReplacement, error) {
	root, err := validateDataRoot(root, true)
	if err != nil {
		return nil, err
	}
	if filepath.Dir(stage) != root || !safePrivateDir(root, filepath.Base(stage), ".wallboard-stage-") {
		return nil, errors.New("unsafe staging directory")
	}
	if _, err := os.Lstat(filepath.Join(root, journalName)); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("data replacement already pending")
	}
	rollback, err := os.MkdirTemp(root, ".wallboard-rollback-")
	if err != nil {
		return nil, err
	}
	tx := &DataReplacement{root: root, journal: replacementJournal{Stage: filepath.Base(stage), Rollback: filepath.Base(rollback), Original: map[string]bool{}}}
	names := append([]string(nil), ownedData...)
	if reset {
		names = append(names, "backups")
	}
	for _, name := range names {
		info, statErr := os.Lstat(filepath.Join(root, name))
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			_ = os.RemoveAll(rollback)
			return nil, statErr
		}
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			_ = os.RemoveAll(rollback)
			return nil, errors.New("unsafe live data")
		}
		tx.journal.Original[name] = statErr == nil
	}
	data, _ := json.Marshal(tx.journal)
	if err = writeJournal(filepath.Join(root, journalName), data, 0o600); err != nil {
		if _, statErr := os.Lstat(filepath.Join(root, journalName)); statErr == nil {
			return tx, err
		}
		_ = os.RemoveAll(rollback)
		return nil, err
	}
	for _, name := range names {
		if tx.journal.Original[name] {
			if err = os.Rename(filepath.Join(root, name), filepath.Join(rollback, name)); err != nil {
				if rollbackErr := tx.Rollback(); rollbackErr != nil {
					return tx, errors.Join(err, rollbackErr)
				}
				return nil, err
			}
		}
		if _, statErr := os.Lstat(filepath.Join(stage, name)); statErr == nil {
			if err = os.Rename(filepath.Join(stage, name), filepath.Join(root, name)); err != nil {
				if rollbackErr := tx.Rollback(); rollbackErr != nil {
					return tx, errors.Join(err, rollbackErr)
				}
				return nil, err
			}
		}
	}
	for _, directory := range []string{root, rollback, stage} {
		if err = syncDirectory(directory); err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				return tx, errors.Join(err, rollbackErr)
			}
			return nil, err
		}
	}
	return tx, nil
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}

func (tx *DataReplacement) Commit() error {
	if err := os.Remove(filepath.Join(tx.root, journalName)); err != nil {
		return err
	}
	if err := syncDirectory(tx.root); err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(tx.root, tx.journal.Rollback))
	_ = os.RemoveAll(filepath.Join(tx.root, tx.journal.Stage))
	return nil
}

func (tx *DataReplacement) Rollback() error {
	for name, original := range tx.journal.Original {
		backup := filepath.Join(tx.root, tx.journal.Rollback, name)
		target := filepath.Join(tx.root, name)
		if _, err := os.Lstat(backup); err == nil {
			if err = os.RemoveAll(target); err != nil {
				return err
			}
			if err = os.Rename(backup, target); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		} else if !original {
			if err = os.RemoveAll(target); err != nil {
				return err
			}
		}
	}
	if err := syncDirectory(tx.root); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Join(tx.root, tx.journal.Rollback)); err != nil {
		return err
	}
	return tx.Commit()
}

func safePrivateDir(root, name, prefix string) bool {
	if filepath.Base(name) != name || !strings.HasPrefix(name, prefix) {
		return false
	}
	info, err := os.Lstat(filepath.Join(root, name))
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

// A journal exists only while a replacement is uncommitted. Restart always
// recovers the old complete dataset before opening state or credentials.
func RecoverReplacement(root string) error {
	data, err := persist.ReadLimited(filepath.Join(root, journalName), 16*1024)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var j replacementJournal
	if json.Unmarshal(data, &j) != nil || !safePrivateDir(root, j.Stage, ".wallboard-stage-") || !safePrivateDir(root, j.Rollback, ".wallboard-rollback-") {
		return errors.New("invalid recovery journal")
	}
	allowed := map[string]bool{"backups": true}
	for _, name := range ownedData {
		allowed[name] = true
	}
	if len(j.Original) == 0 {
		return errors.New("empty recovery journal")
	}
	for name := range j.Original {
		if !allowed[name] {
			return errors.New("unsafe recovery target")
		}
	}
	return (&DataReplacement{root: root, journal: j}).Rollback()
}
