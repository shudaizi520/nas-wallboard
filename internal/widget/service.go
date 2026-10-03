package widget

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/persist"
)

type Service struct {
	registry *Registry
	store    *persist.Store
}

const CurrentDefaultsVersion = 2

var defaultWidgetsByIntegration = map[string][]string{
	"truenas":        {"cpu", "cpu_temperature", "network", "truenas_alerts"},
	"qweather":       {"weather"},
	"plex":           {"plex"},
	"jellyfin":       {"jellyfin"},
	"qbittorrent":    {"qbittorrent"},
	"uptime_kuma":    {"uptime_kuma"},
	"home_assistant": {"home_assistant_fan"},
}

func NewService(registry *Registry, store *persist.Store) *Service {
	return &Service{registry: registry, store: store}
}

func (s *Service) ValidateRestored(store *persist.Store) (config.DashboardConfig, error) {
	candidate := NewService(s.registry, store)
	layout := Layout{Width: candidate.Layout().Width, Widgets: store.Snapshot().Widgets}
	if err := candidate.update(layout); err != nil {
		return config.DashboardConfig{}, err
	}
	return candidate.DashboardConfig(config.DashboardConfig{})
}

func (s *Service) AddIntegrationDefaults(state *persist.State, instance persist.Integration) error {
	definitions := defaultWidgetsByIntegration[instance.Type]
	integrationTypes := make(map[string]string, len(state.Integrations))
	for _, configured := range state.Integrations {
		integrationTypes[configured.ID] = configured.Type
	}
	for _, definitionID := range definitions {
		definition, ok := s.registry.Definition(definitionID)
		if !ok {
			return fmt.Errorf("default widget %q is not registered", definitionID)
		}
		alreadyPresent := false
		usedIDs := map[string]bool{}
		for index := range state.Widgets {
			item := &state.Widgets[index]
			usedIDs[item.ID] = true
			if item.DefinitionID == definitionID {
				alreadyPresent = true
				if definition.IntegrationType != "" && integrationTypes[item.IntegrationID] != definition.IntegrationType {
					item.IntegrationID = instance.ID
				}
			}
		}
		if alreadyPresent {
			continue
		}
		sequence := 1
		widgetID := fmt.Sprintf("%s-%d", definitionID, sequence)
		for usedIDs[widgetID] {
			sequence++
			widgetID = fmt.Sprintf("%s-%d", definitionID, sequence)
		}
		state.Widgets = append(state.Widgets, persist.Widget{
			ID: widgetID, DefinitionID: definitionID, IntegrationID: instance.ID,
			Enabled: true, Order: len(state.Widgets), Config: cloneMap(definition.Defaults),
		})
	}
	return nil
}

func (s *Service) MigrateDefaults() error {
	if s.store.Snapshot().WidgetDefaultsVersion >= CurrentDefaultsVersion {
		return nil
	}
	return s.store.Update(func(state *persist.State) error {
		if state.WidgetDefaultsVersion >= CurrentDefaultsVersion {
			return nil
		}
		if state.WidgetDefaultsVersion < 1 {
			for _, instance := range state.Integrations {
				if instance.Type == "home_assistant" {
					if err := s.AddIntegrationDefaults(state, instance); err != nil {
						return err
					}
				}
			}
		}
		if state.WidgetDefaultsVersion < 2 {
			moveAutoAppendedFanBeforePlex(state)
		}
		state.WidgetDefaultsVersion = CurrentDefaultsVersion
		return nil
	})
}

func moveAutoAppendedFanBeforePlex(state *persist.State) {
	sort.SliceStable(state.Widgets, func(i, j int) bool { return state.Widgets[i].Order < state.Widgets[j].Order })
	if len(state.Widgets) < 2 || state.Widgets[len(state.Widgets)-1].DefinitionID != "home_assistant_fan" {
		return
	}
	plexIndex := -1
	for index, item := range state.Widgets {
		if item.DefinitionID == "plex" {
			plexIndex = index
			break
		}
	}
	if plexIndex < 0 {
		return
	}
	fan := state.Widgets[len(state.Widgets)-1]
	ordered := make([]persist.Widget, 0, len(state.Widgets))
	ordered = append(ordered, state.Widgets[:plexIndex]...)
	ordered = append(ordered, fan)
	ordered = append(ordered, state.Widgets[plexIndex:len(state.Widgets)-1]...)
	for index := range ordered {
		ordered[index].Order = index
	}
	state.Widgets = ordered
}
func (s *Service) Catalog(capabilities map[string]bool) []Definition {
	return s.registry.Catalog(capabilities)
}

func (s *Service) CatalogForLayout(capabilities map[string]bool) []Definition {
	result := s.registry.Catalog(capabilities)
	seen := map[string]bool{}
	for _, definition := range result {
		seen[definition.ID] = true
	}
	for _, item := range s.store.Snapshot().Widgets {
		if seen[item.DefinitionID] {
			continue
		}
		if definition, ok := s.registry.Definition(item.DefinitionID); ok {
			result = append(result, definition)
			seen[item.DefinitionID] = true
		}
	}
	return result
}
func (s *Service) Layout() Layout {
	return s.layout(s.store.Snapshot())
}

