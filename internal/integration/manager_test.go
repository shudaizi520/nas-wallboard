package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/persist"
)

type managerDefinition struct {
	contractDefinition
	build func(Config, Secrets) (Collector, error)
}

func (definition managerDefinition) Collector(config Config, secrets Secrets) (Collector, error) {
	return definition.build(config, secrets)
}

type blockingCollector struct {
	started chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func (collector *blockingCollector) Run(ctx context.Context) error {
	collector.once.Do(func() { close(collector.started) })
	<-ctx.Done()
	close(collector.stopped)
	return nil
}

func waitClosed(t *testing.T, channel <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func managerFixture(t *testing.T, build func(Config, Secrets) (Collector, error)) (*Manager, *persist.Store, *persist.SecretStore) {
	t.Helper()
	root := t.TempDir()
	store, err := persist.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := persist.OpenSecrets(root)
	if err != nil {
		t.Fatal(err)
	}
	definition := validDefinition("plex", "Plex 媒体")
	registry := NewRegistry()
	if err := registry.Register(managerDefinition{contractDefinition: definition, build: build}); err != nil {
		t.Fatal(err)
	}
	return NewManager(registry, store, secrets), store, secrets
}

func TestManagerKeepsUnchangedCollectorAndRestartsChangedCollectorOnce(t *testing.T) {
	var mu sync.Mutex
	collectors := []*blockingCollector{}
	manager, store, _ := managerFixture(t, func(Config, Secrets) (Collector, error) {
		collector := &blockingCollector{started: make(chan struct{}), stopped: make(chan struct{})}
		mu.Lock()
		collectors = append(collectors, collector)
		mu.Unlock()
		return collector, nil
	})
	first := persist.Integration{ID: "one", Type: "plex", Enabled: true, Config: map[string]any{"url": "http://one.local"}}
	if err := store.Update(func(state *persist.State) error { state.Integrations = []persist.Integration{first}; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, collectors[0].started, "initial collector did not start")
	if err := manager.Apply(ctx, []persist.Integration{first}, []persist.Integration{first}); err != nil {
		t.Fatal(err)
	}
	if len(collectors) != 1 {
		t.Fatalf("unchanged collector restarted: %d builds", len(collectors))
	}
	changed := first
	changed.Config = map[string]any{"url": "http://two.local"}
	if err := manager.Apply(ctx, []persist.Integration{first}, []persist.Integration{changed}); err != nil {
		t.Fatal(err)
	}
	if len(collectors) != 2 {
		t.Fatalf("changed collector builds = %d", len(collectors))
	}
	waitClosed(t, collectors[1].started, "replacement collector did not start")
	waitClosed(t, collectors[0].stopped, "old collector did not stop")
	manager.Close()
}

func TestManagerStopsDisabledAndRemovedCollectorsWithoutStartingDisabledOnes(t *testing.T) {
	builds := 0
	var active *blockingCollector
	manager, _, _ := managerFixture(t, func(Config, Secrets) (Collector, error) {
		builds++
		active = &blockingCollector{started: make(chan struct{}), stopped: make(chan struct{})}
		return active, nil
	})
	ctx := context.Background()
	disabled := persist.Integration{ID: "disabled", Type: "plex", Enabled: false, Config: map[string]any{"url": "http://plex.local"}}
	if err := manager.Apply(ctx, nil, []persist.Integration{disabled}); err != nil {
		t.Fatal(err)
	}
	if builds != 0 {
		t.Fatalf("disabled collector builds = %d", builds)
	}
	enabled := disabled
	enabled.Enabled = true
	if err := manager.Apply(ctx, []persist.Integration{disabled}, []persist.Integration{enabled}); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, active.started, "enabled collector did not start")
	if err := manager.Apply(ctx, []persist.Integration{enabled}, []persist.Integration{disabled}); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, active.stopped, "disabled collector did not stop")
	if err := manager.Apply(ctx, []persist.Integration{disabled}, nil); err != nil {
		t.Fatal(err)
	}
	manager.Close()
}

func TestManagerBuildFailureDoesNotStopWorkingCollectorOrOtherInstances(t *testing.T) {
	working := &blockingCollector{started: make(chan struct{}), stopped: make(chan struct{})}
	manager, _, _ := managerFixture(t, func(config Config, _ Secrets) (Collector, error) {
		if config["url"] == "http://bad.local" {
			return nil, errors.New("contains upstream secret")
		}
		return working, nil
	})
	ctx := context.Background()
	current := persist.Integration{ID: "one", Type: "plex", Enabled: true, Config: map[string]any{"url": "http://good.local"}}
	if err := manager.Apply(ctx, nil, []persist.Integration{current}); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, working.started, "working collector did not start")
	broken := current
	broken.Config = map[string]any{"url": "http://bad.local"}
	if err := manager.Apply(ctx, []persist.Integration{current}, []persist.Integration{broken}); err == nil {
		t.Fatal("broken replacement unexpectedly succeeded")
	}
	select {
	case <-working.stopped:
		t.Fatal("working collector was stopped")
	default:
	}
	health := manager.Health()
	if len(health) != 1 || health[0].Healthy || health[0].Message == "contains upstream secret" {
		t.Fatalf("health = %#v", health)
	}
	manager.Close()
}

type handoverCollector struct {
	started  chan struct{}
	canceled chan struct{}
	release  chan struct{}
	publish  func()
}

func (c *handoverCollector) Run(ctx context.Context) error {
	close(c.started)
	<-ctx.Done()
	close(c.canceled)
	<-c.release
	c.publish()
	return nil
}

func TestManagerWaitsForOldFinalPublicationBeforeStartingReplacement(t *testing.T) {
	var mu sync.Mutex
	last := ""
	old := &handoverCollector{started: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{}), publish: func() { mu.Lock(); last = "old"; mu.Unlock() }}
	replacement := &publishingCollector{started: make(chan struct{}), publish: func() { mu.Lock(); last = "new"; mu.Unlock() }}
	manager, _, _ := managerFixture(t, func(config Config, _ Secrets) (Collector, error) {
		if config["url"] == "old" {
			return old, nil
		}
		return replacement, nil
	})
	first := persist.Integration{ID: "one", Type: "plex", Enabled: true, Config: map[string]any{"url": "old"}}
	if err := manager.Apply(context.Background(), nil, []persist.Integration{first}); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, old.started, "old collector did not start")
	second := first
	second.Config = map[string]any{"url": "new"}
	applied := make(chan error, 1)
	go func() {
		applied <- manager.Apply(context.Background(), []persist.Integration{first}, []persist.Integration{second})
	}()
	waitClosed(t, old.canceled, "old collector was not canceled")
	// If a replacement is started while the old collector drains, its new
	// value can be overwritten by the old request's final publication.
	select {
	case <-replacement.started:
		close(old.release)
		<-applied
		manager.Close()
		t.Fatal("replacement published before old collector exited")
	case <-time.After(50 * time.Millisecond):
	}
	close(old.release)
	if err := <-applied; err != nil {
		t.Fatal(err)
	}
	waitClosed(t, replacement.started, "replacement collector did not start")
	mu.Lock()
	got := last
	mu.Unlock()
	manager.Close()
	if got != "new" {
		t.Fatalf("last published value = %q, want new", got)
	}
}

type publishingCollector struct {
	started chan struct{}
	publish func()
}

func (c *publishingCollector) Run(ctx context.Context) error {
	c.publish()
	close(c.started)
	<-ctx.Done()
	return nil
}
