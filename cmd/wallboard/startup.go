package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"example.com/nas-wallboard/internal/migrate"
	"example.com/nas-wallboard/internal/persist"
	"example.com/nas-wallboard/internal/support"
)

type startupMode string

const (
	startupLegacy    startupMode = "legacy"
	startupMigration startupMode = "migration"
	startupPublic    startupMode = "public"
)

type startupPaths struct {
	DataRoot            string
	Config              string
	TrueNASSecret       string
	Dashboard           string
	HomeAssistantSecret string
}

type startupSelection struct {
	Mode        startupMode
	State       *persist.Store
	Secrets     *persist.SecretStore
	Import      migrate.Result
	ImportError string
}

func selectStartup(ctx context.Context, paths startupPaths) (startupSelection, error) {
	if err := support.RecoverReplacement(paths.DataRoot); err != nil {
		return startupSelection{}, err
	}
	statePath := filepath.Join(paths.DataRoot, "state.json")
	_, stateErr := os.Lstat(statePath)
	stateExists := stateErr == nil
	if stateErr != nil && !errors.Is(stateErr, os.ErrNotExist) {
		return startupSelection{}, stateErr
	}

	store, err := persist.Open(paths.DataRoot)
	if err != nil {
		return startupSelection{}, err
	}
	secrets, err := persist.OpenSecrets(paths.DataRoot)
	if err != nil {
		if !stateExists {
			_ = os.Remove(statePath)
		}
		return startupSelection{}, err
	}
	if stateExists {
		mode := startupMigration
		if store.Snapshot().SetupComplete {
			mode = startupPublic
		}
		return startupSelection{Mode: mode, State: store, Secrets: secrets}, nil
	}
	if _, err := os.Stat(paths.Config); errors.Is(err, os.ErrNotExist) {
		return startupSelection{Mode: startupMigration, State: store, Secrets: secrets}, nil
	} else if err != nil {
		return startupSelection{}, err
	}

	result, err := migrate.ImportLegacy(ctx, migrate.LegacyPaths{
		Config:              paths.Config,
		TrueNASSecret:       paths.TrueNASSecret,
		Dashboard:           paths.Dashboard,
		HomeAssistantSecret: paths.HomeAssistantSecret,
	}, store, secrets)
	if err == nil {
		return startupSelection{Mode: startupMigration, State: store, Secrets: secrets, Import: result}, nil
	}
	// The legacy files are the rollback source of truth. Remove only state and
	// empty secret directories created by this failed import attempt.
	_ = secrets.Collect(map[persist.SecretRef]struct{}{})
	_ = os.Remove(filepath.Join(paths.DataRoot, "secrets"))
	_ = os.Remove(statePath)
	_ = os.Remove(paths.DataRoot)
	return startupSelection{
		Mode:        startupLegacy,
		ImportError: "旧版配置导入失败；已继续使用原配置",
	}, nil
}
