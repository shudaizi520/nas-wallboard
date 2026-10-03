package config

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	maxSecretBytes        = 16 * 1024
	DashboardDefaultWidth = 360
	DashboardMinWidth     = 300
	DashboardMaxWidth     = 720

	MetricTypeCPU             = "cpu"
	MetricTypeCPUTemperature  = "cpu_temperature"
	MetricTypeDiskTemperature = "disk_temperature"
	MetricTypeNetwork         = "network"
	MetricTypePoolCapacity    = "pool_capacity"

	ActivityTypeTrueNASAlerts         = "truenas_alerts"
	ActivityTypeAppHealth             = "app_health"
	ActivityTypeAppUpdates            = "app_updates"
	ActivityTypeHomeAssistantFan      = "home_assistant_fan"
	ActivityTypeQBittorrent           = "qbittorrent"
	ActivityTypePlex                  = "plex"
	ActivityTypeJellyfin              = "jellyfin"
	ActivityTypeUptimeKuma            = "uptime_kuma"
	ActivityTypeWeather               = "weather"
	ActivityTypeMemoryPressure        = "memory_pressure"
	ActivityTypeSMARTExceptions       = "smart_exceptions"
	ActivityTypeReplicationExceptions = "replication_exceptions"
)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(text []byte) error {
	value, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}
	d.Duration = value
	return nil
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	TrueNAS       TrueNASConfig       `yaml:"truenas"`
	Dashboard     DashboardConfig     `yaml:"dashboard"`
	Refresh       RefreshConfig       `yaml:"refresh"`
	Apps          []AppConfig         `yaml:"apps"`
	Weather       WeatherConfig       `yaml:"weather"`
	HomeAssistant HomeAssistantConfig `yaml:"home_assistant"`
	QBittorrent   QBittorrentConfig   `yaml:"qbittorrent"`
	Plex          PlexConfig          `yaml:"plex"`
	Jellyfin      JellyfinConfig      `yaml:"jellyfin"`
	UptimeKuma    UptimeKumaConfig    `yaml:"uptime_kuma"`
	Scrutiny      ScrutinyConfig      `yaml:"scrutiny"`
	APIKey        string              `yaml:"-"`
}

type ServerConfig struct {
	Listen       string   `yaml:"listen"`
	ReadTimeout  Duration `yaml:"read_timeout"`
	WriteTimeout Duration `yaml:"write_timeout"`
}

type TrueNASConfig struct {
	URL                string   `yaml:"url"`
	Username           string   `yaml:"username"`
	InsecureSkipVerify bool     `yaml:"insecure_skip_verify"`
	CallTimeout        Duration `yaml:"call_timeout"`
}

type DashboardConfig struct {
	Title              string           `yaml:"title"`
	Language           string           `yaml:"language"`
	Timezone           string           `yaml:"timezone"`
	Theme              string           `yaml:"theme"`
	BackgroundStrength float64          `yaml:"background_strength"`
	ShowPools          bool             `yaml:"show_pools"`
	ShowDisks          bool             `yaml:"show_disks"`
	ShowApps           bool             `yaml:"show_apps"`
	ShowAlerts         bool             `yaml:"show_alerts"`
	Width              int              `yaml:"width"`
	Metrics            []MetricConfig   `yaml:"metrics"`
	Activities         []ActivityConfig `yaml:"activities"`
}

type MetricConfig struct {
	Type            string          `yaml:"type"`
	Interface       string          `yaml:"interface,omitempty"`
	Match           DiskMatchConfig `yaml:"match"`
	Name            string          `yaml:"name,omitempty"`
	WarningPercent  float64         `yaml:"warning_percent,omitempty"`
	CriticalPercent float64         `yaml:"critical_percent,omitempty"`
}

type DiskMatchConfig struct {
	Serial    string `yaml:"serial"`
	Model     string `yaml:"model"`
	SizeBytes uint64 `yaml:"size_bytes"`
	Name      string `yaml:"name"`
}

