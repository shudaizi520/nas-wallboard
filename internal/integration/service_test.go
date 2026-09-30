package integration

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/persist"
)

type serviceDefinition struct {
	contractDefinition
	probe func(context.Context, Config, Secrets) ProbeResult
}

func (definition serviceDefinition) Test(ctx context.Context, config Config, secrets Secrets) ProbeResult {
	return definition.probe(ctx, config, secrets)
}

func newServiceFixture(t *testing.T, definition Definition, timeout time.Duration) (*Service, *persist.Store, *persist.SecretStore) {
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
	registry := NewRegistry()
	if err := registry.Register(definition); err != nil {
		t.Fatal(err)
	}
	return NewService(registry, store, secrets, ServiceOptions{ProbeTimeout: timeout}), store, secrets
}

func testServiceDefinition(probe func(context.Context, Config, Secrets) ProbeResult) serviceDefinition {
	definition := validDefinition("plex", "Plex 媒体")
	definition.metadata.DiscoveryHints = []string{"plex"}
	definition.fields = append(definition.fields, Field{Key: "token", Kind: FieldSecret, Label: "访问令牌", Help: "填写 Plex 生成的只读令牌。", Required: true})
	return serviceDefinition{contractDefinition: definition, probe: probe}
}

func successfulProbe(_ context.Context, _ Config, _ Secrets) ProbeResult {
	return ProbeResult{Stage: ProbeStageFeature, OK: true, Message: "连接成功"}
}

