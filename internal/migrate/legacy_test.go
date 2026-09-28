package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"example.com/nas-wallboard/internal/persist"
)

func TestImportLegacyImportsCompleteConfigurationAndLayout(t *testing.T) {
	fixture := completeLegacyFixture(t)
	state, secrets := newStores(t, fixture.dataRoot)
	originals := snapshotFiles(t, fixture.sourceFiles...)

	result, err := ImportLegacy(context.Background(), fixture.paths, state, secrets)
	if err != nil {
		t.Fatalf("ImportLegacy() error = %v", err)
	}
	if !result.Imported || !result.NeedsAdmin || len(result.SourceFingerprint) != 64 {
		t.Fatalf("result = %#v", result)
	}
	got := state.Snapshot()
	if got.SetupComplete {
		t.Fatal("legacy import must require administrator creation")
	}
	if got.Server.Listen != ":8080" || got.Server.ReadTimeout != "11s" || got.Server.WriteTimeout != "12s" || got.Server.Width != 520 || got.Server.Title != "家庭 NAS" || got.Server.Timezone != "Asia/Shanghai" || got.Server.Theme != "midnight" {
		t.Fatalf("server/dashboard settings = %#v", got.Server)
	}
	if got.Legacy == nil || got.Legacy.SourceFingerprint != result.SourceFingerprint {
		t.Fatalf("legacy metadata = %#v", got.Legacy)
	}

	wantTypes := []string{"truenas", "qweather", "plex", "jellyfin", "qbittorrent", "uptime_kuma", "home_assistant", "scrutiny"}
	if types := integrationTypes(got.Integrations); !reflect.DeepEqual(types, wantTypes) {
		t.Fatalf("integration types = %#v, want %#v", types, wantTypes)
	}
	trueNAS := integrationByType(t, got, "truenas")
	if trueNAS.ID != "truenas-main" || trueNAS.Config["url"] != "wss://nas.example.invalid/api/current" || trueNAS.Config["username"] != "nas_wallboard" || trueNAS.Config["realtime_refresh"] != "5s" || trueNAS.Config["disk_refresh"] != "1m3s" {
		t.Fatalf("TrueNAS import = %#v", trueNAS)
	}
	weather := integrationByType(t, got, "qweather")
	if weather.Config["name"] != "平湖" || weather.Config["refresh"] != "16m0s" || weather.Config["api_host"] != "abc123.qweatherapi.com" {
		t.Fatalf("weather import = %#v", weather)
	}
	if weather.Config["latitude"] != "22.69" || weather.Config["longitude"] != "114.13" {
		t.Fatalf("weather coordinates must use editable text values: %#v", weather.Config)
	}
	home := integrationByType(t, got, "home_assistant")
	if home.Config["fan_entity_id"] != "fan.living_room" || home.Config["remind_after"] != "1h30m0s" {
		t.Fatalf("home assistant import = %#v", home)
	}
	assertSecret(t, secrets, trueNAS, "api_key", "truenas-secret")
	if _, ok := weather.SecretRefs["api_host"]; ok {
		t.Fatal("public QWeather API host must not be stored as a secret")
	}
	assertSecret(t, secrets, weather, "api_key", "weather-secret")
	assertSecret(t, secrets, integrationByType(t, got, "plex"), "token", "plex-secret")
	assertSecret(t, secrets, integrationByType(t, got, "jellyfin"), "api_key", "jellyfin-secret")
	assertSecret(t, secrets, integrationByType(t, got, "qbittorrent"), "password", "qbit-secret")
	assertSecret(t, secrets, integrationByType(t, got, "uptime_kuma"), "api_key", "uptime-secret")
	assertSecret(t, secrets, home, "token", "home-secret")

	wantWidgets := []string{"network", "disk_temperature", "plex", "weather", "truenas_alerts"}
	if definitions := widgetDefinitions(got.Widgets); !reflect.DeepEqual(definitions, wantWidgets) {
		t.Fatalf("widget definitions = %#v, want %#v", definitions, wantWidgets)
	}
	if disk := got.Widgets[1].Config; disk["model"] != "ST14000NM001G" || disk["size_bytes"] != json.Number("14000519643136") {
		t.Fatalf("disk widget config = %#v", disk)
	}
	if alerts := got.Widgets[4].Config; alerts["limit"] != json.Number("3") {
		t.Fatalf("alert widget config = %#v", alerts)
	}
	assertFilesUnchanged(t, originals)
}

