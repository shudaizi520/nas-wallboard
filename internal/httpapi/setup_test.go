package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/auth"
	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/persist"
	"example.com/nas-wallboard/internal/truenas"
)

type setupRuntime struct{ calls atomic.Int32 }

func (runtime *setupRuntime) Apply(_ context.Context, before, after []persist.Integration) error {
	if len(before) != 0 || len(after) != 1 || after[0].Type != "truenas" {
		return errors.New("unexpected setup runtime diff")
	}
	runtime.calls.Add(1)
	return nil
}

func TestSetupStatusProbeAndCompletion(t *testing.T) {
	var probes atomic.Int32
	probe := func(_ context.Context, cfg config.TrueNASConfig, apiKey string) (truenas.SetupProbeResult, error) {
		probes.Add(1)
		if cfg.URL != "wss://nas.local/api/current" || cfg.Username != "wallboard" || apiKey != "api-key-secret" {
			t.Fatalf("probe input = %#v / %q", cfg, apiKey)
		}
		return successfulSetupProbe(), nil
	}
	runtime := &setupRuntime{}
	fixture := newProtectedFixtureWithRuntime(t, false, probe, runtime)
	status := protectedRequest(t, fixture.handler, http.MethodGet, "http://nas.local/api/setup/status", nil, nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"setup_required":true`) {
		t.Fatalf("setup status = %d %q", status.Code, status.Body.String())
	}

	payload := `{"url":"wss://nas.local/api/current","username":"wallboard","api_key":"api-key-secret","insecure_skip_verify":true}`
	probeResponse := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/setup/probe", bytes.NewBufferString(payload), map[string]string{
		"Content-Type": "application/json", "Origin": "http://nas.local",
	})
	if probeResponse.Code != http.StatusOK || !strings.Contains(probeResponse.Body.String(), `"version":"25.10.0"`) {
		t.Fatalf("probe = %d %q", probeResponse.Code, probeResponse.Body.String())
	}
	if fixture.state.Snapshot().SetupComplete || len(fixture.state.Snapshot().Integrations) != 0 {
		t.Fatal("probe persisted candidate")
	}

	complete := `{"url":"wss://nas.local/api/current","username":"wallboard","api_key":"api-key-secret","insecure_skip_verify":true,"administrator_username":"Owner","password":"correct horse battery staple","password_confirmation":"correct horse battery staple"}`
	completeResponse := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/setup/complete", bytes.NewBufferString(complete), map[string]string{
		"Content-Type": "application/json", "Origin": "http://nas.local",
	})
	if completeResponse.Code != http.StatusCreated || strings.Contains(completeResponse.Body.String(), "api-key-secret") || strings.Contains(completeResponse.Body.String(), "correct horse") {
		t.Fatalf("complete = %d %q", completeResponse.Code, completeResponse.Body.String())
	}
	if probes.Load() != 2 {
		t.Fatalf("probe calls = %d, final save did not re-probe", probes.Load())
	}
	if runtime.calls.Load() != 1 || !strings.Contains(completeResponse.Body.String(), `"runtime_started":true`) {
		t.Fatalf("runtime activation = %d %q", runtime.calls.Load(), completeResponse.Body.String())
	}
	state := fixture.state.Snapshot()
	if !state.SetupComplete || len(state.Integrations) != 1 || state.Integrations[0].Type != "truenas" || len(state.Widgets) != 4 {
		t.Fatalf("state = %#v", state)
	}
	secret, err := fixture.secrets.Read(state.Integrations[0].SecretRefs["api_key"])
	if err != nil || string(secret) != "api-key-secret" {
		t.Fatalf("stored secret = %q / %v", secret, err)
	}
	if !fixture.auth.Configured() || fixture.auth.Username() != "Owner" {
		t.Fatal("administrator password not configured")
	}
	if view := fixture.builder.Build(apiTestStore().Snapshot(), time.Now()); view.Width != config.DashboardDefaultWidth || len(view.Metrics) != 3 {
		t.Fatalf("default widget layout was not applied without restart: %#v", view)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, "setup.pending.json")); !os.IsNotExist(err) {
		t.Fatalf("setup journal survived: %v", err)
	}
}

func TestSetupRejectsMismatchBadProbeAndNeverLeaksSecrets(t *testing.T) {
	secret := "must-never-escape-api-key"
	fixture := newProtectedFixture(t, false, func(_ context.Context, _ config.TrueNASConfig, _ string) (truenas.SetupProbeResult, error) {
		return truenas.SetupProbeResult{}, errors.New("raw upstream rejected " + secret + " from /run/secrets/private")
	})
	mismatch := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/setup/complete", bytes.NewBufferString(`{
  "url":"wss://nas.local/api/current","username":"wallboard","api_key":"`+secret+`",
  "password":"correct horse battery staple","password_confirmation":"different password value"
}`), map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local"})
	if mismatch.Code != http.StatusBadRequest || strings.Contains(mismatch.Body.String(), secret) {
		t.Fatalf("mismatch = %d %q", mismatch.Code, mismatch.Body.String())
	}

	probe := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/setup/probe", bytes.NewBufferString(`{
  "url":"wss://nas.local/api/current","username":"wallboard","api_key":"`+secret+`"
}`), map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local"})
	for _, forbidden := range []string{secret, "/run/secrets", "raw upstream"} {
		if strings.Contains(probe.Body.String(), forbidden) {
			t.Fatalf("probe leaked %q: %q", forbidden, probe.Body.String())
		}
	}
	if probe.Code != http.StatusBadGateway {
		t.Fatalf("probe status = %d %q", probe.Code, probe.Body.String())
	}
}

func TestSetupCompletesImportedInstallationWithoutReenteringAPIKey(t *testing.T) {
	fixture := newProtectedFixture(t, false, func(_ context.Context, cfg config.TrueNASConfig, apiKey string) (truenas.SetupProbeResult, error) {
		if cfg.URL != "wss://imported-nas.local/api/current" || cfg.Username != "imported-user" || apiKey != "imported-secret" {
			t.Fatalf("imported probe = %#v / %q", cfg, apiKey)
		}
		return successfulSetupProbe(), nil
	})
	ref, _, err := fixture.secrets.Stage("truenas-main", "api_key", []byte("imported-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.state.Update(func(value *persist.State) error {
		value.Legacy = &persist.LegacyMetadata{SourceFingerprint: "fixture"}
		value.Integrations = []persist.Integration{{
			ID: "truenas-main", Type: "truenas", Enabled: true,
			Config:     map[string]any{"url": "wss://imported-nas.local/api/current", "username": "imported-user", "insecure_skip_verify": true, "realtime_refresh": "15s"},
			SecretRefs: map[string]persist.SecretRef{"api_key": ref},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	probeResponse := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/setup/probe", bytes.NewBufferString(`{"use_imported":true}`), map[string]string{
		"Content-Type": "application/json", "Origin": "http://nas.local",
	})
	if probeResponse.Code != http.StatusOK {
		t.Fatalf("probe imported = %d %q", probeResponse.Code, probeResponse.Body.String())
	}
	response := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/setup/complete", bytes.NewBufferString(`{
  "use_imported":true,
	"administrator_username":"admin",
  "password":"correct horse battery staple",
  "password_confirmation":"correct horse battery staple"
}`), map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local"})
	if response.Code != http.StatusCreated {
		t.Fatalf("complete imported = %d %q", response.Code, response.Body.String())
	}
	got := fixture.state.Snapshot()
	if !got.SetupComplete || got.Integrations[0].SecretRefs["api_key"] != ref || got.Integrations[0].Config["realtime_refresh"] != "15s" {
		t.Fatalf("imported state = %#v", got)
	}
}

func TestSetupAndProbeRateLimitsAreIndependent(t *testing.T) {
	fixture := newProtectedFixture(t, false, nil)
	headers := map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local"}
	for attempt := 1; attempt <= 11; attempt++ {
		response := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/setup/probe", bytes.NewBufferString(`{"url":"wss://nas.local/api/current","username":"wallboard","api_key":"key"}`), headers)
		if attempt == 11 && (response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "") {
			t.Fatalf("probe rate limit = %d %#v", response.Code, response.Header())
		}
	}
	for attempt := 1; attempt <= 11; attempt++ {
		response := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/setup/complete", bytes.NewBufferString(`{}`), headers)
		if attempt == 11 && response.Code != http.StatusTooManyRequests {
			t.Fatalf("setup rate limit = %d", response.Code)
		}
	}
}

func TestRecoverPendingSetupRollsBackIncompleteTransaction(t *testing.T) {
	root := t.TempDir()
	state, err := persist.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := persist.OpenSecrets(root)
	if err != nil {
		t.Fatal(err)
	}
	ref, _, err := secrets.Stage("truenas-main", "api_key", []byte("orphan-secret"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := auth.Open(filepath.Join(root, "auth.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetInitialCredentials("admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	journal, _ := json.Marshal(setupJournal{Version: 1, SecretRef: ref, CreatedAt: time.Now()})
	if err := persist.WriteAtomic(filepath.Join(root, setupJournalName), journal, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecoverPendingSetup(root, state, secrets); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"auth.json", setupJournalName} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("%s survived recovery: %v", name, err)
		}
	}
	if _, err := secrets.Read(ref); err == nil {
		t.Fatal("orphan staged secret survived recovery")
	}
}

func TestRecoverPendingSetupKeepsCommittedAuthenticationAndSecret(t *testing.T) {
	root := t.TempDir()
	state, err := persist.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := persist.OpenSecrets(root)
	if err != nil {
		t.Fatal(err)
	}
	ref, _, err := secrets.Stage("truenas-main", "api_key", []byte("committed-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Update(func(value *persist.State) error {
		value.SetupComplete = true
		value.Integrations = []persist.Integration{{ID: "truenas-main", Type: "truenas", SecretRefs: map[string]persist.SecretRef{"api_key": ref}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	manager, err := auth.Open(filepath.Join(root, "auth.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetInitialCredentials("admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	journal, _ := json.Marshal(setupJournal{Version: 1, SecretRef: ref, CreatedAt: time.Now()})
	if err := persist.WriteAtomic(filepath.Join(root, setupJournalName), journal, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecoverPendingSetup(root, state, secrets); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "auth.json")); err != nil {
		t.Fatalf("committed auth removed: %v", err)
	}
	if secret, err := secrets.Read(ref); err != nil || string(secret) != "committed-secret" {
		t.Fatalf("committed secret = %q / %v", secret, err)
	}
	if _, err := os.Stat(filepath.Join(root, setupJournalName)); !os.IsNotExist(err) {
		t.Fatalf("journal survived recovery: %v", err)
	}
}

func successfulSetupProbe() truenas.SetupProbeResult {
	return truenas.SetupProbeResult{
		OK: true, Version: "25.10.0",
		Permissions: []truenas.SetupPermission{{Feature: "system", OK: true}},
		Pools:       []truenas.SetupResource{{ID: "1", Name: "tank"}},
		Disks:       []truenas.SetupResource{{ID: "sda", Name: "sda"}},
		Interfaces:  []truenas.SetupResource{{ID: "eno1", Name: "eno1"}},
		Apps:        []truenas.SetupResource{{ID: "plex", Name: "Plex"}},
	}
}