type ActivityConfig struct {
	Type            string  `yaml:"type"`
	Limit           int     `yaml:"limit"`
	WarningPercent  float64 `yaml:"warning_percent,omitempty"`
	CriticalPercent float64 `yaml:"critical_percent,omitempty"`
}

type RefreshConfig struct {
	Realtime      Duration `yaml:"realtime"`
	Apps          Duration `yaml:"apps"`
	Alerts        Duration `yaml:"alerts"`
	System        Duration `yaml:"system"`
	Pools         Duration `yaml:"pools"`
	Disks         Duration `yaml:"disks"`
	Weather       Duration `yaml:"weather"`
	HomeAssistant Duration `yaml:"home_assistant"`
	QBittorrent   Duration `yaml:"qbittorrent"`
	Plex          Duration `yaml:"plex"`
	Jellyfin      Duration `yaml:"jellyfin"`
	UptimeKuma    Duration `yaml:"uptime_kuma"`
	Scrutiny      Duration `yaml:"scrutiny"`
}

type AppConfig struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	Sort int    `yaml:"sort"`
	Link string `yaml:"link"`
}

type WeatherConfig struct {
	Enabled     bool     `yaml:"enabled"`
	Name        string   `yaml:"name"`
	Latitude    float64  `yaml:"latitude"`
	Longitude   float64  `yaml:"longitude"`
	Units       string   `yaml:"units"`
	APIHostFile string   `yaml:"api_host_file"`
	SecretFile  string   `yaml:"secret_file"`
	CallTimeout Duration `yaml:"call_timeout"`
	APIHost     string   `yaml:"-" json:"-"`
	APIKey      string   `yaml:"-" json:"-"`
}

type HomeAssistantConfig struct {
	Enabled       bool     `yaml:"enabled"`
	URL           string   `yaml:"url"`
	FanEntityID   string   `yaml:"fan_entity_id"`
	PowerEntityID string   `yaml:"power_entity_id"`
	FanName       string   `yaml:"fan_name"`
	RemindAfter   Duration `yaml:"remind_after"`
	CallTimeout   Duration `yaml:"call_timeout"`
}

type QBittorrentConfig struct {
	Enabled     bool     `yaml:"enabled"`
	URL         string   `yaml:"url"`
	Username    string   `yaml:"username"`
	SecretFile  string   `yaml:"secret_file"`
	CallTimeout Duration `yaml:"call_timeout"`
	Password    string   `yaml:"-" json:"-"`
}

type PlexConfig struct {
	Enabled     bool     `yaml:"enabled"`
	URL         string   `yaml:"url"`
	SecretFile  string   `yaml:"secret_file"`
	CallTimeout Duration `yaml:"call_timeout"`
	Token       string   `yaml:"-" json:"-"`
}

type JellyfinConfig struct {
	Enabled     bool     `yaml:"enabled"`
	URL         string   `yaml:"url"`
	SecretFile  string   `yaml:"secret_file"`
	CallTimeout Duration `yaml:"call_timeout"`
	Token       string   `yaml:"-" json:"-"`
}

type UptimeKumaConfig struct {
	Enabled     bool     `yaml:"enabled"`
	URL         string   `yaml:"url"`
	SecretFile  string   `yaml:"secret_file"`
	CallTimeout Duration `yaml:"call_timeout"`
	APIKey      string   `yaml:"-" json:"-"`
}

type ScrutinyConfig struct {
	Enabled     bool     `yaml:"enabled"`
	URL         string   `yaml:"url"`
	CallTimeout Duration `yaml:"call_timeout"`
}

