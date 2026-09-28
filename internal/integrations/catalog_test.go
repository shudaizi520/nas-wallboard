package integrations

import (
	"reflect"
	"testing"

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
