package widget

import (
	"encoding/json"
	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/persist"
	"sync"
	"testing"
)

func TestDisabledSourceRetainsStoredDisplayPreference(t *testing.T) {
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.Update(func(s *persist.State) error {
		s.Server.Width = 360
		s.Integrations = []persist.Integration{{ID: "plex-main", Type: "plex", Enabled: false}}
		s.Widgets = []persist.Widget{{ID: "plex-1", DefinitionID: "plex", IntegrationID: "plex-main", Enabled: true, Config: map[string]any{"limit": 3}}}
		return nil
	})
	service := NewService(testRegistry(t), store)
	layout := service.Layout()
	if !layout.Widgets[0].Enabled {
		t.Fatal("disabled source erased saved preference")
	}
	layout.Width = 500
	if err := service.Update(layout); err != nil {
		t.Fatal(err)
	}
	dashboard, err := service.DashboardConfig(config.DashboardConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if len(dashboard.Activities) != 0 {
		t.Fatal("disabled source rendered")
	}
	store.Update(func(s *persist.State) error { s.Integrations[0].Enabled = true; return nil })
	dashboard, _ = service.DashboardConfig(config.DashboardConfig{})
	if len(dashboard.Activities) != 1 {
		t.Fatal("reenabled source did not restore display choice")
	}
}

func TestLayoutRevisionCheckAndWriteAreAtomic(t *testing.T) {
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(testRegistry(t), store)
	layout := service.Layout()
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, width := range []int{500, 600} {
		workers.Add(1)
		go func(width int) {
			defer workers.Done()
			candidate := layout
			candidate.Width = width
			<-start
			results <- service.Update(candidate)
		}(width)
	}
	close(start)
	workers.Wait()
	close(results)
	successes := 0
	conflicts := 0
	for err := range results {
		if err == nil {
			successes++
		} else if err == ErrLayoutConflict {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successful=%d conflicts=%d", successes, conflicts)
	}
}

func TestIntegrationConfigurationChangeInvalidatesLayoutRevision(t *testing.T) {
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(s *persist.State) error {
		s.Integrations = []persist.Integration{{ID: "plex-main", Type: "plex", Enabled: true, Config: map[string]any{"url": "http://old.local"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(testRegistry(t), store)
	stale := service.Layout()
	if err := store.Update(func(s *persist.State) error { s.Integrations[0].Config["url"] = "http://new.local"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := service.Update(stale); err != ErrLayoutConflict {
		t.Fatalf("configuration change accepted stale layout: %v", err)
	}
}

func TestEmptyManagementLayoutReturnsAnEditableWidgetList(t *testing.T) {
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(testRegistry(t), store)
	if service.Layout().Widgets == nil {
		t.Fatal("empty widget list becomes JSON null and breaks editor initialization")
	}
}

func TestLayoutRevisionRejectsConcurrentAndSourceChanges(t *testing.T) {
	store, err := persist.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(testRegistry(t), store)
	first := service.Layout()
	data, _ := json.Marshal(first)
	var fields map[string]any
	json.Unmarshal(data, &fields)
	if fields["revision"] == nil || fields["revision"] == "" {
		t.Fatal("management layout has no revision")
	}
	second := first
	second.Width = 500
	if err := service.Update(second); err != nil {
		t.Fatal(err)
	}
	first.Width = 600
	if err := service.Update(first); err == nil {
		t.Fatal("stale write overwrote newer layout")
	}
	current := service.Layout()
	store.Update(func(s *persist.State) error {
		s.Integrations = append(s.Integrations, persist.Integration{ID: "plex-main", Type: "plex", Enabled: true})
		return nil
	})
	if err := service.Update(current); err == nil {
		t.Fatal("source change did not invalidate stale layout")
	}
	if store.Snapshot().Server.Width != 500 {
		t.Fatal("conflict mutated stored width")
	}
}
