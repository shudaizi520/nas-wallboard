package support

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"example.com/nas-wallboard/internal/model"
	"example.com/nas-wallboard/internal/persist"
	"filippo.io/age"
)

const (
	maximumBundleBytes = 16 << 20
	maximumBackupFile  = 2 << 20
	ResetConfirmation  = "删除 NAS WALLBOARD"
)

type BundleInput struct {
	Version     string
	GeneratedAt time.Time
	State       persist.State
	Runtime     model.Snapshot
}

type redactedIntegration struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config,omitempty"`
}

type redactedWidget struct {
	ID            string         `json:"id"`
	DefinitionID  string         `json:"definition_id"`
	IntegrationID string         `json:"integration_id,omitempty"`
	Enabled       bool           `json:"enabled"`
	Order         int            `json:"order"`
	Config        map[string]any `json:"config,omitempty"`
}

func WriteRedactedBundle(output io.Writer, input BundleInput) error {
	archive := zip.NewWriter(output)
	files := []struct {
		name  string
		value any
	}{
		{"overview.json", map[string]any{"version": input.Version, "generated_at": input.GeneratedAt.UTC()}},
		{"runtime.json", runtimeSummary(input.Runtime)},
		{"state-redacted.json", redactedState(input.State)},
	}
	for _, file := range files {
		data, err := json.MarshalIndent(file.value, "", "  ")
		if err != nil {
			_ = archive.Close()
			return err
		}
		if len(data) > maximumBundleBytes {
			_ = archive.Close()
			return errors.New("support bundle entry is too large")
		}
		writer, err := archive.Create(file.name)
		if err != nil {
			_ = archive.Close()
			return err
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			_ = archive.Close()
			return err
		}
	}
	return archive.Close()
}

func runtimeSummary(snapshot model.Snapshot) map[string]any {
	return map[string]any{
		"schema_version": snapshot.SchemaVersion,
		"version":        snapshot.Version,
		"snapshot_at":    snapshot.SnapshotAt,
		"connected":      snapshot.Connected,
		"modules": map[string]any{
			"system":   moduleSummary(snapshot.System.UpdatedAt, snapshot.System.Stale, snapshot.System.Error),
			"realtime": moduleSummary(snapshot.Realtime.UpdatedAt, snapshot.Realtime.Stale, snapshot.Realtime.Error),
			"pools":    moduleSummary(snapshot.Pools.UpdatedAt, snapshot.Pools.Stale, snapshot.Pools.Error),
			"apps":     moduleSummary(snapshot.Apps.UpdatedAt, snapshot.Apps.Stale, snapshot.Apps.Error),
			"alerts":   moduleSummary(snapshot.Alerts.UpdatedAt, snapshot.Alerts.Stale, snapshot.Alerts.Error),
		},
	}
}

func moduleSummary(updated time.Time, stale bool, message string) map[string]any {
	result := map[string]any{"updated_at": updated, "stale": stale}
	if message != "" {
		result["error"] = "collector unavailable"
	}
	return result
}

func redactedState(state persist.State) map[string]any {
	integrations := make([]redactedIntegration, 0, len(state.Integrations))
	for _, item := range state.Integrations {
		integrations = append(integrations, redactedIntegration{
			ID: item.ID, Type: item.Type, Enabled: item.Enabled, Config: redactMap(item.Config),
		})
	}
	widgets := make([]redactedWidget, 0, len(state.Widgets))
	for _, item := range state.Widgets {
		widgets = append(widgets, redactedWidget{
			ID: item.ID, DefinitionID: item.DefinitionID, IntegrationID: item.IntegrationID,
			Enabled: item.Enabled, Order: item.Order, Config: redactMap(item.Config),
		})
	}
	return map[string]any{
		"schema_version": state.SchemaVersion,
		"setup_complete": state.SetupComplete,
		"server":         state.Server,
		"integrations":   integrations,
		"widgets":        widgets,
		"migration":      map[string]any{"imported": state.Legacy != nil},
	}
}

func redactMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "api_key") || strings.Contains(lower, "cookie") || strings.Contains(lower, "serial") {
			result[key] = "[redacted]"
			continue
		}
		switch nested := value.(type) {
		case map[string]any:
			result[key] = redactMap(nested)
		default:
			result[key] = value
		}
	}
	return result
}

func WriteEncryptedBackup(output io.Writer, root, password string) error {
	if len([]rune(password)) < 12 {
		return errors.New("backup password must contain at least 12 characters")
	}
	root, err := validateDataRoot(root, true)
	if err != nil {
		return err
	}
	plain, err := buildBackupArchive(root)
	if err != nil {
		return err
	}
	recipient, err := age.NewScryptRecipient(password)
	if err != nil {
		return err
	}
	writer, err := age.Encrypt(output, recipient)
	if err != nil {
		return err
	}
	if _, err := writer.Write(plain); err != nil {
		return err
	}
	return writer.Close()
}

func DecryptBackup(input io.Reader, password string) ([]byte, error) {
	identity, err := age.NewScryptIdentity(password)
	if err != nil {
		return nil, err
	}
	identity.SetMaxWorkFactor(18)
	reader, err := age.Decrypt(input, identity)
	if err != nil {
		return nil, err
	}
	plain, err := io.ReadAll(io.LimitReader(reader, maximumBundleBytes+1))
	if err != nil {
		return nil, err
	}
	if len(plain) > maximumBundleBytes {
		return nil, errors.New("decrypted backup exceeds size limit")
	}
	return plain, nil
}

func buildBackupArchive(root string) ([]byte, error) {
	type entry struct{ absolute, relative string }
	entries := []entry{}
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if item.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if item.IsDir() {
			if strings.HasPrefix(item.Name(), ".wallboard-") {
				return filepath.SkipDir
			}
			return nil
		}
		if !item.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("unsafe backup path")
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		if info.Size() > maximumBackupFile {
			return fmt.Errorf("backup file %s is too large", relative)
		}
		entries = append(entries, entry{path, filepath.ToSlash(relative)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].relative < entries[j].relative })
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, item := range entries {
		data, err := persist.ReadLimited(item.absolute, maximumBackupFile)
		if err != nil {
			_ = archive.Close()
			return nil, err
		}
		writer, err := archive.Create(item.relative)
		if err != nil {
			_ = archive.Close()
			return nil, err
		}
		if _, err := writer.Write(data); err != nil {
			_ = archive.Close()
			return nil, err
		}
		if buffer.Len() > maximumBundleBytes {
			_ = archive.Close()
			return nil, errors.New("backup exceeds size limit")
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func FactoryReset(root, confirmation string) error {
	if confirmation != ResetConfirmation {
		return errors.New("reset confirmation does not match")
	}
	root, err := validateDataRoot(root, true)
	if err != nil {
		return err
	}
	tx, err := BeginReset(root)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func validateDataRoot(root string, requireState bool) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("data root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil || absolute == string(filepath.Separator) {
		return "", errors.New("unsafe data root")
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("data root must be a real directory")
	}
	if requireState {
		state, err := os.Lstat(filepath.Join(absolute, "state.json"))
		if err != nil || state.Mode()&os.ModeSymlink != 0 || !state.Mode().IsRegular() {
			return "", errors.New("data root does not contain Wallboard state")
		}
	}
	return absolute, nil
}