var ErrLayoutConflict = errors.New("layout changed; synchronize before saving")

func layoutRevision(state persist.State) string {
	// Source settings and opaque credential references invalidate stale editors;
	// plaintext credentials are never loaded or returned to the client.
	sources := append([]persist.Integration(nil), state.Integrations...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
	data, _ := json.Marshal(struct {
		Width   int
		Widgets []persist.Widget
		Sources []persist.Integration
	}{state.Server.Width, state.Widgets, sources})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func (s *Service) layout(state persist.State) Layout {
	width := state.Server.Width
	if width < 300 || width > 720 {
		width = 360
	}
	widgets := append([]persist.Widget{}, state.Widgets...)
	sources := map[string]string{}
	for _, source := range state.Integrations {
		sources[source.ID] = source.Type
	}
	retained := widgets[:0]
	for _, item := range widgets {
		definition, _ := s.registry.Definition(item.DefinitionID)
		if definition.IntegrationType != "" && sources[item.IntegrationID] != definition.IntegrationType {
			continue
		}
		retained = append(retained, item)
	}
	widgets = retained
	for index := range widgets {
		widgets[index].Config = cloneMap(widgets[index].Config)
	}
	sort.SliceStable(widgets, func(i, j int) bool { return widgets[i].Order < widgets[j].Order })
	for index := range widgets {
		widgets[index].Order = index
	}
	return Layout{Revision: layoutRevision(state), Width: width, Widgets: widgets}
}
func (s *Service) Update(layout Layout) error {
	return s.update(layout)
}

func (s *Service) update(layout Layout) error {
	if layout.Width < 300 || layout.Width > 720 {
		return errors.New("width must be between 300 and 720")
	}
	return s.store.Update(func(state *persist.State) error {
		if layout.Revision != "" && layout.Revision != layoutRevision(*state) {
			return ErrLayoutConflict
		}
		sources := map[string]string{}
		for _, item := range state.Integrations {
			sources[item.ID] = item.Type
		}
		seenIDs := map[string]bool{}
		seenDefinitions := map[string]bool{}
		seenOrders := map[int]bool{}
		for index, item := range layout.Widgets {
			if !safeInstanceID.MatchString(item.ID) || seenIDs[item.ID] {
				return errors.New("invalid widget ID")
			}
			seenIDs[item.ID] = true
			definition, ok := s.registry.Definition(item.DefinitionID)
			if !ok {
				return errors.New("unknown widget definition")
			}
			if !definition.AllowMultiple && seenDefinitions[item.DefinitionID] {
				return errors.New("duplicate widget definition")
			}
			seenDefinitions[item.DefinitionID] = true
			if seenOrders[item.Order] || item.Order != index {
				return errors.New("widget order must be contiguous")
			}
			seenOrders[item.Order] = true
			if item.Enabled && definition.IntegrationType != "" && sources[item.IntegrationID] != definition.IntegrationType {
				return errors.New("widget source is unavailable")
			}
			if visibility, ok := item.Config["visibility"].(string); ok && Visibility(visibility) != definition.Visibility {
				return fmt.Errorf("widget %s visibility is fixed", item.ID)
			}
			configuration := integration.Config{}
			fieldKinds := map[string]integration.FieldKind{}
			for _, field := range definition.Fields {
				fieldKinds[field.Key] = field.Kind
			}
			for key, value := range item.Config {
				if key != "visibility" {
					if fieldKinds[key] == integration.FieldInteger {
						if parsed, valid := int64Value(value); valid {
							value = parsed
						}
					}
					configuration[key] = value
				}
			}
			if err := integration.ValidateFields(definition.Fields, configuration); err != nil {
				return fmt.Errorf("widget %s configuration: %w", item.ID, err)
			}
			if definition.ID == "network" && !config.ValidNetworkInterface(stringValue(item.Config["interface"])) {
				return fmt.Errorf("widget %s configuration: invalid network interface", item.ID)
			}
			warning, hasWarning := int64Value(item.Config["warning_percent"])
			critical, hasCritical := int64Value(item.Config["critical_percent"])
			if hasWarning && hasCritical {
				switch definition.ID {
				case "pool_capacity":
					if warning >= critical {
						return fmt.Errorf("widget %s configuration: warning threshold must be lower than critical threshold", item.ID)
					}
				case "memory_pressure":
					if critical >= warning {
						return fmt.Errorf("widget %s configuration: critical threshold must be lower than warning threshold", item.ID)
					}
				}
			}
		}
		state.Server.Width = layout.Width
		state.Widgets = make([]persist.Widget, len(layout.Widgets))
		for index, item := range layout.Widgets {
			item.Config = cloneMap(item.Config)
			state.Widgets[index] = item
		}
		return nil
	})
}
func (s *Service) MigrateLegacy(width int, metrics, activities []LegacyItem, sources map[string]string) (Layout, error) {
	mapping := map[string]string{"cpu": "cpu", "cpu_temperature": "cpu_temperature", "network": "network", "disk_temperature": "disk_temperature", "pool_capacity": "pool_capacity", "plex": "plex", "jellyfin": "jellyfin", "weather": "weather", "truenas_alerts": "truenas_alerts", "app_health": "app_health", "app_updates": "app_updates", "memory_pressure": "memory_pressure", "smart_exceptions": "smart_exceptions", "replication_exceptions": "replication_exceptions", "qbittorrent": "qbittorrent", "uptime_kuma": "uptime_kuma", "home_assistant_fan": "home_assistant_fan"}
	items := append(append([]LegacyItem{}, metrics...), activities...)
	layout := Layout{Width: width, Widgets: []persist.Widget{}}
	for index, item := range items {
		definitionID := mapping[item.Type]
		definition, ok := s.registry.Definition(definitionID)
		if !ok {
			continue
		}
		configuration := cloneMap(definition.Defaults)
		for key, value := range item.Config {
			configuration[key] = value
		}
		if item.Limit > 0 {
			configuration["limit"] = item.Limit
		}
		layout.Widgets = append(layout.Widgets, persist.Widget{ID: fmt.Sprintf("%s-%d", definitionID, index+1), DefinitionID: definitionID, IntegrationID: sources[definition.IntegrationType], Enabled: true, Order: len(layout.Widgets), Config: configuration})
	}
	if err := s.Update(layout); err != nil {
		return Layout{}, err
	}
	return s.Layout(), nil
}
func cloneMap(value map[string]any) map[string]any {
	result := map[string]any{}
	for key, item := range value {
		result[key] = item
	}
	return result
}
func cloneLayout(value Layout) Layout {
	result := Layout{Revision: value.Revision, Width: value.Width, Widgets: make([]persist.Widget, len(value.Widgets))}
	for index, item := range value.Widgets {
		item.Config = cloneMap(item.Config)
		result.Widgets[index] = item
	}
	return result
}

// DashboardConfig adapts stable widget instances to the normalized dashboard
// configuration shared by the browser and Windows desktop renderers.
func (s *Service) DashboardConfig(base config.DashboardConfig) (config.DashboardConfig, error) {
	state := s.store.Snapshot()
	layout := s.layout(state)
	enabled := map[string]bool{}
	for _, source := range state.Integrations {
		enabled[source.ID] = source.Enabled
	}
	result := base
	result.Width = layout.Width
	result.Metrics = []config.MetricConfig{}
	result.Activities = []config.ActivityConfig{}
	for _, item := range layout.Widgets {
		if !item.Enabled {
			continue
		}
		definition, ok := s.registry.Definition(item.DefinitionID)
		if !ok || definition.LegacyType == "" {
			continue
		}
		if definition.IntegrationType != "" && !enabled[item.IntegrationID] {
			continue
		}
		switch definition.Placement {
		case PlacementMetric:
			metric := config.MetricConfig{Type: definition.LegacyType}
			if definition.LegacyType == config.MetricTypeNetwork {
				metric.Interface = stringValue(item.Config["interface"])
			}
			if definition.LegacyType == config.MetricTypeDiskTemperature {
				metric.Match = config.DiskMatchConfig{
					Serial: stringValue(item.Config["serial"]),
					Model:  stringValue(item.Config["model"]),
					Name:   stringValue(item.Config["name"]),
				}
				if size, valid := int64Value(item.Config["size_bytes"]); valid && size >= 0 {
					metric.Match.SizeBytes = uint64(size)
				}
			}
			if definition.LegacyType == config.MetricTypePoolCapacity {
				metric.Name = stringValue(item.Config["name"])
				metric.WarningPercent = float64OrZero(item.Config["warning_percent"])
				metric.CriticalPercent = float64OrZero(item.Config["critical_percent"])
			}
			result.Metrics = append(result.Metrics, metric)
		case PlacementActivity:
			activity := config.ActivityConfig{Type: definition.LegacyType}
			if limit, ok := int64Value(item.Config["limit"]); ok && limit > 0 {
				activity.Limit = int(limit)
			}
			if definition.LegacyType == config.ActivityTypeMemoryPressure {
				activity.WarningPercent = float64OrZero(item.Config["warning_percent"])
				activity.CriticalPercent = float64OrZero(item.Config["critical_percent"])
			}
			result.Activities = append(result.Activities, activity)
		default:
			return config.DashboardConfig{}, fmt.Errorf("widget %s has invalid placement", item.ID)
		}
	}
	return result, nil
}

func float64OrZero(value any) float64 {
	if integer, ok := int64Value(value); ok {
		return float64(integer)
	}
	number, _ := value.(float64)
	return number
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func int64Value(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		if typed != float64(int64(typed)) {
			return 0, false
		}
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
