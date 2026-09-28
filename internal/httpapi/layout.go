package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"example.com/nas-wallboard/internal/widget"
)

const maxLayoutRequestBytes = 64 * 1024

func (s *server) manageLayout(w http.ResponseWriter, request *http.Request) {
	if s.widgets == nil || s.manager == nil || s.builder == nil {
		http.NotFound(w, request)
		return
	}
	principal, ok := s.requireManagement(w, request)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		s.writeLayout(w)
	case http.MethodPut:
		if !s.requireMutation(w, request, principal) {
			return
		}
		if request.Header.Get("Content-Type") != "application/json" {
			writeAPIError(w, http.StatusUnsupportedMediaType, "content_type")
			return
		}
		request.Body = http.MaxBytesReader(w, request.Body, maxLayoutRequestBytes)
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var next widget.Layout
		if err := decoder.Decode(&next); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_layout")
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeAPIError(w, http.StatusBadRequest, "invalid_layout")
			return
		}
		if err := s.widgets.Update(next); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_layout")
			return
		}
		configured, err := s.widgets.DashboardConfig(s.manager.Config())
		if err != nil || s.builder.Update(configured) != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_layout")
			return
		}
		s.writeLayout(w)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) writeLayout(w http.ResponseWriter) {
	capabilities := map[string]bool{}
	sources := []map[string]any{}
	if s.configState != nil {
		for _, source := range s.configState.Snapshot().Integrations {
			if !source.Enabled {
				continue
			}
			capabilities[source.Type] = true
			sources = append(sources, map[string]any{"id": source.ID, "type": source.Type, "enabled": true})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"catalog": s.widgets.CatalogForLayout(capabilities),
		"sources": sources,
		"layout":  s.widgets.Layout(),
	})
}
