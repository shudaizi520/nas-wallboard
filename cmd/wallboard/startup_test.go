package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"example.com/nas-wallboard/internal/persist"
)

func TestSelectStartupUsesPublicStateWhenStateExists(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	store, err := persist.Open(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(state *persist.State) error {
		state.SetupComplete = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	selection, err := selectStartup(context.Background(), startupPaths{DataRoot: dataRoot})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Mode != startupPublic || !selection.State.Snapshot().SetupComplete {
		t.Fatalf("selection = %#v", selection)
	}
}

func TestSelectStartupImportsLegacyIntoMigrationMode(t *testing.T) {
	root := t.TempDir()
	configPath := writeStartupFixture(t, root, "config.yaml", `truenas:
  url: wss://nas.example.invalid/api/current
  username: nas_wallboard
`, 0o600)
	secretPath := writeStartupFixture(t, root, "truenas_api_key", "true-secret", 0o600)

	selection, err := selectStartup(context.Background(), startupPaths{
		DataRoot: filepath.Join(root, "data"), Config: configPath, TrueNASSecret: secretPath,
		Dashboard: filepath.Join(root, "missing-dashboard.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Mode != startupMigration || !selection.Import.Imported || !selection.Import.NeedsAdmin {
		t.Fatalf("selection = %#v", selection)
	}
	if got := selection.State.Snapshot(); got.SetupComplete || len(got.Integrations) != 1 {
		t.Fatalf("migrated state = %#v", got)
	}
}

func TestSelectStartupCreatesFreshSetupWhenLegacyFilesDoNotExist(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	selection, err := selectStartup(context.Background(), startupPaths{
		DataRoot: dataRoot, Config: filepath.Join(root, "missing-config.yaml"),
		TrueNASSecret: filepath.Join(root, "missing-secret"), Dashboard: filepath.Join(root, "missing-dashboard.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Mode != startupMigration || selection.State == nil || selection.Secrets == nil || selection.Import.Imported {
		t.Fatalf("selection = %#v", selection)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "state.json")); err != nil {
		t.Fatalf("fresh state missing: %v", err)
	}
}

func TestSelectStartupFallsBackAndRemovesNewStateAfterImportFailure(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	configPath := writeStartupFixture(t, root, "config.yaml", `truenas:
  url: wss://nas.example.invalid/api/current
  username: nas_wallboard
`, 0o600)
	secretPath := writeStartupFixture(t, root, "truenas_api_key", "true-secret", 0o600)
	dashboardPath := writeStartupFixture(t, root, "dashboard.json", `{"width":`, 0o600)

	selection, err := selectStartup(context.Background(), startupPaths{
		DataRoot: dataRoot, Config: configPath, TrueNASSecret: secretPath, Dashboard: dashboardPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Mode != startupLegacy || selection.ImportError == "" {
		t.Fatalf("selection = %#v", selection)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("new state survived failed import: %v", err)
	}
	if data, err := os.ReadFile(dashboardPath); err != nil || string(data) != `{"width":` {
		t.Fatalf("legacy dashboard changed: %q / %v", data, err)
	}
}

func TestSelectStartupImportErrorDoesNotExposeSecret(t *testing.T) {
	root := t.TempDir()
	secret := "must-never-escape"
	configPath := writeStartupFixture(t, root, "config.yaml", `truenas:
  url: wss://nas.example.invalid/api/current
  username: nas_wallboard
unexpected: `+secret+"\n", 0o600)
	secretPath := writeStartupFixture(t, root, "truenas_api_key", secret, 0o600)

	selection, err := selectStartup(context.Background(), startupPaths{
		DataRoot: filepath.Join(root, "data"), Config: configPath, TrueNASSecret: secretPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Mode != startupLegacy || strings.Contains(selection.ImportError, secret) {
		t.Fatalf("selection leaked secret: %#v", selection)
	}
}

func writeStartupFixture(t *testing.T, directory, name, value string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(value), mode); err != nil {
		t.Fatal(err)
	}
	return path
}
