package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"example.com/nas-wallboard/internal/auth"
	"example.com/nas-wallboard/internal/support"
)

func (s *server) manageSupport(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireManagement(w, request); !ok {
		return
	}
	var output bytes.Buffer
	if err := support.WriteRedactedBundle(&output, support.BundleInput{
		Version: s.version, GeneratedAt: s.clock(), State: s.configState.Snapshot(), Runtime: s.store.Snapshot(),
	}); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "support_unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="nas-wallboard-support.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output.Bytes())
}

func (s *server) manageReauthenticate(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.requireManagement(w, request)
	if !ok || !s.requireMutation(w, request, principal) {
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if decodeJSON(w, request, 16*1024, &input) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if !s.requireReauthenticationAttempt(w, request) {
		return
	}
	token, err := s.auth.Reauthenticate(principal.SessionID, input.Password)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	s.limiter.Reset(auth.ScopeReauth, requestRemoteIP(request))
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_in_seconds": 300})
}

func (s *server) manageBackup(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.requireManagement(w, request)
	if !ok || !s.requireMutation(w, request, principal) {
		return
	}
	if !s.auth.ConsumeReauthentication(principal.SessionID, request.Header.Get("X-Reauth-Token")) {
		writeAPIError(w, http.StatusForbidden, "reauth_required")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if decodeJSON(w, request, 16*1024, &input) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	var output bytes.Buffer
	if err := support.WriteEncryptedBackup(&output, s.dataRoot, input.Password); err != nil {
		writeAPIError(w, http.StatusBadRequest, "backup_failed")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="nas-wallboard-backup.age"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output.Bytes())
}

func (s *server) managePassword(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.requireManagement(w, request)
	if !ok || !s.requireMutation(w, request, principal) {
		return
	}
	var input struct {
		Current     string `json:"current"`
		Replacement string `json:"replacement"`
	}
	if decodeJSON(w, request, 16*1024, &input) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if !utf8.ValidString(input.Replacement) || utf8.RuneCountInString(input.Replacement) < 12 {
		writeAPIError(w, http.StatusBadRequest, "password_change_failed")
		return
	}
	if !s.requireReauthenticationAttempt(w, request) {
		return
	}
	if err := s.auth.ChangePassword(input.Current, input.Replacement); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeAPIError(w, http.StatusUnauthorized, "invalid_credentials")
			return
		}
		writeAPIError(w, http.StatusBadRequest, "password_change_failed")
		return
	}
	s.limiter.Reset(auth.ScopeReauth, requestRemoteIP(request))
	expireSessionCookie(w, request)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) manageUsername(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.requireManagement(w, request)
	if !ok || !s.requireMutation(w, request, principal) {
		return
	}
	var input struct {
		CurrentPassword string `json:"current_password"`
		Username        string `json:"username"`
	}
	if decodeJSON(w, request, 16*1024, &input) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if _, err := auth.ValidateUsername(input.Username); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_username")
		return
	}
	if !s.requireReauthenticationAttempt(w, request) {
		return
	}
	if err := s.auth.ChangeUsername(input.CurrentPassword, input.Username); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeAPIError(w, http.StatusUnauthorized, "invalid_credentials")
			return
		}
		writeAPIError(w, http.StatusBadRequest, "username_change_failed")
		return
	}
	s.limiter.Reset(auth.ScopeReauth, requestRemoteIP(request))
	expireSessionCookie(w, request)
	writeJSON(w, http.StatusOK, map[string]string{"username": s.auth.Username()})
}

func (s *server) requireReauthenticationAttempt(w http.ResponseWriter, request *http.Request) bool {
	if !s.limiter.Allow(auth.ScopeReauth, requestRemoteIP(request), s.clock()) {
		w.Header().Set("Retry-After", "600")
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited")
		return false
	}
	return true
}

func (s *server) manageReset(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.requireManagement(w, request)
	if !ok || !s.requireMutation(w, request, principal) {
		return
	}
	if !s.auth.ConsumeReauthentication(principal.SessionID, request.Header.Get("X-Reauth-Token")) {
		writeAPIError(w, http.StatusForbidden, "reauth_required")
		return
	}
	var input struct {
		Confirmation string `json:"confirmation"`
	}
	if decodeJSON(w, request, 16*1024, &input) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if input.Confirmation != support.ResetConfirmation {
		writeAPIError(w, http.StatusBadRequest, "reset_failed")
		return
	}
	before := s.configState.Snapshot().Integrations
	if s.integrationRuntime != nil {
		if err := s.integrationRuntime.Apply(request.Context(), before, nil); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "reset_failed")
			return
		}
	}
	tx, err := support.BeginReset(s.dataRoot)
	if err != nil {
		if tx != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				s.recoveryRequired = true
				writeAPIError(w, http.StatusServiceUnavailable, "reset_recovery_required")
				return
			}
			if reloadErr := s.reloadData(); reloadErr != nil {
				s.recoveryRequired = true
				writeAPIError(w, http.StatusServiceUnavailable, "reset_recovery_required")
				return
			}
		}
		if s.integrationRuntime != nil {
			_ = s.integrationRuntime.Apply(context.Background(), nil, before)
		}
		writeAPIError(w, http.StatusBadRequest, "reset_failed")
		return
	}
	err = s.reloadData()
	if err == nil {
		err = request.Context().Err()
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			s.recoveryRequired = true
			writeAPIError(w, http.StatusServiceUnavailable, "reset_recovery_required")
			return
		}
		if reloadErr := s.reloadData(); reloadErr != nil {
			s.recoveryRequired = true
			writeAPIError(w, http.StatusServiceUnavailable, "reset_recovery_required")
			return
		}
		if s.integrationRuntime != nil {
			_ = s.integrationRuntime.Apply(context.Background(), nil, before)
		}
		writeAPIError(w, http.StatusInternalServerError, "reset_failed")
		return
	}
	s.store.Reset()
	expireSessionCookie(w, request)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) manageUpdate(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireManagement(w, request); !ok {
		return
	}
	if s.updates == nil {
		writeJSON(w, http.StatusOK, map[string]any{"current": s.version, "available": false, "disabled": true})
		return
	}
	writeJSON(w, http.StatusOK, s.updates.Check(request.Context()))
}

func expireSessionCookie(w http.ResponseWriter, request *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Path: "/", HttpOnly: true, Secure: request.TLS != nil,
		SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0),
	})
}