func TestServiceCRUDViewsAreRedactedAndSecretsAreCollected(t *testing.T) {
	service, store, secrets := newServiceFixture(t, testServiceDefinition(successfulProbe), time.Second)
	created, probe, err := service.Create(context.Background(), Candidate{Type: "plex", Config: Config{"url": "http://plex.local"}, Secrets: Secrets{"token": []byte("top-secret")}})
	if err != nil || !probe.OK {
		t.Fatalf("Create() = %#v, %#v, %v", created, probe, err)
	}
	if created.ID == "" || !created.Enabled || !created.Secrets["token"] {
		t.Fatalf("created = %#v", created)
	}
	if _, leaked := created.Config["token"]; leaked {
		t.Fatal("secret leaked into config")
	}
	if got := service.Instances(); len(got) != 1 || got[0].ID != created.ID {
		t.Fatalf("Instances() = %#v", got)
	}

	snapshot := store.Snapshot()
	ref := snapshot.Integrations[0].SecretRefs["token"]
	value, err := secrets.Read(ref)
	if err != nil || string(value) != "top-secret" {
		t.Fatalf("stored secret = %q, %v", value, err)
	}

	if _, _, err := service.Update(context.Background(), created.ID, Candidate{Type: "plex", Config: Config{"url": "http://plex-new.local"}, Secrets: Secrets{"token": []byte(MaskedSecret)}}); err != nil {
		t.Fatal(err)
	}
	updated := store.Snapshot().Integrations[0]
	if updated.SecretRefs["token"] != ref {
		t.Fatal("masked secret should retain its revision")
	}
	if _, _, err := service.Update(context.Background(), created.ID, Candidate{Type: "plex", Config: Config{"url": "http://plex-new.local"}, Secrets: Secrets{"token": []byte("rotated-secret")}}); err != nil {
		t.Fatal(err)
	}
	rotatedRef := store.Snapshot().Integrations[0].SecretRefs["token"]
	if rotatedRef == ref {
		t.Fatal("secret rotation retained the old revision")
	}
	if value, err := secrets.Read(rotatedRef); err != nil || string(value) != "rotated-secret" {
		t.Fatalf("rotated secret = %q, %v", value, err)
	}
	if _, err := secrets.Read(ref); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old revision was not collected: %v", err)
	}
	ref = rotatedRef

	if err := service.Disable(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if service.Instances()[0].Enabled {
		t.Fatal("Disable did not persist")
	}
	if err := service.Enable(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if !service.Instances()[0].Enabled {
		t.Fatal("Enable did not persist")
	}
	if err := store.Update(func(state *persist.State) error {
		state.Widgets = []persist.Widget{{ID: "plex-1", DefinitionID: "plex", IntegrationID: created.ID, Enabled: true}, {ID: "cpu-1", DefinitionID: "cpu", Enabled: true, Order: 1}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Remove(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if len(service.Instances()) != 0 {
		t.Fatal("Remove did not persist")
	}
	remaining := store.Snapshot().Widgets
	if len(remaining) != 1 || remaining[0].ID != "cpu-1" || remaining[0].Order != 0 {
		t.Fatalf("removed source widgets survived: %#v", remaining)
	}
	if _, err := secrets.Read(ref); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed secret is still readable: %v", err)
	}
}

func TestServiceRejectsUnknownFieldsUnsafeURLsAndDuplicateTypes(t *testing.T) {
	service, _, _ := newServiceFixture(t, testServiceDefinition(successfulProbe), time.Second)
	for name, candidate := range map[string]Candidate{
		"unknown":        {Type: "plex", Config: Config{"url": "http://plex.local", "other": true}, Secrets: Secrets{"token": []byte("secret")}},
		"credentials":    {Type: "plex", Config: Config{"url": "http://admin:secret@plex.local"}, Secrets: Secrets{"token": []byte("secret")}},
		"missing secret": {Type: "plex", Config: Config{"url": "http://plex.local"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := service.Create(context.Background(), candidate); err == nil {
				t.Fatal("invalid candidate unexpectedly succeeded")
			}
		})
	}
	if _, _, err := service.Create(context.Background(), Candidate{Type: "plex", Config: Config{"url": "http://plex.local"}, Secrets: Secrets{"token": []byte("secret")}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Create(context.Background(), Candidate{Type: "plex", Config: Config{"url": "http://other.local"}, Secrets: Secrets{"token": []byte("other")}}); !errors.Is(err, ErrAlreadyConfigured) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestServiceFailedRotationPreservesOldStateAndSecret(t *testing.T) {
	service, store, secrets := newServiceFixture(t, testServiceDefinition(func(_ context.Context, _ Config, candidate Secrets) ProbeResult {
		if string(candidate["token"]) == "bad-secret" {
			return ProbeResult{Stage: ProbeStageAuthentication, Message: "身份验证失败"}
		}
		return successfulProbe(context.Background(), nil, nil)
	}), time.Second)
	created, _, err := service.Create(context.Background(), Candidate{Type: "plex", Config: Config{"url": "http://plex.local"}, Secrets: Secrets{"token": []byte("good-secret")}})
	if err != nil {
		t.Fatal(err)
	}
	notifications := 0
	service.onChange = func(context.Context, []persist.Integration, []persist.Integration) error { notifications++; return nil }
	before := store.Snapshot().Integrations[0]
	_, result, err := service.Update(context.Background(), created.ID, Candidate{Type: "plex", Config: Config{"url": "http://changed.local"}, Secrets: Secrets{"token": []byte("bad-secret")}})
	if !errors.Is(err, ErrProbeFailed) || result.Stage != ProbeStageAuthentication {
		t.Fatalf("Update = %#v, %v", result, err)
	}
	if notifications != 0 {
		t.Fatalf("failed rotation notified runtime %d times", notifications)
	}
	after := store.Snapshot().Integrations[0]
	if after.Config["url"] != before.Config["url"] || after.SecretRefs["token"] != before.SecretRefs["token"] {
		t.Fatalf("failed update changed state: %#v", after)
	}
	value, err := secrets.Read(before.SecretRefs["token"])
	if err != nil || string(value) != "good-secret" {
		t.Fatalf("old secret = %q, %v", value, err)
	}
}

func TestServiceProbeTimeoutAndCandidateTestingDoNotPersist(t *testing.T) {
	service, store, _ := newServiceFixture(t, testServiceDefinition(func(ctx context.Context, _ Config, _ Secrets) ProbeResult {
		<-ctx.Done()
		return ProbeResult{Stage: ProbeStageTCP, Message: "连接超时"}
	}), 20*time.Millisecond)
	result, err := service.TestCandidate(context.Background(), Candidate{Type: "plex", Config: Config{"url": "http://plex.local"}, Secrets: Secrets{"token": []byte("secret")}})
	if !errors.Is(err, ErrProbeFailed) || result.Stage != ProbeStageTCP {
		t.Fatalf("TestCandidate = %#v, %v", result, err)
	}
	if len(store.Snapshot().Integrations) != 0 {
		t.Fatal("candidate test persisted state")
	}
}

func TestServiceCatalogMarksOnlyMatchingDiscoveryHints(t *testing.T) {
	service, _, _ := newServiceFixture(t, testServiceDefinition(successfulProbe), time.Second)
	catalog := service.Catalog(Discovery{Apps: []DiscoveredApp{{ID: "ix-plex", Name: "Plex"}}})
	if len(catalog) != 1 || !catalog[0].Detected || catalog[0].Configured {
		t.Fatalf("catalog = %#v", catalog)
	}
}

func TestServiceCreatePersistsIntegrationDefaultsAtomically(t *testing.T) {
	service, store, _ := newServiceFixture(t, testServiceDefinition(successfulProbe), time.Second)
	service.initialize = func(state *persist.State, instance persist.Integration) error {
		state.Widgets = append(state.Widgets, persist.Widget{
			ID: "plex-1", DefinitionID: "plex", IntegrationID: instance.ID, Enabled: true, Order: len(state.Widgets),
		})
		return nil
	}
	created, _, err := service.Create(context.Background(), Candidate{Type: "plex", Config: Config{"url": "http://plex.local"}, Secrets: Secrets{"token": []byte("secret")}})
	if err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	if len(state.Integrations) != 1 || len(state.Widgets) != 1 || state.Widgets[0].IntegrationID != created.ID {
		t.Fatalf("state = %#v", state)
	}
}
