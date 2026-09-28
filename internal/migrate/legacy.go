package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/persist"
)

const maxLegacyDashboardBytes = 32 * 1024

type LegacyPaths struct {
	Config              string
	TrueNASSecret       string
	Dashboard           string
	HomeAssistantSecret string
}

type Result struct {
	Imported          bool     `json:"imported"`
	NeedsAdmin        bool     `json:"needs_admin"`
	Warnings          []string `json:"warnings,omitempty"`
	SourceFingerprint string   `json:"source_fingerprint"`
}

type legacyDashboard struct {
	Width      int      `json:"width"`
	Metrics    []string `json:"metrics"`
	Activities []string `json:"activities"`
}

type stagedSecret struct {
	ref     persist.SecretRef
	discard func() error
}

func ImportLegacy(ctx context.Context, paths LegacyPaths, state *persist.Store, secrets *persist.SecretStore) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if state == nil || secrets == nil {
		return Result{}, errors.New("state and secret stores are required")
	}
	cfg, err := config.LoadLegacy(paths.Config, paths.TrueNASSecret)
	if err != nil {
		return Result{}, err
	}
	dashboard, warnings, dashboardBytes, err := loadLegacyDashboard(paths.Dashboard, cfg.Dashboard)
	if err != nil {
		return Result{}, fmt.Errorf("load legacy dashboard: %w", err)
	}
	homeToken := ""
	if cfg.HomeAssistant.Enabled {
		homeToken, err = config.LoadHomeAssistantToken(paths.HomeAssistantSecret)
		if err != nil {
			return Result{}, err
		}
	}
	fingerprint, err := legacyFingerprint(paths.Config, dashboardBytes, cfg, homeToken)
	if err != nil {
		return Result{}, err
	}
	current := state.Snapshot()
	if current.Legacy != nil && current.Legacy.SourceFingerprint == fingerprint {
		if err := secrets.Collect(liveSecretRefs(current)); err != nil {
			return Result{}, fmt.Errorf("collect legacy secret revisions: %w", err)
		}
		return Result{NeedsAdmin: !current.SetupComplete, Warnings: append([]string(nil), current.Legacy.Warnings...), SourceFingerprint: fingerprint}, nil
	}

	staged := []stagedSecret{}
	discardAll := func() {
		for _, item := range staged {
			_ = item.discard()
		}
	}
	stage := func(instanceID, key, value string) (persist.SecretRef, error) {
		ref, discard, err := secrets.Stage(instanceID, key, []byte(value))
		if err != nil {
			return persist.SecretRef{}, err
		}
		staged = append(staged, stagedSecret{ref: ref, discard: discard})
		return ref, nil
	}

	integrations, err := importIntegrations(cfg, homeToken, stage)
	if err != nil {
		discardAll()
		return Result{}, err
	}
	next := persist.State{
		SchemaVersion: persist.CurrentSchemaVersion,
		SetupComplete: false,
		Server: persist.ServerSettings{
			Listen: cfg.Server.Listen, ReadTimeout: cfg.Server.ReadTimeout.String(), WriteTimeout: cfg.Server.WriteTimeout.String(),
			Title: cfg.Dashboard.Title, Language: cfg.Dashboard.Language, Timezone: cfg.Dashboard.Timezone, Theme: cfg.Dashboard.Theme,
			BackgroundStrength: cfg.Dashboard.BackgroundStrength, Width: dashboard.Width,
		},
		Integrations: integrations,
		Widgets:      importWidgets(cfg, dashboard),
		Legacy:       &persist.LegacyMetadata{SourceFingerprint: fingerprint, Warnings: append([]string(nil), warnings...)},
	}
	if err := state.Update(func(value *persist.State) error {
		*value = next
		return nil
	}); err != nil {
		discardAll()
		return Result{}, fmt.Errorf("save imported state: %w", err)
	}
	if err := secrets.Collect(liveSecretRefs(next)); err != nil {
		warnings = append(warnings, "未能清理旧密钥版本；当前配置仍然可用")
	}
	return Result{Imported: true, NeedsAdmin: true, Warnings: warnings, SourceFingerprint: fingerprint}, nil
}

