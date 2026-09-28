package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"example.com/nas-wallboard/internal/config"
)

const maxSettingsBytes = 32 * 1024

type Availability struct {
	Weather       bool
	HomeAssistant bool
	QBittorrent   bool
	Plex          bool
	Jellyfin      bool
	UptimeKuma    bool
}

type Settings struct {
	Width      int      `json:"width"`
	Metrics    []string `json:"metrics"`
	Activities []string `json:"activities"`
}

type SettingsItem struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
}

type SettingsView struct {
	Width      int            `json:"width"`
	MinWidth   int            `json:"min_width"`
	MaxWidth   int            `json:"max_width"`
	Metrics    []SettingsItem `json:"metrics"`
	Activities []SettingsItem `json:"activities"`
}

type settingsTemplate struct {
	id       string
	label    string
	metric   config.MetricConfig
	activity config.ActivityConfig
}

type SettingsManager struct {
	mu         sync.RWMutex
	path       string
	base       config.DashboardConfig
	current    Settings
	metrics    []settingsTemplate
	activities []settingsTemplate
	builder    *Builder
}

func NewSettingsManager(path string, base config.DashboardConfig, availability Availability, builder *Builder) (*SettingsManager, error) {
	if builder == nil {
		return nil, errors.New("dashboard builder is required")
	}
	manager := &SettingsManager{path: path, base: base, builder: builder}
	manager.metrics = metricCatalog(base)
	manager.activities = activityCatalog(base, availability)
	manager.current = settingsFromConfig(base)
	if path != "" {
		loaded, err := loadSettings(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("load dashboard settings: %w", err)
		}
		if err == nil {
			manager.current = loaded
		}
	}
	manager.current = availableSettings(manager.current, manager.metrics, manager.activities)
	configured, err := manager.resolve(manager.current)
	if err != nil {
		return nil, fmt.Errorf("validate dashboard settings: %w", err)
	}
	manager.current = settingsFromConfig(configured)
	if err := builder.Update(configured); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *SettingsManager) Current() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneSettings(m.current)
}

func (m *SettingsManager) Config() config.DashboardConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	resolved, _ := m.resolve(m.current)
	return resolved
}

func (m *SettingsManager) View() SettingsView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return SettingsView{
		Width: m.current.Width, MinWidth: config.DashboardMinWidth, MaxWidth: config.DashboardMaxWidth,
		Metrics: orderedItems(m.metrics, m.current.Metrics), Activities: orderedItems(m.activities, m.current.Activities),
	}
}

func (m *SettingsManager) Update(next Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	configured, err := m.resolve(next)
	if err != nil {
		return err
	}
	clean := settingsFromConfig(configured)
	if m.path != "" {
		if err := saveSettings(m.path, clean); err != nil {
			return fmt.Errorf("save dashboard settings: %w", err)
		}
	}
	if err := m.builder.Update(configured); err != nil {
		return err
	}
	m.current = clean
	return nil
}

func (m *SettingsManager) resolve(settings Settings) (config.DashboardConfig, error) {
	if settings.Width < config.DashboardMinWidth || settings.Width > config.DashboardMaxWidth {
		return config.DashboardConfig{}, fmt.Errorf("width must be between %d and %d", config.DashboardMinWidth, config.DashboardMaxWidth)
	}
	resolved := m.base
	resolved.Width = settings.Width
	resolved.Metrics = make([]config.MetricConfig, 0, len(settings.Metrics))
	seen := map[string]bool{}
	for _, id := range settings.Metrics {
		template, ok := findTemplate(m.metrics, id)
		if !ok || seen[id] {
			return config.DashboardConfig{}, fmt.Errorf("metric %q is unavailable or duplicated", id)
		}
		seen[id] = true
		resolved.Metrics = append(resolved.Metrics, template.metric)
	}
	resolved.Activities = make([]config.ActivityConfig, 0, len(settings.Activities))
	seen = map[string]bool{}
	for _, id := range settings.Activities {
		template, ok := findTemplate(m.activities, id)
		if !ok || seen[id] {
			return config.DashboardConfig{}, fmt.Errorf("activity %q is unavailable or duplicated", id)
		}
		seen[id] = true
		resolved.Activities = append(resolved.Activities, template.activity)
	}
	return resolved, nil
}