func TestImportLegacyAllowsMissingDisabledOptionalFiles(t *testing.T) {
	root := t.TempDir()
	configPath := writeFile(t, root, "config.yaml", `truenas:
  url: wss://nas.example.invalid/api/current
  username: nas_wallboard
plex:
  enabled: false
  secret_file: /missing/plex
`, 0o600)
	trueNASKey := writeFile(t, root, "truenas_api_key", "core-secret", 0o600)
	state, secrets := newStores(t, filepath.Join(root, "data"))

	result, err := ImportLegacy(context.Background(), LegacyPaths{
		Config: configPath, TrueNASSecret: trueNASKey,
		Dashboard: filepath.Join(root, "missing-dashboard.json"), HomeAssistantSecret: filepath.Join(root, "missing-home-token"),
	}, state, secrets)
	if err != nil {
		t.Fatalf("ImportLegacy() error = %v", err)
	}
	if !result.Imported || len(state.Snapshot().Integrations) != 1 || state.Snapshot().Integrations[0].Type != "truenas" {
		t.Fatalf("result/state = %#v / %#v", result, state.Snapshot())
	}
}

func TestImportLegacyIsIdempotent(t *testing.T) {
	fixture := completeLegacyFixture(t)
	state, secrets := newStores(t, fixture.dataRoot)
	first, err := ImportLegacy(context.Background(), fixture.paths, state, secrets)
	if err != nil {
		t.Fatal(err)
	}
	firstState := state.Snapshot()
	firstFiles := secretFiles(t, fixture.dataRoot)

	second, err := ImportLegacy(context.Background(), fixture.paths, state, secrets)
	if err != nil {
		t.Fatal(err)
	}
	if second.Imported || !second.NeedsAdmin || second.SourceFingerprint != first.SourceFingerprint {
		t.Fatalf("second result = %#v", second)
	}
	if !reflect.DeepEqual(state.Snapshot(), firstState) {
		t.Fatal("idempotent import changed state")
	}
	if got := secretFiles(t, fixture.dataRoot); !reflect.DeepEqual(got, firstFiles) {
		t.Fatalf("secret files changed: got %#v want %#v", got, firstFiles)
	}
}

func TestImportLegacyFingerprintChangesWithSource(t *testing.T) {
	fixture := completeLegacyFixture(t)
	state, secrets := newStores(t, fixture.dataRoot)
	first, err := ImportLegacy(context.Background(), fixture.paths, state, secrets)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fixture.paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("title: 家庭 NAS"), []byte("title: 新 NAS"), 1)
	if err := os.WriteFile(fixture.paths.Config, data, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := ImportLegacy(context.Background(), fixture.paths, state, secrets)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Imported || second.SourceFingerprint == first.SourceFingerprint || state.Snapshot().Server.Title != "新 NAS" {
		t.Fatalf("fingerprints/results = %#v / %#v", first, second)
	}
}

