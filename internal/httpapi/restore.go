package httpapi

import (
	"context"
	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/dashboard"
	"example.com/nas-wallboard/internal/persist"
	"example.com/nas-wallboard/internal/support"
	"net/http"
	"os"
	"path/filepath"
)

func (s *server) reloadData() error {
	if err := s.configState.Reload(); err != nil {
		return err
	}
	if err := s.auth.Reload(); err != nil {
		return err
	}
	_, err := persist.OpenSecrets(s.dataRoot)
	return err
}

func (s *server) manageRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.requireManagement(w, r)
	if !ok || !s.requireMutation(w, r, principal) {
		return
	}
	if !s.auth.ConsumeReauthentication(principal.SessionID, r.Header.Get("X-Reauth-Token")) {
		writeAPIError(w, http.StatusForbidden, "reauth_required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_backup")
		return
	}
	defer r.MultipartForm.RemoveAll()
	if r.FormValue("confirmation") != support.RestoreConfirmation {
		writeAPIError(w, http.StatusBadRequest, "confirmation_required")
		return
	}
	file, _, err := r.FormFile("backup")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_backup")
		return
	}
	defer file.Close()
	plain, err := support.DecryptBackup(file, r.FormValue("password"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_backup")
		return
	}
	stage, err := support.PrepareRestore(s.dataRoot, plain)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_backup")
		return
	}
	defer func() {
		if _, statErr := os.Lstat(filepath.Join(s.dataRoot, ".wallboard-restore.json")); os.IsNotExist(statErr) {
			_ = os.RemoveAll(stage)
		}
	}()
	prepared, err := persist.Open(stage)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_backup")
		return
	}
	secrets, err := persist.OpenSecrets(stage)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_backup")
		return
	}
	if s.integrations != nil {
		if err = s.integrations.ValidateRestored(prepared, secrets); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_backup")
			return
		}
	}
	layout := config.DashboardConfig{Metrics: []config.MetricConfig{}, Activities: []config.ActivityConfig{}}
	if s.widgets != nil {
		layout, err = s.widgets.ValidateRestored(prepared)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_backup")
			return
		}
	}
	if _, err = dashboard.New(layout); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_backup")
		return
	}
	before := s.configState.Snapshot().Integrations
	if r.Context().Err() != nil {
		writeAPIError(w, http.StatusRequestTimeout, "restore_cancelled")
		return
	}
	if s.integrationRuntime != nil {
		if err = s.integrationRuntime.Apply(r.Context(), before, nil); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "restore_failed")
			return
		}
	}
	if r.Context().Err() != nil {
		if s.integrationRuntime != nil {
			_ = s.integrationRuntime.Apply(context.Background(), nil, before)
		}
		writeAPIError(w, http.StatusRequestTimeout, "restore_cancelled")
		return
	}
	tx, err := support.BeginReplacement(s.dataRoot, stage, false)
	if err == nil {
		err = s.reloadData()
	}
	if err == nil && s.builder != nil {
		err = s.builder.Update(layout)
	}
	if err == nil && s.integrationRuntime != nil {
		s.store.Reset()
		err = s.integrationRuntime.Apply(r.Context(), nil, s.configState.Snapshot().Integrations)
	}
	if err == nil {
		err = r.Context().Err()
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		if s.integrationRuntime != nil {
			_ = s.integrationRuntime.Apply(r.Context(), nil, nil)
		}
		if tx != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				s.recoveryRequired = true
				writeAPIError(w, http.StatusServiceUnavailable, "restore_recovery_required")
				return
			}
			if reloadErr := s.reloadData(); reloadErr != nil {
				s.recoveryRequired = true
				writeAPIError(w, http.StatusServiceUnavailable, "restore_recovery_required")
				return
			}
		}
		if s.widgets != nil && s.builder != nil {
			if old, loadErr := s.widgets.DashboardConfig(config.DashboardConfig{}); loadErr == nil {
				_ = s.builder.Update(old)
			}
		}
		if s.integrationRuntime != nil {
			_ = s.integrationRuntime.Apply(context.Background(), nil, before)
		}
		writeAPIError(w, http.StatusInternalServerError, "restore_failed")
		return
	}
	if s.integrationRuntime == nil {
		s.store.Reset()
	}
	expireSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}