func metricCatalog(base config.DashboardConfig) []settingsTemplate {
	disk := config.MetricConfig{Type: config.MetricTypeDiskTemperature}
	for _, item := range base.Metrics {
		if item.Type == config.MetricTypeDiskTemperature {
			disk = item
		}
	}
	items := []settingsTemplate{
		{id: config.MetricTypeCPU, label: "处理器", metric: config.MetricConfig{Type: config.MetricTypeCPU}},
		{id: config.MetricTypeCPUTemperature, label: "处理器温度", metric: config.MetricConfig{Type: config.MetricTypeCPUTemperature}},
		{id: config.MetricTypeNetwork, label: "网速", metric: config.MetricConfig{Type: config.MetricTypeNetwork}},
	}
	if disk.Match.Serial != "" || disk.Match.Model != "" || disk.Match.Name != "" {
		items = append(items, settingsTemplate{id: config.MetricTypeDiskTemperature, label: "机械硬盘", metric: disk})
	}
	return items
}

func activityCatalog(base config.DashboardConfig, availability Availability) []settingsTemplate {
	configured := map[string]config.ActivityConfig{}
	for _, item := range base.Activities {
		configured[item.Type] = item
	}
	add := func(items []settingsTemplate, id, label string, available bool, defaultLimit int) []settingsTemplate {
		value, configuredAlready := configured[id]
		if !available {
			return items
		}
		if !configuredAlready {
			value = config.ActivityConfig{Type: id, Limit: defaultLimit}
		}
		return append(items, settingsTemplate{id: id, label: label, activity: value})
	}
	items := []settingsTemplate{}
	items = add(items, config.ActivityTypeWeather, "天气", availability.Weather, 0)
	items = add(items, config.ActivityTypePlex, "Plex", availability.Plex, 0)
	items = add(items, config.ActivityTypeUptimeKuma, "网站状态", availability.UptimeKuma, 0)
	items = add(items, config.ActivityTypeHomeAssistantFan, "小米风扇", availability.HomeAssistant, 0)
	items = add(items, config.ActivityTypeAppUpdates, "应用更新", true, 0)
	items = add(items, config.ActivityTypeAppHealth, "应用异常", true, 0)
	items = add(items, config.ActivityTypeTrueNASAlerts, "NAS 告警", true, 2)
	items = add(items, config.ActivityTypeQBittorrent, "下载状态", availability.QBittorrent, 0)
	items = add(items, config.ActivityTypeJellyfin, "Jellyfin", availability.Jellyfin, 0)
	return items
}

func availableSettings(settings Settings, metrics, activities []settingsTemplate) Settings {
	clean := Settings{Width: settings.Width, Metrics: make([]string, 0, len(settings.Metrics)), Activities: make([]string, 0, len(settings.Activities))}
	for _, id := range settings.Metrics {
		if _, ok := findTemplate(metrics, id); ok {
			clean.Metrics = append(clean.Metrics, id)
		}
	}
	for _, id := range settings.Activities {
		if _, ok := findTemplate(activities, id); ok {
			clean.Activities = append(clean.Activities, id)
		}
	}
	return clean
}

func findTemplate(items []settingsTemplate, id string) (settingsTemplate, bool) {
	for _, item := range items {
		if item.id == id {
			return item, true
		}
	}
	return settingsTemplate{}, false
}

func orderedItems(catalog []settingsTemplate, selected []string) []SettingsItem {
	result := make([]SettingsItem, 0, len(catalog))
	used := map[string]bool{}
	for _, id := range selected {
		if item, ok := findTemplate(catalog, id); ok {
			result = append(result, SettingsItem{ID: item.id, Label: item.label, Enabled: true})
			used[id] = true
		}
	}
	for _, item := range catalog {
		if !used[item.id] {
			result = append(result, SettingsItem{ID: item.id, Label: item.label})
		}
	}
	return result
}

func settingsFromConfig(cfg config.DashboardConfig) Settings {
	settings := Settings{Width: cfg.Width, Metrics: make([]string, 0, len(cfg.Metrics)), Activities: make([]string, 0, len(cfg.Activities))}
	if settings.Width == 0 {
		settings.Width = config.DashboardDefaultWidth
	}
	for _, item := range cfg.Metrics {
		settings.Metrics = append(settings.Metrics, item.Type)
	}
	for _, item := range cfg.Activities {
		settings.Activities = append(settings.Activities, item.Type)
	}
	return settings
}

func cloneSettings(value Settings) Settings {
	return Settings{Width: value.Width, Metrics: append([]string(nil), value.Metrics...), Activities: append([]string(nil), value.Activities...)}
}

func loadSettings(path string) (Settings, error) {
	file, err := os.Open(path)
	if err != nil {
		return Settings{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxSettingsBytes))
	decoder.DisallowUnknownFields()
	var settings Settings
	if err := decoder.Decode(&settings); err != nil {
		return Settings{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Settings{}, errors.New("multiple settings documents are not allowed")
	}
	return settings, nil
}

func saveSettings(path string, settings Settings) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".dashboard-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(settings); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}
