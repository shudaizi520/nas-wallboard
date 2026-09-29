package integrations

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/integration"
)

func TestBuiltInCatalogContainsEverySupportedIntegration(t *testing.T) {
	registry, err := BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog := registry.Catalog()
	ids := make([]string, len(catalog))
	for index, definition := range catalog {
		ids[index] = definition.ID
	}
	want := []string{"home_assistant", "jellyfin", "plex", "qbittorrent", "qweather", "scrutiny", "truenas", "uptime_kuma"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("catalog = %#v, want %#v", ids, want)
	}

	for _, public := range catalog {
		definition, ok := registry.Definition(public.ID)
		if !ok {
			t.Fatalf("definition %q missing", public.ID)
		}
		if err := definition.Validate(integration.Config{}); err == nil {
			t.Errorf("%s accepts an empty configuration", public.ID)
		}
		if len(public.Fields) == 0 {
			t.Errorf("%s has no fields", public.ID)
		}
		if public.Metadata.MinimumRefresh <= 0 {
			t.Errorf("%s has no minimum refresh", public.ID)
		}
		if !public.Metadata.SingleInstance {
			t.Errorf("%s allows multiple instances", public.ID)
		}
	}
}

func TestBuiltInCatalogDeclaresExpectedSecretsAndCapabilities(t *testing.T) {
	registry, err := BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	wantSecret := map[string]string{
		"truenas": "api_key", "qweather": "api_key", "plex": "token", "jellyfin": "token",
		"qbittorrent": "password", "uptime_kuma": "api_key", "home_assistant": "token",
	}
	for id, key := range wantSecret {
		definition, _ := registry.Definition(id)
		found := false
		for _, field := range definition.Fields() {
			if field.Key == key && field.Kind == integration.FieldSecret {
				found = true
			}
		}
		if !found {
			t.Errorf("%s missing secret field %q", id, key)
		}
	}
	if fields := registry.Catalog()[5].Fields; len(fields) == 0 {
		t.Fatal("Scrutiny schema missing")
	}
	trueNAS, _ := registry.Definition("truenas")
	capabilityIDs := []string{}
	for _, capability := range trueNAS.Capabilities(nil) {
		capabilityIDs = append(capabilityIDs, capability.ID)
	}
	for _, required := range []string{"memory", "pool_capacity", "smart", "replication", "app_exceptions"} {
		if !containsString(capabilityIDs, required) {
			t.Errorf("TrueNAS missing capability %q: %#v", required, capabilityIDs)
		}
	}
}

func TestHomeAssistantCatalogOffersOptionalNASPowerSensorAtFifteenSeconds(t *testing.T) {
	registry, err := BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	definition, _ := registry.Definition("home_assistant")
	if definition.Metadata().MinimumRefresh != 15*time.Second {
		t.Fatalf("minimum refresh = %v", definition.Metadata().MinimumRefresh)
	}
	found := false
	for _, field := range definition.Fields() {
		if field.Key == "power_entity_id" {
			found = true
			if field.Required || field.Kind != integration.FieldEntityID {
				t.Fatalf("power field = %#v", field)
			}
		}
	}
	if !found {
		t.Fatal("power_entity_id field missing")
	}
	if err := definition.Validate(integration.Config{
		"url": "http://homeassistant.local:8123", "entity_id": "fan.office",
	}); err != nil {
		t.Fatalf("optional power field rejected: %v", err)
	}
}

func TestQWeatherCatalogPollsAtFiveMinuteAlertCadence(t *testing.T) {
	registry, err := BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	definition, _ := registry.Definition("qweather")
	if definition.Metadata().MinimumRefresh != 5*time.Minute {
		t.Fatalf("minimum refresh = %v, want 5m", definition.Metadata().MinimumRefresh)
	}
}

func TestCatalogKeepsAdvancedFieldsOutOfTheCommonSetupPath(t *testing.T) {
	registry, err := BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"truenas", "qweather", "plex", "jellyfin", "qbittorrent", "uptime_kuma", "home_assistant", "scrutiny"} {
		definition, _ := registry.Definition(id)
		foundTimeout := false
		for _, field := range definition.Fields() {
			if field.Key == "call_timeout" {
				foundTimeout = true
				if !field.Advanced {
					t.Errorf("%s call_timeout should be advanced", id)
				}
			}
		}
		if !foundTimeout {
			t.Errorf("%s has no call_timeout field", id)
		}
	}
	for _, public := range registry.Catalog() {
		if public.ID != "plex" {
			continue
		}
		for _, field := range public.Fields {
			if field.Key == "call_timeout" && !field.Advanced {
				t.Fatal("advanced flag was not exposed in the public catalog")
			}
		}
	}
}

func TestCatalogHelpExplainsWhereNewUsersFindCredentialsAndEntities(t *testing.T) {
	registry, err := BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string][]string{
		"truenas":        {"api_key": {"Add API Key", "My API Keys"}},
		"qweather":       {"api_key": {"控制台", "API Key"}},
		"plex":           {"token": {"X-Plex-Token"}},
		"jellyfin":       {"token": {"控制台", "API 密钥"}},
		"uptime_kuma":    {"api_key": {"设置", "API Key"}},
		"home_assistant": {"entity_id": {"设置", "实体"}, "token": {"个人资料", "长期访问令牌"}},
	}
	for id, fields := range want {
		definition, _ := registry.Definition(id)
		byKey := map[string]integration.Field{}
		for _, field := range definition.Fields() {
			byKey[field.Key] = field
		}
		for key, fragments := range fields {
			for _, fragment := range fragments {
				if !strings.Contains(byKey[key].Help, fragment) {
					t.Errorf("%s.%s help %q does not contain %q", id, key, byKey[key].Help, fragment)
				}
			}
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestQWeatherRejectsCredentialBearingHostAndInvalidCoordinates(t *testing.T) {
	registry, err := BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	definition, _ := registry.Definition("qweather")
	base := integration.Config{"api_host": "api.qweather.com", "latitude": "22.54", "longitude": "114.06"}
	if err := definition.Validate(base); err != nil {
		t.Fatal(err)
	}
	for _, settings := range []integration.Config{
		{"api_host": "user:pass@api.qweather.com", "latitude": "22.54", "longitude": "114.06"},
		{"api_host": "api.qweather.com", "latitude": "north", "longitude": "114.06"},
	} {
		if err := definition.Validate(settings); err == nil {
			t.Fatalf("Validate(%#v) unexpectedly succeeded", settings)
		}
	}
}
