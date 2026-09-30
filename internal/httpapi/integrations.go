package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"example.com/nas-wallboard/internal/auth"
	"example.com/nas-wallboard/internal/integration"
)

const maxIntegrationRequestBytes = 64 * 1024

type integrationInput struct {
	InstanceID string            `json:"instance_id,omitempty"`
	Type       string            `json:"type"`
	Config     map[string]any    `json:"config"`
	Secrets    map[string]string `json:"secrets"`
}

func (input integrationInput) candidate() integration.Candidate {
	secrets := integration.Secrets{}
	for key, value := range input.Secrets {
		secrets[key] = []byte(value)
	}
	return integration.Candidate{Type: input.Type, Config: integration.Config(input.Config), Secrets: secrets}
}

func (s *server) manageIntegrations(w http.ResponseWriter, request *http.Request) {
	if s.integrations == nil {
		http.NotFound(w, request)
		return
	}
	principal, ok := s.requireManagement(w, request)
	if !ok {
		return
	}
	relative := strings.TrimPrefix(request.URL.Path, "/api/manage/integrations")
	parts := splitPath(relative)
	if request.Method != http.MethodGet {
		if !s.requireMutation(w, request, principal) {
			return
		}
		if !s.limiter.Allow(auth.ScopeIntegration, requestRemoteIP(request), s.clock()) {
			w.Header().Set("Retry-After", "60")
			writeAPIError(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
	}
	switch {
	case request.Method == http.MethodPost && len(parts) == 1 && parts[0] == "entities":
		var input struct {
			Type       string            `json:"type"`
			Config     map[string]any    `json:"config"`
			Secrets    map[string]string `json:"secrets"`
			InstanceID string            `json:"instance_id"`
		}
		if decodeJSON(w, request, maxIntegrationRequestBytes, &input) != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		candidate := integrationInput{Type: input.Type, Config: input.Config, Secrets: input.Secrets}.candidate()
		entities, err := s.integrations.DiscoverEntities(request.Context(), candidate, input.InstanceID)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "entity_discovery_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entities": entities})
	case request.Method == http.MethodGet && len(parts) == 0:
		s.manageIntegrationCatalog(w)
	case request.Method == http.MethodPost && len(parts) == 0:
		input, valid := decodeIntegrationInput(w, request)
		if !valid {
			return
		}
		instance, probe, err := s.integrations.Create(request.Context(), input.candidate())
		if errors.Is(err, integration.ErrProbeFailed) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "probe_failed", "probe": probe})
			return
		}
		if err != nil {
			writeIntegrationError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"instance": instance, "probe": probe})
	case request.Method == http.MethodPost && len(parts) == 1 && parts[0] == "probe":
		input, valid := decodeIntegrationInput(w, request)
		if !valid {
			return
		}
		probe, err := s.integrations.TestCandidate(request.Context(), input.candidate(), input.InstanceID)
		if err != nil && !errors.Is(err, integration.ErrProbeFailed) {
			writeIntegrationError(w, err)
			return
		}
		status := http.StatusOK
		if err != nil {
			status = http.StatusUnprocessableEntity
		}
		if err != nil {
			writeJSON(w, status, map[string]any{"error": "probe_failed", "probe": probe})
		} else {
			writeJSON(w, status, probe)
		}
	case request.Method == http.MethodPut && len(parts) == 1:
		input, valid := decodeIntegrationInput(w, request)
		if !valid {
			return
		}
		instance, probe, err := s.integrations.Update(request.Context(), parts[0], input.candidate())
		if errors.Is(err, integration.ErrProbeFailed) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "probe_failed", "probe": probe})
			return
		}
		if err != nil {
			writeIntegrationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"instance": instance, "probe": probe})
	case request.Method == http.MethodPost && len(parts) == 2 && (parts[1] == "enable" || parts[1] == "disable"):
		var err error
		if parts[1] == "enable" {
			err = s.integrations.Enable(request.Context(), parts[0])
		} else {
			err = s.integrations.Disable(request.Context(), parts[0])
		}
		if err != nil {
			writeIntegrationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": parts[1] == "enable"})
	case request.Method == http.MethodDelete && len(parts) == 1:
		var confirmation struct {
			Confirm string `json:"confirm"`
		}
		if err := decodeJSON(w, request, maxIntegrationRequestBytes, &confirmation); err != nil || confirmation.Confirm != parts[0] {
			writeAPIError(w, http.StatusBadRequest, "confirmation_required")
			return
		}
		if err := s.integrations.Remove(request.Context(), parts[0]); err != nil {
			writeIntegrationError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) manageIntegrationCatalog(w http.ResponseWriter) {
	discovery := integration.Discovery{}
	if s.store != nil {
		for _, app := range s.store.Snapshot().Apps.Data {
			discovery.Apps = append(discovery.Apps, integration.DiscoveredApp{ID: app.ID, Name: app.Name})
		}
	}
	health := []integration.RuntimeHealth{}
	if provider, ok := s.integrationRuntime.(interface {
		Health() []integration.RuntimeHealth
	}); ok {
		health = provider.Health()
	}
	writeJSON(w, http.StatusOK, map[string]any{"catalog": s.integrations.Catalog(discovery), "instances": s.integrations.Instances(), "health": health})
}

func decodeIntegrationInput(w http.ResponseWriter, request *http.Request) (integrationInput, bool) {
	var input integrationInput
	if err := decodeJSON(w, request, maxIntegrationRequestBytes, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request")
		return integrationInput{}, false
	}
	if input.Config == nil {
		input.Config = map[string]any{}
	}
	if input.Secrets == nil {
		input.Secrets = map[string]string{}
	}
	return input, true
}

func splitPath(value string) []string {
	value = strings.Trim(value, "/")
	if value == "" {
		return nil
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return []string{"invalid", "path", "shape"}
		}
	}
	return parts
}

func writeIntegrationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, integration.ErrUnknownIntegration), errors.Is(err, integration.ErrInstanceNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, integration.ErrAlreadyConfigured):
		writeAPIError(w, http.StatusConflict, "already_configured")
	case errors.Is(err, integration.ErrRequiredIntegration):
		writeAPIError(w, http.StatusConflict, "required_integration")
	case errors.Is(err, integration.ErrProbeFailed):
		writeAPIError(w, http.StatusUnprocessableEntity, "probe_failed")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeAPIError(w, http.StatusRequestTimeout, "timeout")
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_integration")
	}
}
