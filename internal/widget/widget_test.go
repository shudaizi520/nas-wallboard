package widget

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/persist"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	registry := NewRegistry()
	for _, definition := range []Definition{
		{ID: "cpu", LegacyType: "cpu", Placement: PlacementMetric, Label: "处理器", Visibility: VisibilityAlways, Defaults: map[string]any{}},
		{ID: "disk_temperature", LegacyType: "disk_temperature", IntegrationType: "truenas", Placement: PlacementMetric, Label: "硬盘温度", Visibility: VisibilityAlways, AllowMultiple: true, Fields: []integration.Field{{Key: "serial", Kind: integration.FieldText}}, Defaults: map[string]any{}},
		{ID: "plex", LegacyType: "plex", IntegrationType: "plex", Placement: PlacementActivity, Label: "Plex 播放", Visibility: VisibilityNonEmpty, Fields: []integration.Field{{Key: "limit", Kind: integration.FieldInteger}}, Defaults: map[string]any{"limit": 3}},
		{ID: "home_assistant_fan", LegacyType: "home_assistant_fan", IntegrationType: "home_assistant", Placement: PlacementActivity, Label: "设备提醒", Visibility: VisibilityNonEmpty, Defaults: map[string]any{}},
		{ID: "truenas_alerts", LegacyType: "truenas_alerts", IntegrationType: "truenas", Placement: PlacementActivity, Label: "NAS 告警", Visibility: VisibilityWarningOnly, Fields: []integration.Field{{Key: "limit", Kind: integration.FieldInteger}}, Defaults: map[string]any{"limit": 2}},
	} {
		if err := registry.Register(definition); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

func TestServiceMigratesLegacyOrderAndPreservesDiskMatch(t *testing.T) {
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(state *persist.State) error {
		state.Integrations = []persist.Integration{{ID: "truenas-main", Type: "truenas", Enabled: true}, {ID: "plex-main", Type: "plex", Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(testRegistry(t), store)
	layout, err := service.MigrateLegacy(420,
		[]LegacyItem{{Type: "cpu"}, {Type: "disk_temperature", Config: map[string]any{"serial": "ABC"}}},
		[]LegacyItem{{Type: "plex", Limit: 4}, {Type: "truenas_alerts", Limit: 2}}, map[string]string{"truenas": "truenas-main", "plex": "plex-main"})
	if err != nil {
		t.Fatal(err)
	}
	if layout.Width != 420 || len(layout.Widgets) != 4 {
		t.Fatalf("layout = %#v", layout)
	}
	if got := []string{layout.Widgets[0].DefinitionID, layout.Widgets[1].DefinitionID, layout.Widgets[2].DefinitionID, layout.Widgets[3].DefinitionID}; !reflect.DeepEqual(got, []string{"cpu", "disk_temperature", "plex", "truenas_alerts"}) {
		t.Fatalf("order = %#v", got)
	}
	if layout.Widgets[1].Config["serial"] != "ABC" || fmt.Sprint(layout.Widgets[2].Config["limit"]) != "4" {
		t.Fatalf("configs = %#v %#v", layout.Widgets[1].Config, layout.Widgets[2].Config)
	}
}

func TestServiceAddsDefaultWidgetForNewHomeAssistantIntegration(t *testing.T) {
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(testRegistry(t), store)
	instance := persist.Integration{ID: "home-main", Type: "home_assistant", Enabled: true}
	if err := store.Update(func(state *persist.State) error {
		state.Integrations = append(state.Integrations, instance)
		return service.AddIntegrationDefaults(state, instance)
	}); err != nil {
		t.Fatal(err)
	}
	widgets := store.Snapshot().Widgets
	if len(widgets) != 1 || widgets[0].DefinitionID != "home_assistant_fan" || widgets[0].IntegrationID != "home-main" || !widgets[0].Enabled || widgets[0].Order != 0 {
		t.Fatalf("widgets = %#v", widgets)
	}
	if err := store.Update(func(state *persist.State) error { return service.AddIntegrationDefaults(state, instance) }); err != nil {
		t.Fatal(err)
	}
	if got := len(store.Snapshot().Widgets); got != 1 {
		t.Fatalf("duplicate default widgets = %d", got)
	}
}

func TestServiceRebindsDefaultWidgetWhenIntegrationIsRecreated(t *testing.T) {
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(testRegistry(t), store)
	if err := store.Update(func(state *persist.State) error {
		state.Widgets = []persist.Widget{{
			ID: "plex-1", DefinitionID: "plex", IntegrationID: "removed-plex",
			Enabled: true, Order: 0, Config: map[string]any{"limit": 4},
		}}
		instance := persist.Integration{ID: "replacement-plex", Type: "plex", Enabled: true}
		state.Integrations = []persist.Integration{instance}
		return service.AddIntegrationDefaults(state, instance)
	}); err != nil {
		t.Fatal(err)
	}
	widgets := store.Snapshot().Widgets
	if len(widgets) != 1 {
		t.Fatalf("widgets = %#v", widgets)
	}
	if widgets[0].IntegrationID != "replacement-plex" || !widgets[0].Enabled || fmt.Sprint(widgets[0].Config["limit"]) != "4" {
		t.Fatalf("rebound widget = %#v", widgets[0])
	}
}

func TestServiceMigratesMissingHomeAssistantWidgetOnlyOnce(t *testing.T) {
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(state *persist.State) error {
		state.WidgetDefaultsVersion = 0
		state.Integrations = []persist.Integration{{ID: "home-main", Type: "home_assistant", Enabled: true}}
		state.Widgets = []persist.Widget{{ID: "cpu-1", DefinitionID: "cpu", Enabled: true, Order: 0}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(testRegistry(t), store)
	if err := service.MigrateDefaults(); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	if state.WidgetDefaultsVersion != CurrentDefaultsVersion || len(state.Widgets) != 2 || state.Widgets[1].DefinitionID != "home_assistant_fan" {
		t.Fatalf("migrated state = %#v", state)
	}
	if err := store.Update(func(state *persist.State) error {
		state.Widgets = state.Widgets[:1]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.MigrateDefaults(); err != nil {
		t.Fatal(err)
	}
	if got := len(store.Snapshot().Widgets); got != 1 {
		t.Fatalf("manual removal was undone: %d widgets", got)
	}
}

func TestServiceMovesAutoAppendedFanBeforePlexOnce(t *testing.T) {
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(state *persist.State) error {
		state.WidgetDefaultsVersion = 1
		state.Integrations = []persist.Integration{
			{ID: "plex-main", Type: "plex", Enabled: true},
			{ID: "home-main", Type: "home_assistant", Enabled: true},
		}
		state.Widgets = []persist.Widget{
			{ID: "cpu-1", DefinitionID: "cpu", Enabled: true, Order: 0},
			{ID: "plex-1", DefinitionID: "plex", IntegrationID: "plex-main", Enabled: true, Order: 1},
			{ID: "fan-1", DefinitionID: "home_assistant_fan", IntegrationID: "home-main", Enabled: true, Order: 2},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(testRegistry(t), store)
	if err := service.MigrateDefaults(); err != nil {
		t.Fatal(err)
	}
	layout := service.Layout()
	got := []string{layout.Widgets[0].DefinitionID, layout.Widgets[1].DefinitionID, layout.Widgets[2].DefinitionID}
	if !reflect.DeepEqual(got, []string{"cpu", "home_assistant_fan", "plex"}) {
		t.Fatalf("migrated widget order = %#v", got)
	}
	if store.Snapshot().WidgetDefaultsVersion != CurrentDefaultsVersion {
		t.Fatalf("defaults version = %d, want %d", store.Snapshot().WidgetDefaultsVersion, CurrentDefaultsVersion)
	}
}

func TestServiceValidatesWidthDuplicatesSourcesVisibilityAndRemoval(t *testing.T) {
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(state *persist.State) error {
		state.Integrations = []persist.Integration{{ID: "plex-main", Type: "plex", Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(testRegistry(t), store)
	valid := Layout{Width: 360, Widgets: []persist.Widget{{ID: "cpu-1", DefinitionID: "cpu", Enabled: true, Order: 0}, {ID: "plex-1", DefinitionID: "plex", IntegrationID: "plex-main", Enabled: true, Order: 1, Config: map[string]any{"visibility": "non_empty", "limit": 3}}}}
	if err := service.Update(valid); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Layout){
		"width": func(value *Layout) { value.Width = 900 },
		"duplicate": func(value *Layout) {
			value.Widgets = append(value.Widgets, persist.Widget{ID: "cpu-2", DefinitionID: "cpu", Order: 2})
		},
		"source":     func(value *Layout) { value.Widgets[1].IntegrationID = "missing" },
		"visibility": func(value *Layout) { value.Widgets[1].Config["visibility"] = "always" },
		"config":     func(value *Layout) { value.Widgets[1].Config["limit"] = "many" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneLayout(valid)
			mutate(&candidate)
			if err := service.Update(candidate); err == nil {
				t.Fatal("invalid layout accepted")
			}
		})
	}
	if err := store.Update(func(state *persist.State) error { state.Integrations = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	clean := service.Layout()
	if clean.Widgets[1].Enabled {
		t.Fatal("widget with removed source remained enabled")
	}
	if err := service.Update(clean); err != nil {
		t.Fatalf("disabled widget with removed source should remain editable: %v", err)
	}
}

func TestDashboardConfigPreservesWidgetOrderDiskMatchAndActivityLimit(t *testing.T) {
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(state *persist.State) error {
		state.Server.Width = 300
		state.Integrations = []persist.Integration{{ID: "truenas-main", Type: "truenas", Enabled: true}, {ID: "plex-main", Type: "plex", Enabled: true}}
		state.Widgets = []persist.Widget{
			{ID: "disk-1", DefinitionID: "disk_temperature", IntegrationID: "truenas-main", Enabled: true, Order: 0, Config: map[string]any{"serial": "SER-1", "model": "MODEL-1", "size_bytes": json.Number("14000519643136"), "name": "sda"}},
			{ID: "cpu-1", DefinitionID: "cpu", Enabled: true, Order: 1},
			{ID: "plex-1", DefinitionID: "plex", IntegrationID: "plex-main", Enabled: true, Order: 2, Config: map[string]any{"limit": json.Number("4")}},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(testRegistry(t), store)
	got, err := service.DashboardConfig(config.DashboardConfig{Title: "NAS", Width: 300})
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 300 || len(got.Metrics) != 2 || got.Metrics[0].Type != config.MetricTypeDiskTemperature || got.Metrics[1].Type != config.MetricTypeCPU {
		t.Fatalf("metrics = %#v", got.Metrics)
	}
	if match := got.Metrics[0].Match; match.Serial != "SER-1" || match.Model != "MODEL-1" || match.SizeBytes != 14000519643136 || match.Name != "sda" {
		t.Fatalf("disk match = %#v", match)
	}
	if len(got.Activities) != 1 || got.Activities[0].Type != config.ActivityTypePlex || got.Activities[0].Limit != 4 {
		t.Fatalf("activities = %#v", got.Activities)
	}
}

func TestCatalogFiltersUnavailableDefinitionsAndLayoutCopiesDefensively(t *testing.T) {
	store, _ := persist.Open(t.TempDir())
	service := NewService(testRegistry(t), store)
	items := service.Catalog(map[string]bool{"truenas": true})
	if got := []string{items[0].ID, items[1].ID, items[2].ID}; !reflect.DeepEqual(got, []string{"cpu", "disk_temperature", "truenas_alerts"}) {
		t.Fatalf("catalog = %#v", got)
	}
	layout := service.Layout()
	layout.Width = 999
	if service.Layout().Width == 999 {
		t.Fatal("layout was not copied")
	}
}

func TestCatalogForLayoutRetainsDefinitionAfterItsSourceIsRemoved(t *testing.T) {
	store, _ := persist.Open(t.TempDir())
	if err := store.Update(func(state *persist.State) error {
		state.Widgets = []persist.Widget{{ID: "plex-1", DefinitionID: "plex", IntegrationID: "removed", Enabled: true, Order: 0}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	items := NewService(testRegistry(t), store).CatalogForLayout(map[string]bool{})
	if len(items) != 2 || items[0].ID != "cpu" || items[1].ID != "plex" {
		t.Fatalf("catalog = %#v", items)
	}
}

func TestDefaultTrueNASWidgetsAreStableAndUseful(t *testing.T) {
	items := DefaultTrueNASWidgets("truenas-main")
	if got := []string{items[0].DefinitionID, items[1].DefinitionID, items[2].DefinitionID, items[3].DefinitionID}; !reflect.DeepEqual(got, []string{"cpu", "cpu_temperature", "network", "truenas_alerts"}) {
		t.Fatalf("defaults = %#v", got)
	}
	for index, item := range items {
		if item.Order != index || item.IntegrationID != "truenas-main" || !item.Enabled {
			t.Fatalf("default %d = %#v", index, item)
		}
	}
}

func TestBuiltInRegistryIncludesTrueNASStorageAndProtectionWidgets(t *testing.T) {
	registry, err := BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	items := registry.Catalog(map[string]bool{"truenas": true})
	ids := map[string]bool{}
	for _, item := range items {
		ids[item.ID] = true
	}
	for _, id := range []string{"pool_capacity", "memory_pressure", "smart_exceptions", "replication_exceptions"} {
		if !ids[id] {
			t.Errorf("missing widget %q", id)
		}
	}
}

func TestServiceRejectsInvertedStorageAndMemoryThresholds(t *testing.T) {
	registry, err := BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(state *persist.State) error {
		state.Integrations = []persist.Integration{{ID: "truenas-main", Type: "truenas", Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(registry, store)
	for name, item := range map[string]persist.Widget{
		"pool": {
			ID: "pool-1", DefinitionID: "pool_capacity", IntegrationID: "truenas-main", Enabled: true,
			Config: map[string]any{"name": "tank", "warning_percent": 95, "critical_percent": 90},
		},
		"memory": {
			ID: "memory-1", DefinitionID: "memory_pressure", IntegrationID: "truenas-main", Enabled: true,
			Config: map[string]any{"warning_percent": 8, "critical_percent": 15},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := service.Update(Layout{Width: 360, Widgets: []persist.Widget{item}}); err == nil {
				t.Fatal("inverted thresholds accepted")
			}
		})
	}
}