func TestImportLegacyFailureLeavesSourcesAndStateUnchanged(t *testing.T) {
	root := t.TempDir()
	configPath := writeFile(t, root, "config.yaml", `truenas:
  url: wss://nas.example.invalid/api/current
  username: nas_wallboard
unknown_field: rejected
`, 0o600)
	keyPath := writeFile(t, root, "truenas_api_key", "original-secret", 0o600)
	dashboardPath := writeFile(t, root, "dashboard.json", `{"width":460,"metrics":["cpu"],"activities":[]}`, 0o600)
	dataRoot := filepath.Join(root, "data")
	state, secrets := newStores(t, dataRoot)
	if err := state.Update(func(value *persist.State) error {
		value.Integrations = []persist.Integration{{ID: "existing", Type: "truenas", Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	beforeState := state.Snapshot()
	beforeSources := snapshotFiles(t, configPath, keyPath, dashboardPath)
	beforeSecrets := secretFiles(t, dataRoot)

	_, err := ImportLegacy(context.Background(), LegacyPaths{
		Config: configPath, TrueNASSecret: keyPath, Dashboard: dashboardPath,
	}, state, secrets)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("ImportLegacy() error = %v", err)
	}
	if !reflect.DeepEqual(state.Snapshot(), beforeState) {
		t.Fatalf("state changed: got %#v want %#v", state.Snapshot(), beforeState)
	}
	assertFilesUnchanged(t, beforeSources)
	if got := secretFiles(t, dataRoot); !reflect.DeepEqual(got, beforeSecrets) {
		t.Fatalf("secret store changed: got %#v want %#v", got, beforeSecrets)
	}
}

type legacyFixture struct {
	dataRoot    string
	paths       LegacyPaths
	sourceFiles []string
}

func completeLegacyFixture(t *testing.T) legacyFixture {
	t.Helper()
	root := t.TempDir()
	secretsDir := filepath.Join(root, "source-secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"truenas_api_key": "truenas-secret", "qweather_host": "abc123.qweatherapi.com", "qweather_key": "weather-secret",
		"plex_token": "plex-secret", "jellyfin_key": "jellyfin-secret", "qbit_password": "qbit-secret",
		"uptime_key": "uptime-secret", "home_token": "home-secret",
	}
	paths := map[string]string{}
	for name, value := range files {
		paths[name] = writeFile(t, secretsDir, name, value, 0o600)
	}
	template, err := os.ReadFile(filepath.Join("testdata", "legacy-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	replacer := strings.NewReplacer(
		"{{WEATHER_HOST}}", paths["qweather_host"], "{{WEATHER_KEY}}", paths["qweather_key"],
		"{{QBIT_PASSWORD}}", paths["qbit_password"], "{{PLEX_TOKEN}}", paths["plex_token"],
		"{{JELLYFIN_KEY}}", paths["jellyfin_key"], "{{UPTIME_KEY}}", paths["uptime_key"],
	)
	configPath := writeFile(t, root, "config.yaml", replacer.Replace(string(template)), 0o600)
	dashboardPath := writeFile(t, root, "dashboard.json", `{
  "width": 520,
  "metrics": ["network", "disk_temperature"],
  "activities": ["plex", "weather", "truenas_alerts"]
}`, 0o600)
	sourceFiles := []string{configPath, dashboardPath}
	for _, path := range paths {
		sourceFiles = append(sourceFiles, path)
	}
	return legacyFixture{
		dataRoot:    filepath.Join(root, "data"),
		paths:       LegacyPaths{Config: configPath, TrueNASSecret: paths["truenas_api_key"], Dashboard: dashboardPath, HomeAssistantSecret: paths["home_token"]},
		sourceFiles: sourceFiles,
	}
}

func newStores(t *testing.T, root string) (*persist.Store, *persist.SecretStore) {
	t.Helper()
	state, err := persist.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := persist.OpenSecrets(root)
	if err != nil {
		t.Fatal(err)
	}
	return state, secrets
}

func writeFile(t *testing.T, directory, name, value string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(value), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func integrationTypes(items []persist.Integration) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Type)
	}
	return result
}

func integrationByType(t *testing.T, state persist.State, kind string) persist.Integration {
	t.Helper()
	for _, item := range state.Integrations {
		if item.Type == kind {
			return item
		}
	}
	t.Fatalf("missing integration %q", kind)
	return persist.Integration{}
}

func assertSecret(t *testing.T, store *persist.SecretStore, integration persist.Integration, key, want string) {
	t.Helper()
	ref, ok := integration.SecretRefs[key]
	if !ok {
		t.Fatalf("%s missing secret %q", integration.Type, key)
	}
	got, err := store.Read(ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s/%s = %q, want %q", integration.Type, key, got, want)
	}
}

func widgetDefinitions(items []persist.Widget) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.DefinitionID)
	}
	return result
}

func snapshotFiles(t *testing.T, paths ...string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result[path] = data
	}
	return result
}

func assertFilesUnchanged(t *testing.T, originals map[string][]byte) {
	t.Helper()
	for path, want := range originals {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("source changed: %s", path)
		}
	}
}

func secretFiles(t *testing.T, dataRoot string) []string {
	t.Helper()
	root := filepath.Join(dataRoot, "secrets")
	result := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			result = append(result, relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(result)
	return result
}