func importIntegrations(cfg config.Config, homeToken string, stage func(string, string, string) (persist.SecretRef, error)) ([]persist.Integration, error) {
	items := make([]persist.Integration, 0, 8)
	trueNAS := persist.Integration{ID: "truenas-main", Type: "truenas", Enabled: true, Config: map[string]any{
		"url": cfg.TrueNAS.URL, "username": cfg.TrueNAS.Username, "insecure_skip_verify": cfg.TrueNAS.InsecureSkipVerify,
		"call_timeout": cfg.TrueNAS.CallTimeout.String(), "realtime_refresh": cfg.Refresh.Realtime.String(),
		"apps_refresh": cfg.Refresh.Apps.String(), "alerts_refresh": cfg.Refresh.Alerts.String(), "system_refresh": cfg.Refresh.System.String(),
		"pool_refresh": cfg.Refresh.Pools.String(), "disk_refresh": cfg.Refresh.Disks.String(), "apps": cfg.Apps,
	}, SecretRefs: map[string]persist.SecretRef{}}
	ref, err := stage(trueNAS.ID, "api_key", cfg.APIKey)
	if err != nil {
		return nil, err
	}
	trueNAS.SecretRefs["api_key"] = ref
	items = append(items, trueNAS)

	if cfg.Weather.Enabled {
		item := persist.Integration{ID: "qweather-main", Type: "qweather", Enabled: true, Config: map[string]any{
			"api_host":     cfg.Weather.APIHost,
			"name":         cfg.Weather.Name,
			"latitude":     strconv.FormatFloat(cfg.Weather.Latitude, 'f', -1, 64),
			"longitude":    strconv.FormatFloat(cfg.Weather.Longitude, 'f', -1, 64),
			"units":        cfg.Weather.Units,
			"call_timeout": cfg.Weather.CallTimeout.String(), "refresh": cfg.Refresh.Weather.String(),
		}, SecretRefs: map[string]persist.SecretRef{}}
		ref, err := stage(item.ID, "api_key", cfg.Weather.APIKey)
		if err != nil {
			return nil, err
		}
		item.SecretRefs["api_key"] = ref
		items = append(items, item)
	}
	if cfg.Plex.Enabled {
		item, err := secretIntegration("plex-main", "plex", map[string]any{"url": cfg.Plex.URL, "call_timeout": cfg.Plex.CallTimeout.String(), "refresh": cfg.Refresh.Plex.String()}, "token", cfg.Plex.Token, stage)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if cfg.Jellyfin.Enabled {
		item, err := secretIntegration("jellyfin-main", "jellyfin", map[string]any{"url": cfg.Jellyfin.URL, "call_timeout": cfg.Jellyfin.CallTimeout.String(), "refresh": cfg.Refresh.Jellyfin.String()}, "api_key", cfg.Jellyfin.Token, stage)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if cfg.QBittorrent.Enabled {
		item, err := secretIntegration("qbittorrent-main", "qbittorrent", map[string]any{"url": cfg.QBittorrent.URL, "username": cfg.QBittorrent.Username, "call_timeout": cfg.QBittorrent.CallTimeout.String(), "refresh": cfg.Refresh.QBittorrent.String()}, "password", cfg.QBittorrent.Password, stage)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if cfg.UptimeKuma.Enabled {
		item, err := secretIntegration("uptime-kuma-main", "uptime_kuma", map[string]any{"url": cfg.UptimeKuma.URL, "call_timeout": cfg.UptimeKuma.CallTimeout.String(), "refresh": cfg.Refresh.UptimeKuma.String()}, "api_key", cfg.UptimeKuma.APIKey, stage)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if cfg.HomeAssistant.Enabled {
		item, err := secretIntegration("home-assistant-main", "home_assistant", map[string]any{
			"url": cfg.HomeAssistant.URL, "fan_entity_id": cfg.HomeAssistant.FanEntityID, "fan_name": cfg.HomeAssistant.FanName,
			"remind_after": cfg.HomeAssistant.RemindAfter.String(), "call_timeout": cfg.HomeAssistant.CallTimeout.String(), "refresh": cfg.Refresh.HomeAssistant.String(),
		}, "token", homeToken, stage)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if cfg.Scrutiny.Enabled {
		items = append(items, persist.Integration{ID: "scrutiny-main", Type: "scrutiny", Enabled: true, Config: map[string]any{
			"url": cfg.Scrutiny.URL, "call_timeout": cfg.Scrutiny.CallTimeout.String(), "refresh": cfg.Refresh.Scrutiny.String(),
		}})
	}
	return items, nil
}

func secretIntegration(id, kind string, values map[string]any, secretKey, secretValue string, stage func(string, string, string) (persist.SecretRef, error)) (persist.Integration, error) {
	ref, err := stage(id, secretKey, secretValue)
	if err != nil {
		return persist.Integration{}, err
	}
	return persist.Integration{ID: id, Type: kind, Enabled: true, Config: values, SecretRefs: map[string]persist.SecretRef{secretKey: ref}}, nil
}

func loadLegacyDashboard(path string, base config.DashboardConfig) (legacyDashboard, []string, []byte, error) {
	fallback := legacyDashboard{Width: base.Width}
	for _, metric := range base.Metrics {
		fallback.Metrics = append(fallback.Metrics, metric.Type)
	}
	for _, activity := range base.Activities {
		fallback.Activities = append(fallback.Activities, activity.Type)
	}
	if strings.TrimSpace(path) == "" {
		return fallback, []string{"未找到旧版网页布局，已使用 YAML 布局"}, nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fallback, []string{"未找到旧版网页布局，已使用 YAML 布局"}, nil, nil
	}
	if err != nil {
		return legacyDashboard{}, nil, nil, err
	}
	if len(data) > maxLegacyDashboardBytes {
		return legacyDashboard{}, nil, data, errors.New("dashboard settings are too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var dashboard legacyDashboard
	if err := decoder.Decode(&dashboard); err != nil {
		return legacyDashboard{}, nil, data, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return legacyDashboard{}, nil, data, errors.New("multiple dashboard documents are not allowed")
	}
	if dashboard.Width < config.DashboardMinWidth || dashboard.Width > config.DashboardMaxWidth {
		return legacyDashboard{}, nil, data, errors.New("dashboard width is outside the supported range")
	}
	return dashboard, nil, data, nil
}

func importWidgets(cfg config.Config, dashboard legacyDashboard) []persist.Widget {
	metricConfigs := map[string]config.MetricConfig{}
	for _, metric := range cfg.Dashboard.Metrics {
		metricConfigs[metric.Type] = metric
	}
	activityConfigs := map[string]config.ActivityConfig{}
	for _, activity := range cfg.Dashboard.Activities {
		activityConfigs[activity.Type] = activity
	}
	items := make([]persist.Widget, 0, len(dashboard.Metrics)+len(dashboard.Activities))
	for _, kind := range dashboard.Metrics {
		widget := persist.Widget{ID: "legacy-" + kind, DefinitionID: kind, IntegrationID: "truenas-main", Enabled: true, Order: len(items), Config: map[string]any{}}
		if kind == config.MetricTypeDiskTemperature {
			match := metricConfigs[kind].Match
			widget.Config = map[string]any{"serial": match.Serial, "model": match.Model, "size_bytes": match.SizeBytes, "name": match.Name}
		}
		items = append(items, widget)
	}
	for _, kind := range dashboard.Activities {
		widget := persist.Widget{ID: "legacy-" + kind, DefinitionID: kind, IntegrationID: widgetIntegration(kind), Enabled: true, Order: len(items), Config: map[string]any{}}
		if limit := activityConfigs[kind].Limit; limit > 0 {
			widget.Config["limit"] = limit
		}
		items = append(items, widget)
	}
	return items
}

func widgetIntegration(kind string) string {
	switch kind {
	case config.ActivityTypeWeather:
		return "qweather-main"
	case config.ActivityTypePlex:
		return "plex-main"
	case config.ActivityTypeJellyfin:
		return "jellyfin-main"
	case config.ActivityTypeQBittorrent:
		return "qbittorrent-main"
	case config.ActivityTypeUptimeKuma:
		return "uptime-kuma-main"
	case config.ActivityTypeHomeAssistantFan:
		return "home-assistant-main"
	default:
		return "truenas-main"
	}
}

func legacyFingerprint(configPath string, dashboard []byte, cfg config.Config, homeToken string) (string, error) {
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	parts := map[string][]byte{
		"config": configBytes, "dashboard": dashboard, "truenas": []byte(cfg.APIKey), "home_assistant": []byte(homeToken),
		"qweather_host": []byte(cfg.Weather.APIHost), "qweather_key": []byte(cfg.Weather.APIKey), "plex": []byte(cfg.Plex.Token),
		"jellyfin": []byte(cfg.Jellyfin.Token), "qbittorrent": []byte(cfg.QBittorrent.Password), "uptime_kuma": []byte(cfg.UptimeKuma.APIKey),
	}
	labels := make([]string, 0, len(parts))
	for label := range parts {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	hash := sha256.New()
	for _, label := range labels {
		_, _ = hash.Write([]byte(label))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(parts[label])
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func liveSecretRefs(state persist.State) map[persist.SecretRef]struct{} {
	result := map[persist.SecretRef]struct{}{}
	for _, integration := range state.Integrations {
		for _, ref := range integration.SecretRefs {
			result[ref] = struct{}{}
		}
	}
	return result
}
