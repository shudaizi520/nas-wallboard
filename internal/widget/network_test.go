package widget

import (
	"encoding/json"
	"testing"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/persist"
)

func TestNetworkInterfaceConfigurationPersistsAndRestores(t *testing.T) {
	root := t.TempDir()
	store, err := persist.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(s *persist.State) error {
		s.Integrations = []persist.Integration{{ID: "truenas-main", Type: "truenas", Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	registry, err := BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(registry, store)
	layout := Layout{Width: 375, Widgets: []persist.Widget{{ID: "network-1", DefinitionID: "network", IntegrationID: "truenas-main", Enabled: true, Order: 0, Config: map[string]any{"interface": "veth-peer.100:1"}}}}
	if err := service.Update(layout); err != nil {
		t.Fatal(err)
	}
	reopened, err := persist.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	restored := NewService(registry, reopened)
	if _, err := restored.ValidateRestored(reopened); err != nil {
		t.Fatal(err)
	}
	if restored.Layout().Width != 375 || restored.Layout().Widgets[0].Config["interface"] != "veth-peer.100:1" {
		t.Fatal("restore lost interface or layout")
	}
	adapted, err := restored.DashboardConfig(config.DashboardConfig{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(adapted.Metrics[0])
	var metric map[string]any
	_ = json.Unmarshal(raw, &metric)
	if metric["Interface"] != "veth-peer.100:1" {
		t.Fatalf("runtime metric lost selection: %s", raw)
	}
}

func TestNetworkInterfaceRejectsPathAndControlCharacters(t *testing.T) {
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(s *persist.State) error {
		s.Integrations = []persist.Integration{{ID: "truenas-main", Type: "truenas", Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	registry, err := BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(registry, store)
	for _, name := range []string{"../eth0", "eth0/other", "eth0\\other", "eth0\n", "eth 0", ".", ".."} {
		layout := Layout{Width: 360, Widgets: []persist.Widget{{ID: "network-1", DefinitionID: "network", IntegrationID: "truenas-main", Enabled: true, Config: map[string]any{"interface": name}}}}
		if err := service.Update(layout); err == nil {
			t.Fatalf("unsafe identifier accepted: %q", name)
		}
	}
}
