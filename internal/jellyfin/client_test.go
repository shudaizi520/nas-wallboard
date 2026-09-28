package jellyfin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
)

func TestProbeClassifiesAuthenticationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	client, err := New(config.JellyfinConfig{URL: server.URL, Token: "secret"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if result := client.Probe(context.Background()); result.OK || result.Stage != integration.ProbeStageAuthentication {
		t.Fatalf("Probe = %#v", result)
	}
}

func TestCurrentReadsOnlyNowPlayingSessionsWithHeaderToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/Sessions" {
			http.NotFound(w, request)
			return
		}
		if request.Header.Get("X-Emby-Token") != "jellyfin-secret" || request.URL.Query().Get("api_key") != "" {
			t.Fatalf("token header/query = %q / %q", request.Header.Get("X-Emby-Token"), request.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[
            {"UserName":"Alice","DeviceName":"卧室电视","NowPlayingItem":{"Name":"第二集","SeriesName":"剧集"},"PlayState":{"IsPaused":true}},
            {"UserName":"Bob","DeviceName":"Phone"}
        ]`))
	}))
	defer server.Close()

	client, err := New(config.JellyfinConfig{URL: server.URL, Token: "jellyfin-secret"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].Title != "剧集 · 第二集" || got.Sessions[0].Device != "卧室电视" || got.Sessions[0].User != "Alice" || !got.Sessions[0].Paused {
		t.Fatalf("media status = %#v", got)
	}
}

func TestCurrentDoesNotForwardTokenAcrossRedirect(t *testing.T) {
	reachedSink := false
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		reachedSink = true
		_, _ = w.Write([]byte(`[]`))
	}))
	defer sink.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, sink.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client, err := New(config.JellyfinConfig{URL: source.URL, Token: "must-not-leak"}, source.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Current(context.Background()); err == nil {
		t.Fatal("Current() error = nil")
	}
	if reachedSink {
		t.Fatal("Jellyfin token request followed a redirect")
	}
}
