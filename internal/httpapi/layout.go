package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"example.com/nas-wallboard/internal/model"
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
		if next.Revision == "" {
			writeAPIError(w, http.StatusConflict, "layout_conflict")
			return
		}
		if err := s.widgets.Update(next); err != nil {
			if errors.Is(err, widget.ErrLayoutConflict) {
				writeAPIError(w, http.StatusConflict, "layout_conflict")
				return
			}
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
	interfaces := []model.NetworkInterfaceStatus{}
	if s.store != nil {
		realtime := s.store.Snapshot().Realtime
		interfaces = append(interfaces, realtime.Data.NetworkInterfaces...)
		if realtime.Stale || realtime.Error != "" {
			for index := range interfaces {
				interfaces[index].Available = false
			}
		}
	}
	capabilities := map[string]bool{}
	sources := []map[string]any{}
	if s.configState != nil {
		for _, source := range s.configState.Snapshot().Integrations {
			capabilities[source.Type] = source.Enabled
			sources = append(sources, map[string]any{"id": source.ID, "type": source.Type, "enabled": source.Enabled})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"catalog":            s.widgets.CatalogForLayout(capabilities),
		"sources":            sources,
		"layout":             s.widgets.Layout(),
		"network_interfaces": interfaces,
	})
}
