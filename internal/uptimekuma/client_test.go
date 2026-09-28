package uptimekuma

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
)

func TestProbeClassifiesAuthenticationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	client, err := New(config.UptimeKumaConfig{URL: server.URL, APIKey: "secret"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if result := client.Probe(context.Background()); result.OK || result.Stage != integration.ProbeStageAuthentication {
		t.Fatalf("Probe = %#v", result)
	}
}

func TestCurrentUsesBasicAPIKeyAndKeepsOnlyDownMonitorNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/metrics" {
			http.NotFound(w, request)
			return
		}
		username, password, ok := request.BasicAuth()
		if !ok || username != "uptime-secret" || password != "" {
			t.Fatalf("basic auth = %q / %q / %v", username, password, ok)
		}
		_, _ = w.Write([]byte(`# HELP monitor_status Monitor Status
monitor_status{monitor_name="NAS",monitor_type="ping",monitor_url="10.0.0.99"} 1
monitor_status{monitor_name="API \"main\"",monitor_type="http",monitor_url="https://secret.example/api"} 0
monitor_response_time{monitor_name="API \"main\""} 12.3
`))
	}))
	defer server.Close()

	client, err := New(config.UptimeKumaConfig{URL: server.URL, APIKey: "uptime-secret"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if got.Total != 2 || len(got.DownNames) != 1 || got.DownNames[0] != `API "main"` {
		t.Fatalf("monitor status = %#v", got)
	}
}

func TestCurrentRejectsOversizedMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
	}))
	defer server.Close()
	client, err := New(config.UptimeKumaConfig{URL: server.URL, APIKey: "key"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Current(context.Background()); err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("Current() error = %v", err)
	}
}

func TestCurrentDoesNotForwardAPIKeyAcrossRedirect(t *testing.T) {
	reachedSink := false
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		reachedSink = true
		_, _ = w.Write([]byte("monitor_status{monitor_name=\"site\"} 1\n"))
	}))
	defer sink.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, sink.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client, err := New(config.UptimeKumaConfig{URL: source.URL, APIKey: "must-not-leak"}, source.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Current(context.Background()); err == nil {
		t.Fatal("Current() error = nil")
	}
	if reachedSink {
		t.Fatal("Uptime Kuma API key request followed a redirect")
	}
}
