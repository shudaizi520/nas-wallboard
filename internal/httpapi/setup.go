package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"example.com/nas-wallboard/internal/auth"
	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/persist"
	"example.com/nas-wallboard/internal/truenas"
	"example.com/nas-wallboard/internal/widget"
)

const setupJournalName = "setup.pending.json"

type setupInput struct {
	URL                   string `json:"url"`
	Username              string `json:"username"`
	APIKey                string `json:"api_key"`
	InsecureSkipVerify    bool   `json:"insecure_skip_verify"`
	AdministratorUsername string `json:"administrator_username"`
	Password              string `json:"password"`
	PasswordConfirmation  string `json:"password_confirmation"`
	UseImported           bool   `json:"use_imported"`
}

type setupJournal struct {
	Version   int               `json:"version"`
	SecretRef persist.SecretRef `json:"secret_ref"`
	CreatedAt time.Time         `json:"created_at"`
}

func (s *server) setupStatus(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.configState == nil || s.auth == nil || !s.requireTrustedLAN(w, request, false) {
		return
	}
	state := s.configState.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"setup_required": !state.SetupComplete || !s.auth.Configured(),
		"migration":      state.Legacy != nil && !state.SetupComplete,
		"configured":     state.SetupComplete && s.auth.Configured(),
	})
}

func (s *server) setupProbeHandler(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.requireSetupMutation(w, request, auth.ScopeProbe, 60) {
		return
	}
	var input setupInput
	if err := decodeJSON(w, request, 32*1024, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	var result truenas.SetupProbeResult
	var err error
	if input.UseImported {
		candidate, apiKey, _, candidateErr := s.setupCandidate(input)
		if candidateErr != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_candidate")
			return
		}
		result, err = s.setupProbe(request.Context(), candidate, apiKey)
	} else {
		result, err = s.runSetupProbe(request.Context(), input)
	}
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "probe_failed")
		return
	}
	status := http.StatusOK
	if !result.OK {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, result)
}

func (s *server) setupComplete(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.requireSetupMutation(w, request, auth.ScopeSetup, 600) {
		return
	}
	var input setupInput
	if err := decodeJSON(w, request, 32*1024, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if input.Password != input.PasswordConfirmation || input.Password == "" {
		writeAPIError(w, http.StatusBadRequest, "password_confirmation")
		return
	}
	administratorUsername, err := auth.ValidateUsername(input.AdministratorUsername)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_username")
		return
	}
	if _, err := auth.HashPassword([]byte(input.Password)); err != nil {
		writeAPIError(w, http.StatusBadRequest, "password_policy")
		return
	}

	candidate, apiKey, currentRef, err := s.setupCandidate(input)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_candidate")
		return
	}
	result, err := s.setupProbe(request.Context(), candidate, apiKey)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "probe_failed")
		return
	}
	if !result.OK {
		writeJSON(w, http.StatusUnprocessableEntity, result)
		return
	}

	ref := currentRef
	discard := func() error { return nil }
	if !input.UseImported {
		ref, discard, err = s.secrets.Stage("truenas-main", "api_key", []byte(apiKey))
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "save_failed")
			return
		}
	}
	committed := false
	defer func() {
		if !committed {
			_ = discard()
		}
	}()

	journalPath := filepath.Join(s.dataRoot, setupJournalName)
	journalBytes, _ := json.Marshal(setupJournal{Version: 1, SecretRef: ref, CreatedAt: s.clock().UTC()})
	if err := persist.WriteAtomic(journalPath, append(journalBytes, '\n'), 0o600); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "save_failed")
		return
	}
	if err := s.auth.SetInitialCredentials(administratorUsername, input.Password); err != nil {
		_ = os.Remove(journalPath)
		writeAPIError(w, http.StatusConflict, "administrator_exists")
		return
	}
	beforeIntegrations := s.configState.Snapshot().Integrations
	if err := s.configState.Update(func(state *persist.State) error {
		state.SetupComplete = true
		if !input.UseImported {
			upsertTrueNAS(state, candidate, ref)
		}
		if state.Server.Width < config.DashboardMinWidth || state.Server.Width > config.DashboardMaxWidth {
			state.Server.Width = config.DashboardDefaultWidth
		}
		if len(state.Widgets) == 0 {
			state.Widgets = widget.DefaultTrueNASWidgets("truenas-main")
		}
		return nil
	}); err != nil {
		_ = s.auth.RollbackInitialPassword()
		_ = os.Remove(journalPath)
		writeAPIError(w, http.StatusInternalServerError, "save_failed")
		return
	}
	committed = true
	_ = os.Remove(journalPath)
	afterState := s.configState.Snapshot()
	_ = s.secrets.Collect(stateSecretRefs(afterState))
	if s.widgets != nil && s.manager != nil && s.builder != nil {
		if configured, configureErr := s.widgets.DashboardConfig(s.manager.Config()); configureErr == nil {
			_ = s.builder.Update(configured)
		}
	}
	runtimeStarted := true
	if s.integrationRuntime != nil {
		runtimeStarted = s.integrationRuntime.Apply(request.Context(), beforeIntegrations, afterState.Integrations) == nil
	}
	writeJSON(w, http.StatusCreated, map[string]any{"configured": true, "probe": result, "runtime_started": runtimeStarted})
}

