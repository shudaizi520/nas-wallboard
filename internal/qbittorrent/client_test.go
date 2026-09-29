package qbittorrent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
)

func TestProbeClassifiesAuthenticationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v2/auth/login" {
			_, _ = w.Write([]byte("Fails."))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := New(config.QBittorrentConfig{URL: server.URL, Username: "user", Password: "bad"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if result := client.Probe(context.Background()); result.OK || result.Stage != integration.ProbeStageAuthentication {
		t.Fatalf("Probe = %#v", result)
	}
}

func TestCurrentLogsInAndRetriesExpiredSessionOnce(t *testing.T) {
	infoCalls := 0
	loginCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v2/torrents/info":
			infoCalls++
			cookie, err := request.Cookie("SID")
			if err != nil || cookie.Value != "fresh" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
                    {"name":"Ubuntu.iso","progress":0.684,"dlspeed":12500000,"state":"downloading"},
                    {"name":"Queued.iso","progress":0.2,"dlspeed":0,"state":"stalledDL"},
                    {"name":"Done.iso","progress":1,"dlspeed":0,"state":"uploading"}
                ]`))
		case "/api/v2/auth/login":
			loginCalls++
			if err := request.ParseForm(); err != nil || request.Form.Get("username") != "wallboard" || request.Form.Get("password") != "secret" {
				t.Fatalf("login form = %#v, error = %v", request.Form, err)
			}
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "fresh", Path: "/"})
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	client, err := New(config.QBittorrentConfig{URL: server.URL, Username: "wallboard", Password: "secret"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if loginCalls != 1 || infoCalls != 2 {
		t.Fatalf("calls = login %d, info %d", loginCalls, infoCalls)
	}
	if got.ActiveCount != 2 || got.DownloadBps != 12500000 || len(got.Items) != 2 || got.Items[0].Name != "Ubuntu.iso" || got.Items[0].ProgressPercent != 68.4 || got.Items[1].Name != "Queued.iso" || got.Items[1].ProgressPercent != 20 {
		t.Fatalf("download status = %#v", got)
	}
}

func TestCurrentDoesNotRetryTwiceAfterUnauthorizedLogin(t *testing.T) {
	infoCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v2/torrents/info" {
			infoCalls++
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client, err := New(config.QBittorrentConfig{URL: server.URL, Username: "wallboard", Password: "wrong"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Current(context.Background()); err == nil {
		t.Fatal("Current() error = nil")
	}
	if infoCalls != 1 {
		t.Fatalf("info calls = %d, want 1", infoCalls)
	}
}

func TestCurrentDoesNotFollowCredentialRedirect(t *testing.T) {
	reachedSink := false
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		reachedSink = true
		_, _ = w.Write([]byte("Ok."))
	}))
	defer sink.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v2/torrents/info" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		http.Redirect(w, request, sink.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client, err := New(config.QBittorrentConfig{URL: source.URL, Username: "wallboard", Password: "must-not-leak"}, source.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Current(context.Background()); err == nil {
		t.Fatal("Current() error = nil")
	}
	if reachedSink {
		t.Fatal("qBittorrent credential request followed a redirect")
	}
}