func defaults() Config {
	return Config{
		Server: ServerConfig{
			Listen:       ":8080",
			ReadTimeout:  Duration{10 * time.Second},
			WriteTimeout: Duration{10 * time.Second},
		},
		TrueNAS: TrueNASConfig{CallTimeout: Duration{10 * time.Second}},
		Dashboard: DashboardConfig{
			Title:              "NAS Wallboard",
			Language:           "zh-CN",
			Timezone:           "Asia/Shanghai",
			Theme:              "midnight",
			BackgroundStrength: 0.65,
			ShowPools:          true,
			ShowDisks:          true,
			ShowApps:           true,
			ShowAlerts:         true,
			Width:              DashboardDefaultWidth,
		},
		Refresh: RefreshConfig{
			Realtime:      Duration{5 * time.Second},
			Apps:          Duration{30 * time.Second},
			Alerts:        Duration{30 * time.Second},
			System:        Duration{time.Minute},
			Pools:         Duration{time.Minute},
			Disks:         Duration{time.Minute},
			Weather:       Duration{15 * time.Minute},
			HomeAssistant: Duration{30 * time.Second},
			QBittorrent:   Duration{15 * time.Second},
			Plex:          Duration{15 * time.Second},
			Jellyfin:      Duration{15 * time.Second},
			UptimeKuma:    Duration{30 * time.Second},
			Scrutiny:      Duration{30 * time.Minute},
		},
		Weather:       WeatherConfig{Name: "天气", Units: "metric", CallTimeout: Duration{8 * time.Second}},
		HomeAssistant: HomeAssistantConfig{FanName: "小米风扇", RemindAfter: Duration{2 * time.Hour}, CallTimeout: Duration{5 * time.Second}},
		QBittorrent:   QBittorrentConfig{CallTimeout: Duration{5 * time.Second}},
		Plex:          PlexConfig{CallTimeout: Duration{5 * time.Second}},
		Jellyfin:      JellyfinConfig{CallTimeout: Duration{5 * time.Second}},
		UptimeKuma:    UptimeKumaConfig{CallTimeout: Duration{5 * time.Second}},
		Scrutiny:      ScrutinyConfig{CallTimeout: Duration{5 * time.Second}},
	}
}

func Load(configPath, secretPath string) (Config, error) {
	return LoadLegacy(configPath, secretPath)
}

