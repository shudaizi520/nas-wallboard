package integrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/model"
	"example.com/nas-wallboard/internal/state"
)

func TestAdaptiveDownloadsPollsOncePerMinuteWhenIdleAndEveryTickWhenActive(t *testing.T) {
	statuses := []model.DownloadStatus{{}, {ActiveCount: 1}, {ActiveCount: 1}, {}}
	calls := 0
	current := func(context.Context) (model.DownloadStatus, error) {
		status := statuses[calls]
		calls++
		return status, nil
	}
	published := model.DownloadStatus{}
	run := adaptiveDownloads(current, func(value model.DownloadStatus, _ error) { published = value }, 15*time.Second)

	if err := run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("idle requests after 45 seconds = %d, want 1", calls)
	}
	if err := run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || published.ActiveCount != 1 {
		t.Fatalf("one-minute check did not discover downloads: calls=%d published=%#v", calls, published)
	}
	if err := run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("active requests after one tick = %d, want 3", calls)
	}
	if err := run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 4 || published.ActiveCount != 0 {
		t.Fatalf("completed downloads were not cleared: calls=%d published=%#v", calls, published)
	}
}

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

func TestHomeAssistantCollectorPublishesFanAndNASPower(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/states/fan.office":
			_, _ = w.Write([]byte(`{"state":"on","last_changed":"2026-09-29T15:00:00Z","attributes":{"percentage":66,"preset_mode":"直吹风","oscillating":false}}`))
		case "/api/states/sensor.nas_power":
			_, _ = w.Write([]byte(`{"state":"38","attributes":{"unit_of_measurement":"W"}}`))
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	runtimeStore := state.New("test", state.StaleAfter{}, time.Now)
	registry, err := BuiltInRegistry(RuntimeOptions{Store: runtimeStore})
	if err != nil {
		t.Fatal(err)
	}
	definition, _ := registry.Definition("home_assistant")
	collector, err := definition.Collector(integration.Config{
		"url": server.URL, "entity_id": "fan.office", "power_entity_id": "sensor.nas_power",
		"name": "风扇", "remind_after": "2h", "call_timeout": "1s",
	}, integration.Secrets{"token": []byte("secret")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot := runtimeStore.Snapshot()
		if snapshot.Home.Data.State == "on" && snapshot.HomePower.Data.Watts == 38 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	snapshot := runtimeStore.Snapshot()
	if snapshot.Home.Data.State != "on" || snapshot.Home.Data.Percentage == nil || *snapshot.Home.Data.Percentage != 66 {
		t.Fatalf("fan = %#v", snapshot.Home)
	}
	if !snapshot.HomePower.Data.Available || snapshot.HomePower.Data.Watts != 38 {
		t.Fatalf("power = %#v", snapshot.HomePower)
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
