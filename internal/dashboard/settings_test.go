package dashboard

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"example.com/nas-wallboard/internal/config"
)

func TestSettingsManagerPersistsSafeDisplayChoices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.json")
	base := config.DashboardConfig{
		Width: 460,
		Metrics: []config.MetricConfig{
			{Type: config.MetricTypeCPU},
			{Type: config.MetricTypeDiskTemperature, Match: config.DiskMatchConfig{Model: "14T", SizeBytes: 14_000}},
		},
		Activities: []config.ActivityConfig{{Type: config.ActivityTypeWeather}, {Type: config.ActivityTypePlex}},
	}
	builder, err := New(base)
	if err != nil {
		t.Fatal(err)
	}
	availability := Availability{Weather: true, Plex: true, UptimeKuma: true}
	manager, err := NewSettingsManager(path, base, availability, builder)
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{Width: 500, Metrics: []string{config.MetricTypeNetwork, config.MetricTypeDiskTemperature}, Activities: []string{config.ActivityTypeUptimeKuma, config.ActivityTypeWeather}}
	if err := manager.Update(want); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings file = %#v, %v", info, err)
	}

	reloadedBuilder, _ := New(base)
	reloaded, err := NewSettingsManager(path, base, availability, reloadedBuilder)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Current(); !reflect.DeepEqual(got, want) {
		t.Fatalf("reloaded settings = %#v, want %#v", got, want)
	}
}

func TestSettingsManagerRejectsUnavailableAndPreservesDiskMatch(t *testing.T) {
	base := config.DashboardConfig{
		Width:   460,
		Metrics: []config.MetricConfig{{Type: config.MetricTypeDiskTemperature, Match: config.DiskMatchConfig{Model: "14T", SizeBytes: 14_000}}},
	}
	builder, _ := New(base)
	manager, err := NewSettingsManager(filepath.Join(t.TempDir(), "dashboard.json"), base, Availability{}, builder)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Update(Settings{Width: 460, Activities: []string{config.ActivityTypePlex}}); err == nil {
		t.Fatal("unavailable Plex must be rejected")
	}
	if err := manager.Update(Settings{Width: 460, Metrics: []string{config.MetricTypeDiskTemperature}}); err != nil {
		t.Fatal(err)
	}
	if got := manager.Config().Metrics[0].Match; got.Model != "14T" || got.SizeBytes != 14_000 {
		t.Fatalf("disk match = %#v", got)
	}
}

func TestSettingsManagerCatalogIncludesConfiguredSources(t *testing.T) {
	base := config.DashboardConfig{Width: 460, Metrics: []config.MetricConfig{
		{Type: config.MetricTypeCPU},
		{Type: config.MetricTypeDiskTemperature, Match: config.DiskMatchConfig{Model: "14T", SizeBytes: 14_000}},
	}}
	builder, _ := New(base)
	manager, err := NewSettingsManager(filepath.Join(t.TempDir(), "dashboard.json"), base, Availability{Weather: true, HomeAssistant: true, Plex: true}, builder)
	if err != nil {
		t.Fatal(err)
	}
	view := manager.View()
	if len(view.Metrics) != 4 {
		t.Fatalf("metric catalog = %#v", view.Metrics)
	}
	foundCPUTemperature := false
	for _, item := range view.Metrics {
		if item.ID == "cpu_temperature" && item.Label == "处理器温度" {
			foundCPUTemperature = true
		}
	}
	if !foundCPUTemperature {
		t.Fatalf("CPU temperature missing from metric catalog: %#v", view.Metrics)
	}
	wantActivities := map[string]bool{config.ActivityTypeWeather: true, config.ActivityTypeHomeAssistantFan: true, config.ActivityTypePlex: true}
	for _, item := range view.Activities {
		delete(wantActivities, item.ID)
	}
	if len(wantActivities) != 0 {
		t.Fatalf("missing activities = %#v", wantActivities)
	}
}

func TestSettingsManagerHidesUnavailableConfiguredSourceAndDropsItsSavedSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.json")
	if err := os.WriteFile(path, []byte(`{
  "width": 460,
  "metrics": ["cpu"],
  "activities": ["home_assistant_fan", "plex", "app_health"]
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	base := config.DashboardConfig{
		Width:   460,
		Metrics: []config.MetricConfig{{Type: config.MetricTypeCPU}},
		Activities: []config.ActivityConfig{
			{Type: config.ActivityTypeHomeAssistantFan},
			{Type: config.ActivityTypePlex},
			{Type: config.ActivityTypeAppHealth},
		},
	}
	builder, err := New(base)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewSettingsManager(path, base, Availability{Plex: true}, builder)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range manager.View().Activities {
		if item.ID == config.ActivityTypeHomeAssistantFan {
			t.Fatalf("unavailable Home Assistant item is visible: %#v", item)
		}
	}
	want := []string{config.ActivityTypePlex, config.ActivityTypeAppHealth}
	if got := manager.Current().Activities; !reflect.DeepEqual(got, want) {
		t.Fatalf("activities = %#v, want %#v", got, want)
	}
}
