package homeassistant

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
)

func TestProbeClassifiesAuthenticationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	if result := New(testConfig(server.URL), "secret", server.Client()).Probe(context.Background()); result.OK || result.Stage != integration.ProbeStageAuthentication {
		t.Fatalf("Probe = %#v", result)
	}
}

func testConfig(endpoint string) config.HomeAssistantConfig {
	return config.HomeAssistantConfig{
		Enabled: true, URL: endpoint, FanEntityID: "fan.living_room", FanName: "客厅风扇",
		RemindAfter: config.Duration{Duration: 2 * time.Hour}, CallTimeout: config.Duration{Duration: time.Second},
	}
}

func TestCurrentFanAuthenticatesAndMapsSupportedAttributes(t *testing.T) {
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestPath = request.URL.EscapedPath()
		if got := request.Header.Get("Authorization"); got != "Bearer ha-test-token" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entity_id":"fan.living_room","state":"on","last_changed":"2026-09-27T06:30:00Z","attributes":{"percentage":42,"preset_mode":"nature","oscillating":true,"friendly_name":"must not win"}}`))
	}))
	defer server.Close()

	client := New(testConfig(server.URL), "ha-test-token", server.Client())
	status, err := client.CurrentFan(context.Background())
	if err != nil {
		t.Fatalf("CurrentFan() error = %v", err)
	}
	if requestPath != "/api/states/fan.living_room" {
		t.Fatalf("request path = %q", requestPath)
	}
	if !status.Enabled || !status.Available || status.Name != "客厅风扇" || status.State != "on" || status.OnSince == nil || status.OnSince.Format(time.RFC3339) != "2026-09-27T06:30:00Z" {
		t.Fatalf("fan status = %#v", status)
	}
	if status.Percentage == nil || *status.Percentage != 42 || status.PresetMode != "nature" || status.Oscillating == nil || !*status.Oscillating || status.RemindAfterSeconds != 7200 {
		t.Fatalf("fan attributes = %#v", status)
	}
}

func TestCurrentPowerMapsWattsFromConfiguredSensor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.EscapedPath() != "/api/states/sensor.nas_power" {
			t.Errorf("escaped path = %q", request.URL.EscapedPath())
		}
		if got := request.Header.Get("Authorization"); got != "Bearer ha-test-token" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"state":"38","attributes":{"unit_of_measurement":"W","friendly_name":"NAS power"}}`))
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.PowerEntityID = "sensor.nas_power"
	status, err := New(cfg, "ha-test-token", server.Client()).CurrentPower(context.Background())
	if err != nil || !status.Available || status.Watts != 38 {
		t.Fatalf("CurrentPower() = %#v, %v", status, err)
	}
}

func TestCurrentPowerRejectsUnavailableAndNonPowerValues(t *testing.T) {
	for _, body := range []string{
		`{"state":"unavailable","attributes":{"unit_of_measurement":"W"}}`,
		`{"state":"not-a-number","attributes":{"unit_of_measurement":"W"}}`,
		`{"state":"38","attributes":{"unit_of_measurement":"kWh"}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		cfg := testConfig(server.URL)
		cfg.PowerEntityID = "sensor.nas_power"
		if _, err := New(cfg, "token", server.Client()).CurrentPower(context.Background()); err == nil {
			server.Close()
			t.Fatalf("CurrentPower() accepted %s", body)
		}
		server.Close()
	}
}

func TestCurrentFanEscapesEntityIDAsOnePathSegment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.EscapedPath() != "/api/states/fan.room%2Fguest" {
			t.Errorf("escaped path = %q", request.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`{"state":"off","last_changed":"2026-09-27T06:30:00Z","attributes":{}}`))
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.FanEntityID = "fan.room/guest"
	status, err := New(cfg, "token", server.Client()).CurrentFan(context.Background())
	if err != nil || status.State != "off" || status.OnSince != nil {
		t.Fatalf("status/error = %#v / %v", status, err)
	}
}

func TestCurrentFanDiscardsUnknownOrUnsafeAttributes(t *testing.T) {
	longMode := strings.Repeat("x", 200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = fmt.Fprintf(w, `{"state":"on","last_changed":"not-a-time","attributes":{"percentage":900,"preset_mode":%q,"oscillating":"yes","access_token":"secret"}}`, longMode)
	}))
	defer server.Close()

	status, err := New(testConfig(server.URL), "token", server.Client()).CurrentFan(context.Background())
	if err != nil {
		t.Fatalf("CurrentFan() error = %v", err)
	}
	if status.OnSince != nil || status.Percentage != nil || status.Oscillating != nil || len([]rune(status.PresetMode)) > 64 {
		t.Fatalf("unsafe attributes escaped = %#v", status)
	}
}

func TestCurrentFanMapsUnavailableAndHTTPFailures(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantState  string
		wantError  string
	}{
		{"entity-unavailable", http.StatusOK, `{"state":"unavailable","attributes":{}}`, "unavailable", ""},
		{"unauthorized", http.StatusUnauthorized, `{"message":"token ha-secret rejected"}`, "", "unauthorized"},
		{"forbidden", http.StatusForbidden, `{"message":"forbidden ha-secret"}`, "", "unauthorized"},
		{"not-found", http.StatusNotFound, `{"message":"entity missing ha-secret"}`, "", "not_found"},
		{"server-error", http.StatusInternalServerError, `{"message":"internal ha-secret"}`, "", "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			status, err := New(testConfig(server.URL), "token", server.Client()).CurrentFan(context.Background())
			if tt.wantError == "" {
				if err != nil || status.State != tt.wantState || status.Available {
					t.Fatalf("status/error = %#v / %v", status, err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantError || strings.Contains(err.Error(), "ha-secret") {
				t.Fatalf("error = %v, want public %q", err, tt.wantError)
			}
		})
	}
}

func TestCurrentFanHonorsContextAndRejectsOversizedResponses(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			<-request.Context().Done()
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		_, err := New(testConfig(server.URL), "token", server.Client()).CurrentFan(ctx)
		if err == nil || !strings.Contains(err.Error(), "timeout") {
			t.Fatalf("timeout error = %v", err)
		}
	})

	t.Run("oversized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			_, _ = w.Write([]byte(`{"state":"on","attributes":{"padding":"` + strings.Repeat("x", 70*1024) + `"}}`))
		}))
		defer server.Close()
		_, err := New(testConfig(server.URL), "token", server.Client()).CurrentFan(context.Background())
		if err == nil || err.Error() != "response_too_large" {
			t.Fatalf("oversized error = %v", err)
		}
	})
}
