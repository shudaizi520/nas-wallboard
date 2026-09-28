package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testSecret = "wallboard-test-secret"

func writeFixture(t *testing.T, name, contents string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func safeSecretFile(t *testing.T, contents string) string {
	t.Helper()
	return writeFixture(t, "truenas_api_key", contents, 0o600)
}

func minimalConfig(t *testing.T, extra string) string {
	t.Helper()
	return writeFixture(t, "config.yaml", "truenas:\n  url: wss://nas.example.invalid/api/current\n  username: nas_wallboard\n"+extra, 0o600)
}

func TestLoadAppliesSafeDefaults(t *testing.T) {
	configPath := minimalConfig(t, "")
	secretPath := safeSecretFile(t, testSecret)

	cfg, err := Load(configPath, secretPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Listen != ":8080" {
		t.Errorf("Server.Listen = %q, want :8080", cfg.Server.Listen)
	}
	if cfg.Server.ReadTimeout.Duration != 10*time.Second || cfg.Server.WriteTimeout.Duration != 10*time.Second {
		t.Errorf("server timeouts = %v/%v, want 10s/10s", cfg.Server.ReadTimeout.Duration, cfg.Server.WriteTimeout.Duration)
	}
	if cfg.Dashboard.Title != "NAS Wallboard" || cfg.Dashboard.Language != "zh-CN" || cfg.Dashboard.Timezone != "Asia/Shanghai" {
		t.Errorf("dashboard defaults = %#v", cfg.Dashboard)
	}
	if cfg.Dashboard.Theme != "midnight" || cfg.Dashboard.BackgroundStrength != 0.65 {
		t.Errorf("visual defaults = %#v", cfg.Dashboard)
	}
	if cfg.Refresh.Realtime.Duration != 5*time.Second || cfg.Refresh.Apps.Duration != 30*time.Second || cfg.Refresh.Alerts.Duration != 30*time.Second {
		t.Errorf("fast refresh defaults = %#v", cfg.Refresh)
	}
	if cfg.Refresh.System.Duration != time.Minute || cfg.Refresh.Pools.Duration != time.Minute || cfg.Refresh.Disks.Duration != time.Minute {
		t.Errorf("slow refresh defaults = %#v", cfg.Refresh)
	}
	if cfg.Refresh.Weather.Duration != 15*time.Minute {
		t.Errorf("weather refresh default = %v, want 15m", cfg.Refresh.Weather.Duration)
	}
	if cfg.Refresh.HomeAssistant.Duration != 30*time.Second {
		t.Errorf("home assistant refresh default = %v, want 30s", cfg.Refresh.HomeAssistant.Duration)
	}
	if cfg.Weather.Enabled {
		t.Error("weather must be disabled by default")
	}
	if cfg.HomeAssistant.Enabled || cfg.HomeAssistant.RemindAfter.Duration != 2*time.Hour || cfg.HomeAssistant.CallTimeout.Duration != 5*time.Second {
		t.Errorf("home assistant defaults = %#v", cfg.HomeAssistant)
	}
	if cfg.APIKey != testSecret {
		t.Error("API key was not loaded")
	}
}

func TestLoadAcceptsEnabledHomeAssistantFan(t *testing.T) {
	configPath := minimalConfig(t, "home_assistant:\n  enabled: true\n  url: http://home-assistant:8123\n  fan_entity_id: fan.living_room\n  fan_name: 客厅风扇\n  remind_after: 90m\n  call_timeout: 4s\nrefresh:\n  home_assistant: 20s\n")
	cfg, err := Load(configPath, safeSecretFile(t, testSecret))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.HomeAssistant.Enabled || cfg.HomeAssistant.URL != "http://home-assistant:8123" || cfg.HomeAssistant.FanEntityID != "fan.living_room" || cfg.HomeAssistant.FanName != "客厅风扇" {
		t.Fatalf("HomeAssistant = %#v", cfg.HomeAssistant)
	}
	if cfg.HomeAssistant.RemindAfter.Duration != 90*time.Minute || cfg.HomeAssistant.CallTimeout.Duration != 4*time.Second || cfg.Refresh.HomeAssistant.Duration != 20*time.Second {
		t.Fatalf("HomeAssistant durations = %#v / %#v", cfg.HomeAssistant, cfg.Refresh)
	}
}

func TestLoadExternalSourcesFromSecureSecretFiles(t *testing.T) {
	qbitSecret := writeFixture(t, "qbittorrent_password", "  qbit-secret\n", 0o600)
	plexSecret := writeFixture(t, "plex_token", "plex-secret", 0o600)
	jellyfinSecret := writeFixture(t, "jellyfin_api_key", "jellyfin-secret", 0o600)
	uptimeSecret := writeFixture(t, "uptime_kuma_api_key", "uptime-secret", 0o600)
	extra := "qbittorrent:\n  enabled: true\n  url: http://qbittorrent:8080\n  username: wallboard\n  secret_file: " + qbitSecret + "\n  call_timeout: 4s\n" +
		"plex:\n  enabled: true\n  url: http://plex:32400\n  secret_file: " + plexSecret + "\n" +
		"jellyfin:\n  enabled: true\n  url: http://jellyfin:8096\n  secret_file: " + jellyfinSecret + "\n" +
		"uptime_kuma:\n  enabled: true\n  url: http://uptime-kuma:3001\n  secret_file: " + uptimeSecret + "\n" +
		"refresh:\n  qbittorrent: 20s\n  plex: 21s\n  jellyfin: 22s\n  uptime_kuma: 45s\n"

	cfg, err := Load(minimalConfig(t, extra), safeSecretFile(t, testSecret))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.QBittorrent.Enabled || cfg.QBittorrent.Username != "wallboard" || cfg.QBittorrent.Password != "qbit-secret" || cfg.QBittorrent.CallTimeout.Duration != 4*time.Second {
		t.Fatalf("QBittorrent = %#v", cfg.QBittorrent)
	}
	if cfg.Plex.Token != "plex-secret" || cfg.Jellyfin.Token != "jellyfin-secret" || cfg.UptimeKuma.APIKey != "uptime-secret" {
		t.Fatalf("source secrets were not loaded")
	}
	if cfg.Refresh.QBittorrent.Duration != 20*time.Second || cfg.Refresh.Plex.Duration != 21*time.Second || cfg.Refresh.Jellyfin.Duration != 22*time.Second || cfg.Refresh.UptimeKuma.Duration != 45*time.Second {
		t.Fatalf("external refresh = %#v", cfg.Refresh)
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"qbit-secret", "plex-secret", "jellyfin-secret", "uptime-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("serialized config contains secret %q", secret)
		}
	}
}

func TestLoadQWeatherAndScrutinySources(t *testing.T) {
	hostFile := writeFixture(t, "qweather_api_host", "  abc123.qweatherapi.com\n", 0o600)
	keyFile := writeFixture(t, "qweather_api_key", "weather-secret", 0o600)
	extra := "weather:\n  enabled: true\n  name: 深圳\n  latitude: 22.54\n  longitude: 114.06\n  api_host_file: " + hostFile + "\n  secret_file: " + keyFile + "\n  call_timeout: 6s\n" +
		"scrutiny:\n  enabled: true\n  url: http://scrutiny:8080\n  call_timeout: 4s\n" +
		"refresh:\n  scrutiny: 30m\n"

	cfg, err := Load(minimalConfig(t, extra), safeSecretFile(t, testSecret))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Weather.Enabled || cfg.Weather.Name != "深圳" || cfg.Weather.APIHost != "abc123.qweatherapi.com" || cfg.Weather.APIKey != "weather-secret" || cfg.Weather.CallTimeout.Duration != 6*time.Second {
		t.Fatalf("Weather = %#v", cfg.Weather)
	}
	if !cfg.Scrutiny.Enabled || cfg.Scrutiny.URL != "http://scrutiny:8080" || cfg.Scrutiny.CallTimeout.Duration != 4*time.Second || cfg.Refresh.Scrutiny.Duration != 30*time.Minute {
		t.Fatalf("Scrutiny/refresh = %#v / %#v", cfg.Scrutiny, cfg.Refresh)
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "weather-secret") {
		t.Fatal("serialized config contains QWeather API key")
	}
}

func TestLoadExternalSourcesAreDisabledByDefault(t *testing.T) {
	cfg, err := Load(minimalConfig(t, "plex:\n  secret_file: /missing/ignored\n"), safeSecretFile(t, testSecret))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.QBittorrent.Enabled || cfg.Plex.Enabled || cfg.Jellyfin.Enabled || cfg.UptimeKuma.Enabled {
		t.Fatalf("external source unexpectedly enabled: %#v", cfg)
	}
	if cfg.Refresh.QBittorrent.Duration != 15*time.Second || cfg.Refresh.Plex.Duration != 15*time.Second || cfg.Refresh.Jellyfin.Duration != 15*time.Second || cfg.Refresh.UptimeKuma.Duration != 30*time.Second {
		t.Fatalf("external defaults = %#v", cfg.Refresh)
	}
}

func TestLoadRejectsUnsafeExternalSourceConfiguration(t *testing.T) {
	safe := writeFixture(t, "source_secret", "source-secret", 0o600)
	unsafe := writeFixture(t, "unsafe_source_secret", "source-secret", 0o640)
	tests := []struct {
		name  string
		extra string
		want  string
	}{
		{"qbit-missing-username", "qbittorrent:\n  enabled: true\n  url: http://qbittorrent:8080\n  secret_file: " + safe + "\n", "username"},
		{"plex-credentials-in-url", "plex:\n  enabled: true\n  url: http://user:pass@plex:32400\n  secret_file: " + safe + "\n", "credentials"},
		{"jellyfin-unsafe-url", "jellyfin:\n  enabled: true\n  url: file:///tmp/jellyfin\n  secret_file: " + safe + "\n", "jellyfin.url"},
		{"uptime-unsafe-secret", "uptime_kuma:\n  enabled: true\n  url: http://uptime-kuma:3001\n  secret_file: " + unsafe + "\n", "Uptime Kuma"},
		{"missing-secret-file", "plex:\n  enabled: true\n  url: http://plex:32400\n  secret_file: /missing/token\n", "Plex"},
		{"zero-timeout", "plex:\n  enabled: true\n  url: http://plex:32400\n  secret_file: " + safe + "\n  call_timeout: 0s\n", "call_timeout"},
		{"fast-refresh", "refresh:\n  qbittorrent: 4s\n", "qbittorrent"},
		{"weather-unsafe-host", "weather:\n  enabled: true\n  latitude: 22.54\n  longitude: 114.06\n  api_host_file: " + writeFixture(t, "bad_weather_host", "evil.example.com", 0o600) + "\n  secret_file: " + safe + "\n", "API Host"},
		{"scrutiny-credentials-in-url", "scrutiny:\n  enabled: true\n  url: http://user:pass@scrutiny:8080\n", "credentials"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(minimalConfig(t, tt.extra), safeSecretFile(t, testSecret))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadDashboardComponents(t *testing.T) {
	configPath := minimalConfig(t, `dashboard:
  width: 300
  metrics:
    - type: cpu
    - type: cpu_temperature
    - type: network
    - type: disk_temperature
      match:
        model: ST14000NM001G-2KJ103
        size_bytes: 14000519643136
  activities:
    - type: weather
    - type: app_updates
    - type: truenas_alerts
      limit: 2
`)
	cfg, err := Load(configPath, safeSecretFile(t, testSecret))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Dashboard.Width != 300 {
		t.Fatalf("Dashboard.Width = %d, want 300", cfg.Dashboard.Width)
	}
	if len(cfg.Dashboard.Metrics) != 4 || cfg.Dashboard.Metrics[0].Type != "cpu" || cfg.Dashboard.Metrics[1].Type != "cpu_temperature" || cfg.Dashboard.Metrics[2].Type != "network" || cfg.Dashboard.Metrics[3].Type != "disk_temperature" {
		t.Fatalf("Dashboard.Metrics = %#v", cfg.Dashboard.Metrics)
	}
	match := cfg.Dashboard.Metrics[3].Match
	if match.Model != "ST14000NM001G-2KJ103" || match.SizeBytes != 14000519643136 {
		t.Fatalf("disk match = %#v", match)
	}
	if len(cfg.Dashboard.Activities) != 3 || cfg.Dashboard.Activities[0].Type != "weather" || cfg.Dashboard.Activities[1].Type != "app_updates" || cfg.Dashboard.Activities[2].Type != "truenas_alerts" || cfg.Dashboard.Activities[2].Limit != 2 {
		t.Fatalf("Dashboard.Activities = %#v", cfg.Dashboard.Activities)
	}
}

func TestLoadRejectsInvalidDashboardComponents(t *testing.T) {
	tests := []struct {
		name  string
		extra string
		want  string
	}{
		{"width-low", "dashboard:\n  width: 299\n", "dashboard.width"},
		{"width-high", "dashboard:\n  width: 721\n", "dashboard.width"},
		{"unknown-metric", "dashboard:\n  metrics:\n    - type: memory\n", "dashboard.metrics"},
		{"duplicate-metric", "dashboard:\n  metrics:\n    - type: cpu\n    - type: cpu\n", "duplicated"},
		{"disk-without-match", "dashboard:\n  metrics:\n    - type: disk_temperature\n", "match"},
		{"unknown-activity", "dashboard:\n  activities:\n    - type: sonarr\n", "dashboard.activities"},
		{"duplicate-activity", "dashboard:\n  activities:\n    - type: app_updates\n    - type: app_updates\n", "duplicated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(minimalConfig(t, tt.extra), safeSecretFile(t, testSecret))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadKeepsLegacyDashboardFields(t *testing.T) {
	configPath := minimalConfig(t, `dashboard:
  title: Old Wallboard
  language: zh-CN
  timezone: Asia/Shanghai
  theme: midnight
  background_strength: 0.5
  show_pools: true
  show_disks: true
  show_apps: true
  show_alerts: true
`)
	cfg, err := Load(configPath, safeSecretFile(t, testSecret))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Dashboard.Title != "Old Wallboard" || cfg.Dashboard.Width != 360 {
		t.Fatalf("Dashboard = %#v", cfg.Dashboard)
	}
}

func TestLoadRejectsUnsafeHomeAssistantConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		extra string
		want  string
	}{
		{"missing-url", "home_assistant:\n  enabled: true\n  fan_entity_id: fan.living_room\n", "home_assistant.url"},
		{"unsafe-scheme", "home_assistant:\n  enabled: true\n  url: file:///tmp/ha\n  fan_entity_id: fan.living_room\n", "home_assistant.url"},
		{"credentials-in-url", "home_assistant:\n  enabled: true\n  url: http://user:pass@home-assistant:8123\n  fan_entity_id: fan.living_room\n", "credentials"},
		{"wrong-domain", "home_assistant:\n  enabled: true\n  url: http://home-assistant:8123\n  fan_entity_id: switch.living_room\n", "fan_entity_id"},
		{"zero-reminder", "home_assistant:\n  enabled: true\n  url: http://home-assistant:8123\n  fan_entity_id: fan.living_room\n  remind_after: 0s\n", "remind_after"},
		{"zero-timeout", "home_assistant:\n  enabled: true\n  url: http://home-assistant:8123\n  fan_entity_id: fan.living_room\n  call_timeout: 0s\n", "call_timeout"},
		{"fast-refresh", "refresh:\n  home_assistant: 14s\n", "home_assistant"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(minimalConfig(t, tt.extra), safeSecretFile(t, testSecret))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadHomeAssistantTokenUsesSecureSecretFile(t *testing.T) {
	path := writeFixture(t, "home_assistant_token", "  ha-test-token\r\n", 0o600)
	token, err := LoadHomeAssistantToken(path)
	if err != nil {
		t.Fatalf("LoadHomeAssistantToken() error = %v", err)
	}
	if token != "ha-test-token" {
		t.Fatalf("token = %q", token)
	}

	_, err = LoadHomeAssistantToken(writeFixture(t, "unsafe_token", "ha-test-token", 0o640))
	if err == nil || !strings.Contains(err.Error(), "Home Assistant") {
		t.Fatalf("unsafe token error = %v", err)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	configPath := minimalConfig(t, "unexpected: true\n")
	_, err := Load(configPath, safeSecretFile(t, testSecret))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("Load() error = %v, want unknown-field error", err)
	}
}

func TestLoadRejectsMissingTrueNASUsername(t *testing.T) {
	configPath := writeFixture(t, "config.yaml", "truenas:\n  url: wss://nas.example.invalid/api/current\n", 0o600)
	_, err := Load(configPath, safeSecretFile(t, testSecret))
	if err == nil || !strings.Contains(err.Error(), "truenas.username") {
		t.Fatalf("Load() error = %v, want truenas.username validation error", err)
	}
}

func TestLoadRejectsIntervalsBelowMinimum(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value string
	}{
		{"realtime", "realtime", "2s"},
		{"apps", "apps", "14s"},
		{"alerts", "alerts", "14s"},
		{"system", "system", "29s"},
		{"pools", "pools", "29s"},
		{"disks", "disks", "29s"},
		{"weather", "weather", "4m59s"},
		{"home-assistant", "home_assistant", "14s"},
		{"scrutiny", "scrutiny", "4m59s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := minimalConfig(t, "refresh:\n  "+tt.field+": "+tt.value+"\n")
			_, err := Load(configPath, safeSecretFile(t, testSecret))
			if err == nil || !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("Load() error = %v, want %s minimum error", err, tt.field)
			}
		})
	}
}

func TestLoadReadsAndTrimsSecretFile(t *testing.T) {
	cfg, err := Load(minimalConfig(t, ""), safeSecretFile(t, "  "+testSecret+"\r\n"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.APIKey != testSecret {
		t.Fatalf("APIKey = %q, want trimmed secret", cfg.APIKey)
	}
}

func TestLoadRejectsEmptyOrGroupReadableSecret(t *testing.T) {
	tests := []struct {
		name    string
		content string
		mode    os.FileMode
	}{
		{"empty", " \n", 0o600},
		{"group-readable", testSecret, 0o640},
		{"world-readable", testSecret, 0o604},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secretPath := writeFixture(t, "truenas_api_key", tt.content, tt.mode)
			_, err := Load(minimalConfig(t, ""), secretPath)
			if err == nil {
				t.Fatal("Load() error = nil, want secret validation error")
			}
		})
	}
}

func TestLoadNeverIncludesSecretInErrors(t *testing.T) {
	configPath := minimalConfig(t, "refresh:\n  realtime: 1s\n")
	_, err := Load(configPath, safeSecretFile(t, testSecret))
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	if strings.Contains(err.Error(), testSecret) {
		t.Fatalf("Load() leaked secret in error: %v", err)
	}
}