func LoadLegacy(configPath, secretPath string) (Config, error) {
	cfg := defaults()

	file, err := os.Open(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		if strings.Contains(err.Error(), "field ") && strings.Contains(err.Error(), "not found") {
			return Config{}, fmt.Errorf("decode configuration: unknown field: %w", err)
		}
		return Config{}, fmt.Errorf("decode configuration: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("decode configuration: multiple YAML documents are not allowed")
		}
		return Config{}, fmt.Errorf("decode configuration: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return Config{}, err
	}

	secret, err := readSecret(secretPath, "TrueNAS API key")
	if err != nil {
		return Config{}, err
	}
	cfg.APIKey = secret
	if cfg.QBittorrent.Enabled {
		cfg.QBittorrent.Password, err = readSecret(cfg.QBittorrent.SecretFile, "qBittorrent password")
		if err != nil {
			return Config{}, err
		}
	}
	if cfg.Plex.Enabled {
		cfg.Plex.Token, err = readSecret(cfg.Plex.SecretFile, "Plex token")
		if err != nil {
			return Config{}, err
		}
	}
	if cfg.Jellyfin.Enabled {
		cfg.Jellyfin.Token, err = readSecret(cfg.Jellyfin.SecretFile, "Jellyfin API key")
		if err != nil {
			return Config{}, err
		}
	}
	if cfg.UptimeKuma.Enabled {
		cfg.UptimeKuma.APIKey, err = readSecret(cfg.UptimeKuma.SecretFile, "Uptime Kuma API key")
		if err != nil {
			return Config{}, err
		}
	}
	if cfg.Weather.Enabled {
		cfg.Weather.APIHost, err = readSecret(cfg.Weather.APIHostFile, "QWeather API Host")
		if err != nil {
			return Config{}, err
		}
		if err := validateQWeatherHost(cfg.Weather.APIHost); err != nil {
			return Config{}, err
		}
		cfg.Weather.APIKey, err = readSecret(cfg.Weather.SecretFile, "QWeather API key")
		if err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

func validate(cfg *Config) error {
	if strings.TrimSpace(cfg.Server.Listen) == "" {
		return errors.New("server.listen must not be empty")
	}
	if cfg.Server.ReadTimeout.Duration <= 0 || cfg.Server.WriteTimeout.Duration <= 0 {
		return errors.New("server timeouts must be positive")
	}
	if cfg.TrueNAS.CallTimeout.Duration <= 0 {
		return errors.New("truenas.call_timeout must be positive")
	}
	if strings.TrimSpace(cfg.TrueNAS.Username) == "" {
		return errors.New("truenas.username must not be empty")
	}

	endpoint, err := url.Parse(cfg.TrueNAS.URL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "ws" && endpoint.Scheme != "wss") {
		return errors.New("truenas.url must be an absolute ws or wss URL")
	}
	if endpoint.User != nil {
		return errors.New("truenas.url must not contain credentials")
	}

	if _, err := time.LoadLocation(cfg.Dashboard.Timezone); err != nil {
		return errors.New("dashboard.timezone must be a valid IANA timezone")
	}
	if cfg.Dashboard.BackgroundStrength < 0 || cfg.Dashboard.BackgroundStrength > 1 {
		return errors.New("dashboard.background_strength must be between 0 and 1")
	}
	if err := validateDashboard(&cfg.Dashboard); err != nil {
		return err
	}

	minimums := []struct {
		name string
		got  time.Duration
		min  time.Duration
	}{
		{"realtime", cfg.Refresh.Realtime.Duration, 3 * time.Second},
		{"apps", cfg.Refresh.Apps.Duration, 15 * time.Second},
		{"alerts", cfg.Refresh.Alerts.Duration, 15 * time.Second},
		{"system", cfg.Refresh.System.Duration, 30 * time.Second},
		{"pools", cfg.Refresh.Pools.Duration, 30 * time.Second},
		{"disks", cfg.Refresh.Disks.Duration, 30 * time.Second},
		{"weather", cfg.Refresh.Weather.Duration, 5 * time.Minute},
		{"home_assistant", cfg.Refresh.HomeAssistant.Duration, 15 * time.Second},
		{"qbittorrent", cfg.Refresh.QBittorrent.Duration, 5 * time.Second},
		{"plex", cfg.Refresh.Plex.Duration, 5 * time.Second},
		{"jellyfin", cfg.Refresh.Jellyfin.Duration, 5 * time.Second},
		{"uptime_kuma", cfg.Refresh.UptimeKuma.Duration, 15 * time.Second},
		{"scrutiny", cfg.Refresh.Scrutiny.Duration, 5 * time.Minute},
	}
	for _, interval := range minimums {
		if interval.got < interval.min {
			return fmt.Errorf("refresh.%s must be at least %s", interval.name, interval.min)
		}
	}

	seenApps := make(map[string]struct{}, len(cfg.Apps))
	for _, app := range cfg.Apps {
		if strings.TrimSpace(app.ID) == "" {
			return errors.New("apps.id must not be empty")
		}
		if _, exists := seenApps[app.ID]; exists {
			return fmt.Errorf("apps.id %q is duplicated", app.ID)
		}
		seenApps[app.ID] = struct{}{}
		if app.Link != "" {
			link, err := url.Parse(app.Link)
			if err != nil || link.Host == "" || (link.Scheme != "http" && link.Scheme != "https") {
				return fmt.Errorf("apps link for %q must be an absolute http or https URL", app.ID)
			}
		}
	}

	if cfg.Weather.Units != "metric" && cfg.Weather.Units != "imperial" {
		return errors.New("weather.units must be metric or imperial")
	}
	if cfg.Weather.Latitude < -90 || cfg.Weather.Latitude > 90 {
		return errors.New("weather.latitude must be between -90 and 90")
	}
	if cfg.Weather.Longitude < -180 || cfg.Weather.Longitude > 180 {
		return errors.New("weather.longitude must be between -180 and 180")
	}
	if cfg.Weather.Enabled {
		if strings.TrimSpace(cfg.Weather.Name) == "" {
			return errors.New("weather.name must not be empty")
		}
		if strings.TrimSpace(cfg.Weather.APIHostFile) == "" {
			return errors.New("weather.api_host_file must not be empty")
		}
		if strings.TrimSpace(cfg.Weather.SecretFile) == "" {
			return errors.New("weather.secret_file must not be empty")
		}
		if cfg.Weather.CallTimeout.Duration <= 0 {
			return errors.New("weather.call_timeout must be positive")
		}
	}

	if cfg.HomeAssistant.Enabled {
		endpoint, err := url.Parse(cfg.HomeAssistant.URL)
		if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
			return errors.New("home_assistant.url must be an absolute http or https URL")
		}
		if endpoint.User != nil {
			return errors.New("home_assistant.url must not contain credentials")
		}
		if !strings.HasPrefix(strings.TrimSpace(cfg.HomeAssistant.FanEntityID), "fan.") {
			return errors.New("home_assistant.fan_entity_id must start with fan.")
		}
		if cfg.HomeAssistant.RemindAfter.Duration <= 0 {
			return errors.New("home_assistant.remind_after must be positive")
		}
		if cfg.HomeAssistant.CallTimeout.Duration <= 0 {
			return errors.New("home_assistant.call_timeout must be positive")
		}
	}
	if cfg.QBittorrent.Enabled {
		if strings.TrimSpace(cfg.QBittorrent.Username) == "" {
			return errors.New("qbittorrent.username must not be empty")
		}
		if err := validateHTTPSource("qbittorrent", cfg.QBittorrent.URL, cfg.QBittorrent.SecretFile, cfg.QBittorrent.CallTimeout.Duration); err != nil {
			return err
		}
	}
	if cfg.Plex.Enabled {
		if err := validateHTTPSource("plex", cfg.Plex.URL, cfg.Plex.SecretFile, cfg.Plex.CallTimeout.Duration); err != nil {
			return err
		}
	}
	if cfg.Jellyfin.Enabled {
		if err := validateHTTPSource("jellyfin", cfg.Jellyfin.URL, cfg.Jellyfin.SecretFile, cfg.Jellyfin.CallTimeout.Duration); err != nil {
			return err
		}
	}
	if cfg.UptimeKuma.Enabled {
		if err := validateHTTPSource("uptime_kuma", cfg.UptimeKuma.URL, cfg.UptimeKuma.SecretFile, cfg.UptimeKuma.CallTimeout.Duration); err != nil {
			return err
		}
	}
	if cfg.Scrutiny.Enabled {
		if err := validateHTTPURL("scrutiny", cfg.Scrutiny.URL, cfg.Scrutiny.CallTimeout.Duration); err != nil {
			return err
		}
	}

	return nil
}

func validateHTTPSource(name, rawURL, secretFile string, timeout time.Duration) error {
	if err := validateHTTPURL(name, rawURL, timeout); err != nil {
		return err
	}
	if strings.TrimSpace(secretFile) == "" {
		return fmt.Errorf("%s.secret_file must not be empty", name)
	}
	return nil
}

func validateHTTPURL(name, rawURL string, timeout time.Duration) error {
	endpoint, err := url.Parse(rawURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return fmt.Errorf("%s.url must be an absolute http or https URL", name)
	}
	if endpoint.User != nil {
		return fmt.Errorf("%s.url must not contain credentials", name)
	}
	if timeout <= 0 {
		return fmt.Errorf("%s.call_timeout must be positive", name)
	}
	return nil
}

func validateQWeatherHost(host string) error {
	host = strings.ToLower(strings.TrimSpace(host))
	if strings.ContainsAny(host, "/:@") || !strings.HasSuffix(host, ".qweatherapi.com") || len(strings.TrimSuffix(host, ".qweatherapi.com")) == 0 {
		return errors.New("QWeather API Host must be a dedicated qweatherapi.com hostname")
	}
	return nil
}

func validateDashboard(dashboard *DashboardConfig) error {
	if dashboard.Width < DashboardMinWidth || dashboard.Width > DashboardMaxWidth {
		return fmt.Errorf("dashboard.width must be between %d and %d", DashboardMinWidth, DashboardMaxWidth)
	}

	knownMetrics := map[string]struct{}{
		MetricTypeCPU: {}, MetricTypeCPUTemperature: {}, MetricTypeDiskTemperature: {}, MetricTypeNetwork: {}, MetricTypePoolCapacity: {},
	}
	seenMetrics := make(map[string]struct{}, len(dashboard.Metrics))
	for index, metric := range dashboard.Metrics {
		if _, ok := knownMetrics[metric.Type]; !ok {
			return fmt.Errorf("dashboard.metrics[%d].type %q is unknown", index, metric.Type)
		}
		if _, exists := seenMetrics[metric.Type]; exists {
			return fmt.Errorf("dashboard.metrics type %q is duplicated", metric.Type)
		}
		seenMetrics[metric.Type] = struct{}{}
		if metric.Type == MetricTypeNetwork && !ValidNetworkInterface(metric.Interface) {
			return fmt.Errorf("dashboard.metrics[%d].interface is invalid", index)
		}
		if metric.Type == MetricTypeDiskTemperature && !validDiskMatch(metric.Match) {
			return fmt.Errorf("dashboard.metrics[%d].match must set serial, model with size_bytes, or name", index)
		}
		if metric.Type == MetricTypePoolCapacity && !validHighThresholds(metric.WarningPercent, metric.CriticalPercent) {
			return fmt.Errorf("dashboard.metrics[%d] pool thresholds are invalid", index)
		}
	}

	knownActivities := map[string]struct{}{
		ActivityTypeTrueNASAlerts: {}, ActivityTypeAppHealth: {}, ActivityTypeAppUpdates: {},
		ActivityTypeHomeAssistantFan: {}, ActivityTypeQBittorrent: {}, ActivityTypePlex: {},
		ActivityTypeJellyfin: {}, ActivityTypeUptimeKuma: {},
		ActivityTypeWeather: {}, ActivityTypeMemoryPressure: {}, ActivityTypeSMARTExceptions: {}, ActivityTypeReplicationExceptions: {},
	}
	seenActivities := make(map[string]struct{}, len(dashboard.Activities))
	for index, activity := range dashboard.Activities {
		if _, ok := knownActivities[activity.Type]; !ok {
			return fmt.Errorf("dashboard.activities[%d].type %q is unknown", index, activity.Type)
		}
		if _, exists := seenActivities[activity.Type]; exists {
			return fmt.Errorf("dashboard.activities type %q is duplicated", activity.Type)
		}
		seenActivities[activity.Type] = struct{}{}
		if activity.Limit < 0 {
			return fmt.Errorf("dashboard.activities[%d].limit must not be negative", index)
		}
		if activity.Type == ActivityTypeMemoryPressure && !validLowThresholds(activity.WarningPercent, activity.CriticalPercent) {
			return fmt.Errorf("dashboard.activities[%d] memory thresholds are invalid", index)
		}
	}
	return nil
}

func validLowThresholds(warning, critical float64) bool {
	if warning == 0 && critical == 0 {
		return true
	}
	return warning > 0 && warning <= 100 && critical > 0 && critical < warning
}

func validHighThresholds(warning, critical float64) bool {
	if warning == 0 && critical == 0 {
		return true
	}
	return warning > 0 && warning < 100 && critical > warning && critical <= 100
}

func validDiskMatch(match DiskMatchConfig) bool {
	return strings.TrimSpace(match.Serial) != "" ||
		(strings.TrimSpace(match.Model) != "" && match.SizeBytes > 0) ||
		strings.TrimSpace(match.Name) != ""
}

func LoadHomeAssistantToken(path string) (string, error) {
	return readSecret(path, "Home Assistant token")
}

func readSecret(path, label string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect %s file: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s file must be a regular file", label)
	}
	permissions := info.Mode().Perm()
	if permissions&0o400 == 0 || permissions&0o077 != 0 {
		return "", fmt.Errorf("%s file must be owner-readable and mode 0600 or stricter", label)
	}
	if info.Size() > maxSecretBytes {
		return "", fmt.Errorf("%s file is too large", label)
	}

	value, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s file: %w", label, err)
	}
	secret := strings.TrimSpace(string(value))
	if secret == "" {
		return "", fmt.Errorf("%s file is empty", label)
	}
	return secret, nil
}
