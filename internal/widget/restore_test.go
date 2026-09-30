package widget

import (
	"example.com/nas-wallboard/internal/persist"
	"testing"
)

func TestRestorePreservesWidgetSelectionForDisabledIntegration(t *testing.T) {
	root := t.TempDir()
	store, err := persist.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	original := persist.Widget{ID: "plex-main", DefinitionID: "plex", IntegrationID: "plex-source", Enabled: true, Order: 0, Config: map[string]any{"limit": 5}}
	if err = store.Update(func(s *persist.State) error {
		s.Server.Width = 375
		s.Integrations = []persist.Integration{{ID: "plex-source", Type: "plex", Enabled: false}}
		s.Widgets = []persist.Widget{original}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(registry, store)
	if _, err = service.ValidateRestored(store); err != nil {
		t.Fatal(err)
	}
	if !store.Snapshot().Widgets[0].Enabled {
		t.Fatal("restore overwrote saved widget preference")
	}
}