func (s *server) requireSetupMutation(w http.ResponseWriter, request *http.Request, scope string, retryAfter int) bool {
	if s.configState == nil || s.secrets == nil || s.auth == nil || !s.requireTrustedLAN(w, request, true) {
		return false
	}
	if s.configState.Snapshot().SetupComplete || s.auth.Configured() {
		writeAPIError(w, http.StatusConflict, "already_configured")
		return false
	}
	if !s.limiter.Allow(scope, requestRemoteIP(request), s.clock()) {
		w.Header().Set("Retry-After", fmt.Sprint(retryAfter))
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited")
		return false
	}
	return true
}

func (s *server) runSetupProbe(ctx context.Context, input setupInput) (truenas.SetupProbeResult, error) {
	cfg := config.TrueNASConfig{
		URL: input.URL, Username: input.Username, InsecureSkipVerify: input.InsecureSkipVerify,
		CallTimeout: config.Duration{Duration: 10 * time.Second},
	}
	return s.setupProbe(ctx, cfg, input.APIKey)
}

func (s *server) setupCandidate(input setupInput) (config.TrueNASConfig, string, persist.SecretRef, error) {
	if !input.UseImported {
		cfg := config.TrueNASConfig{
			URL: input.URL, Username: input.Username, InsecureSkipVerify: input.InsecureSkipVerify,
			CallTimeout: config.Duration{Duration: 10 * time.Second},
		}
		if input.URL == "" || input.Username == "" || input.APIKey == "" {
			return config.TrueNASConfig{}, "", persist.SecretRef{}, errors.New("candidate is incomplete")
		}
		return cfg, input.APIKey, persist.SecretRef{}, nil
	}
	for _, integration := range s.configState.Snapshot().Integrations {
		if integration.Type != "truenas" {
			continue
		}
		ref, ok := integration.SecretRefs["api_key"]
		if !ok {
			break
		}
		value, err := s.secrets.Read(ref)
		if err != nil {
			return config.TrueNASConfig{}, "", persist.SecretRef{}, err
		}
		cfg := config.TrueNASConfig{
			URL: stringConfig(integration.Config, "url"), Username: stringConfig(integration.Config, "username"),
			InsecureSkipVerify: boolConfig(integration.Config, "insecure_skip_verify"),
			CallTimeout:        config.Duration{Duration: 10 * time.Second},
		}
		return cfg, string(value), ref, nil
	}
	return config.TrueNASConfig{}, "", persist.SecretRef{}, errors.New("imported TrueNAS configuration not found")
}

func upsertTrueNAS(state *persist.State, cfg config.TrueNASConfig, ref persist.SecretRef) {
	integration := persist.Integration{
		ID: "truenas-main", Type: "truenas", Enabled: true,
		Config: map[string]any{
			"url": cfg.URL, "username": cfg.Username, "insecure_skip_verify": cfg.InsecureSkipVerify,
			"call_timeout": cfg.CallTimeout.String(),
		},
		SecretRefs: map[string]persist.SecretRef{"api_key": ref},
	}
	for index := range state.Integrations {
		if state.Integrations[index].Type == "truenas" {
			state.Integrations[index] = integration
			return
		}
	}
	state.Integrations = append(state.Integrations, integration)
}

func RecoverPendingSetup(dataRoot string, state *persist.Store, secrets *persist.SecretStore) error {
	journalPath := filepath.Join(dataRoot, setupJournalName)
	data, err := persist.ReadLimited(journalPath, 16*1024)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read setup journal: %w", err)
	}
	var journal setupJournal
	if err := json.Unmarshal(data, &journal); err != nil || journal.Version != 1 {
		return errors.New("setup journal is invalid")
	}
	current := state.Snapshot()
	if !current.SetupComplete {
		if err := os.Remove(filepath.Join(dataRoot, "auth.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := secrets.Collect(stateSecretRefs(current)); err != nil {
		return err
	}
	return os.Remove(journalPath)
}

func stateSecretRefs(state persist.State) map[persist.SecretRef]struct{} {
	result := map[persist.SecretRef]struct{}{}
	for _, integration := range state.Integrations {
		for _, ref := range integration.SecretRefs {
			result[ref] = struct{}{}
		}
	}
	return result
}

func stringConfig(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func boolConfig(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}
