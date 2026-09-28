package plex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
)

func TestProbeClassifiesAuthenticationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	client, err := New(config.PlexConfig{URL: server.URL, Token: "secret"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if result := client.Probe(context.Background()); result.OK || result.Stage != integration.ProbeStageAuthentication {
		t.Fatalf("Probe = %#v", result)
	}
}

func TestCurrentReadsPlayingAndPausedSessionsWithHeaderToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/status/sessions" {
			http.NotFound(w, request)
			return
		}
		if request.Header.Get("X-Plex-Token") != "plex-secret" || request.URL.Query().Get("X-Plex-Token") != "" {
			t.Fatalf("token header/query = %q / %q", request.Header.Get("X-Plex-Token"), request.URL.RawQuery)
		}
		if request.Header.Get("Accept") != "application/json" {
			t.Fatalf("Accept = %q", request.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte(`{"MediaContainer":{"Metadata":[
            {"type":"episode","title":"第一集","grandparentTitle":"剧集","Player":{"title":"客厅电视","state":"paused"},"User":{"title":"Alice"}},
            {"type":"movie","title":"电影","Player":{"product":"Plex Web","state":"playing"},"User":{"title":"Bob"}}
        ]}}`))
	}))
	defer server.Close()

	client, err := New(config.PlexConfig{URL: server.URL, Token: "plex-secret"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if len(got.Sessions) != 2 || got.Sessions[0].Title != "剧集 · 第一集" || got.Sessions[0].Device != "客厅电视" || got.Sessions[0].User != "Alice" || !got.Sessions[0].Paused || got.Sessions[1].Title != "电影" || got.Sessions[1].Device != "Plex Web" {
		t.Fatalf("media status = %#v", got)
	}
}

func TestCurrentReturnsNoSessionsWhenPlexIsIdle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":0}}`))
	}))
	defer server.Close()
	client, err := New(config.PlexConfig{URL: server.URL, Token: "plex-secret"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Current(context.Background())
	if err != nil || len(got.Sessions) != 0 {
		t.Fatalf("idle status/error = %#v / %v", got, err)
	}
}

func TestCurrentDoesNotForwardTokenAcrossRedirect(t *testing.T) {
	reachedSink := false
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		reachedSink = true
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":0}}`))
	}))
	defer sink.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, sink.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client, err := New(config.PlexConfig{URL: source.URL, Token: "must-not-leak"}, source.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Current(context.Background()); err == nil {
		t.Fatal("Current() error = nil")
	}
	if reachedSink {
		t.Fatal("Plex token request followed a redirect")
	}
}
