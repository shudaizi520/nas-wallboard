package integrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/state"
)

func TestBuiltInPlexCollectorPublishesSessionsAndStops(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"MediaContainer":{"Metadata":[{"title":"电影","Player":{"title":"客厅电视","state":"playing"}}]}}`))
	}))
	defer server.Close()
	runtimeStore := state.New("test", state.StaleAfter{}, time.Now)
	registry, err := BuiltInRegistry(RuntimeOptions{Store: runtimeStore})
	if err != nil {
		t.Fatal(err)
	}
	definition, _ := registry.Definition("plex")
	collector, err := definition.Collector(integration.Config{"url": server.URL, "call_timeout": "1s"}, integration.Secrets{"token": []byte("secret")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for len(runtimeStore.Snapshot().Plex.Data.Sessions) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sessions := runtimeStore.Snapshot().Plex.Data.Sessions; len(sessions) != 1 || sessions[0].Device != "客厅电视" {
		t.Fatalf("sessions = %#v", sessions)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("collector did not stop")
	}
}

func TestWeatherCoordinatesAcceptsNumericValuesFromEarlyLegacyImports(t *testing.T) {
	for _, settings := range []integration.Config{
		{"latitude": json.Number("22.69"), "longitude": json.Number("114.13")},
		{"latitude": 22.69, "longitude": 114.13},
	} {
		latitude, longitude, result := weatherCoordinates(settings)
		if !result.OK || latitude != 22.69 || longitude != 114.13 {
			t.Fatalf("weatherCoordinates(%#v) = %v, %v, %#v", settings, latitude, longitude, result)
		}
	}
}

func TestQWeatherCollectorAcceptsAPIHostFromEarlyLegacySecret(t *testing.T) {
	runtimeStore := state.New("test", state.StaleAfter{}, time.Now)
	registry, err := BuiltInRegistry(RuntimeOptions{Store: runtimeStore})
	if err != nil {
		t.Fatal(err)
	}
	definition, _ := registry.Definition("qweather")
	collector, err := definition.Collector(integration.Config{
		"latitude": json.Number("22.69"), "longitude": json.Number("114.13"),
	}, integration.Secrets{
		"api_host": []byte("abc123.qweatherapi.com"), "api_key": []byte("secret"),
	})
	if err != nil || collector == nil {
		t.Fatalf("Collector() = %#v, %v", collector, err)
	}
}
