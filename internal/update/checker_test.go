package update

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckerCachesETagAndNeverChecksMoreOftenThanDaily(t *testing.T) {
	now := time.Unix(100, 0)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("ETag", `"release-one"`)
			_, _ = fmt.Fprint(w, `{"tag_name":"v1.2.0","html_url":"https://github.com/example/nas-wallboard/releases/tag/v1.2.0"}`)
			return
		}
		if request.Header.Get("If-None-Match") != `"release-one"` {
			t.Fatalf("If-None-Match = %q", request.Header.Get("If-None-Match"))
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	checker, err := New("example/nas-wallboard", "v1.1.0", server.Client(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	checker.endpoint = server.URL
	first := checker.Check(t.Context())
	if !first.Available || first.Latest != "v1.2.0" || calls != 1 {
		t.Fatalf("first = %#v, calls=%d", first, calls)
	}
	if second := checker.Check(t.Context()); second.Latest != first.Latest || calls != 1 {
		t.Fatalf("cached = %#v, calls=%d", second, calls)
	}
	now = now.Add(24*time.Hour + time.Second)
	third := checker.Check(t.Context())
	if third.Latest != "v1.2.0" || calls != 2 {
		t.Fatalf("conditional = %#v, calls=%d", third, calls)
	}
}

func TestCheckerHandlesOfflineRateLimitAndMalformedResponses(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"rate-limit": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
		"malformed":  func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"tag_name":`)) },
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			checker, _ := New("example/nas-wallboard", "v1.0.0", server.Client(), time.Now)
			checker.endpoint = server.URL
			result := checker.Check(t.Context())
			if result.Error == "" {
				t.Fatalf("failure hidden: %#v", result)
			}
		})
	}
	if _, err := New("https://evil.invalid/release", "v1.0.0", http.DefaultClient, time.Now); err == nil {
		t.Fatal("arbitrary update URL accepted")
	}
}
