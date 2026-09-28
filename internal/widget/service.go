package widget

import (
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

func NewService(registry *Registry, store *persist.Store) *Service {
	return &Service{registry: registry, store: store}
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
	for _, item := range s.Layout().Widgets {
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
	state := s.store.Snapshot()
	width := state.Server.Width
	if width < 300 || width > 720 {
		width = 360
	}
	widgets := append([]persist.Widget(nil), state.Widgets...)
	enabled := map[string]bool{}
	for _, item := range state.Integrations {
		enabled[item.ID] = item.Enabled
	}
	for index := range widgets {
		widgets[index].Config = cloneMap(widgets[index].Config)
		definition, _ := s.registry.Definition(widgets[index].DefinitionID)
		if definition.IntegrationType != "" && !enabled[widgets[index].IntegrationID] {
			widgets[index].Enabled = false
		}
	}
	sort.SliceStable(widgets, func(i, j int) bool { return widgets[i].Order < widgets[j].Order })
	return Layout{Width: width, Widgets: widgets}
}
func (s *Service) Update(layout Layout) error {
	if layout.Width < 300 || layout.Width > 720 {
		return errors.New("width must be between 300 and 720")
	}
	state := s.store.Snapshot()
	sources := map[string]string{}
	for _, item := range state.Integrations {
		if item.Enabled {
			sources[item.ID] = item.Type
		}
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
	return s.store.Update(func(state *persist.State) error {
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
	result := Layout{Width: value.Width, Widgets: make([]persist.Widget, len(value.Widgets))}
	for index, item := range value.Widgets {
		item.Config = cloneMap(item.Config)
		result.Widgets[index] = item
	}
	return result
}

// DashboardConfig adapts stable widget instances to the normalized dashboard
// configuration shared by the browser and Windows desktop renderers.
func (s *Service) DashboardConfig(base config.DashboardConfig) (config.DashboardConfig, error) {
	layout := s.Layout()
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
		switch definition.Placement {
		case PlacementMetric:
			metric := config.MetricConfig{Type: definition.LegacyType}
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
