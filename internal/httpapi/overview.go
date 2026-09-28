package httpapi

import (
	"net/http"

	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/persist"
)

type runtimeHealthReader interface {
	Health() []integration.RuntimeHealth
}

func (s *server) manageOverview(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireManagement(w, request); !ok {
		return
	}
	runtime := s.store.Snapshot()
	configured := s.configState.Snapshot()
	health := []integration.RuntimeHealth{}
	if reader, ok := s.integrationRuntime.(runtimeHealthReader); ok {
		health = reader.Health()
	}
	uptime := s.clock().Sub(s.startedAt)
	if uptime < 0 {
		uptime = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":                    s.version,
		"application_uptime_seconds": int64(uptime.Seconds()),
		"nas": map[string]any{
			"version":        runtime.System.Data.Version,
			"uptime_seconds": runtime.System.Data.UptimeSeconds,
			"connected":      runtime.Connected,
			"last_update":    runtime.SnapshotAt,
		},
		"integrations": health,
		"migration": map[string]any{
			"imported":      configured.Legacy != nil,
			"warning_count": legacyWarningCount(configured.Legacy),
		},
	})
}

func legacyWarningCount(metadata *persist.LegacyMetadata) int {
	if metadata == nil {
		return 0
	}
	return len(metadata.Warnings)
}
