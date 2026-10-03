package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"testing/fstest"
)

func TestOverviewReportsActualBundledClientVersionSeparateFromServer(t *testing.T) {
	fixture := newProtectedFixture(t, true, nil)
	handler := New(Dependencies{RuntimeStore: apiTestStore(), State: fixture.state, Auth: fixture.auth, Builder: fixture.builder, Version: "v9.9.9", Assets: fstest.MapFS{
		"downloads/desktop-version.txt": &fstest.MapFile{Data: []byte("v1.0.10\n")},
	}})
	cookie, _ := loginForTest(t, handler)
	response := protectedRequest(t, handler, http.MethodGet, "http://nas.local/api/manage/overview", nil, nil, cookie)
	var result map[string]any
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatalf("overview status = %d", response.Code)
	}
	if result["version"] != "v9.9.9" || result["desktop_client_version"] != "v1.0.10" {
		t.Fatalf("version identities = %#v", result)
	}
}

func TestClientVersionSurvivesAuthenticationRedirectOnlyAsValidatedDisplayMetadata(t *testing.T) {
	fixture := newProtectedFixture(t, true, nil)
	for _, tt := range []struct{ query, want string }{
		{"?client_version=v1.0.10", "/login?client_version=v1.0.10"},
		{"?client_version=unknown", "/login"},
		{"?client_version=%3Cscript%3E&next=https://evil.example", "/login"},
	} {
		response := protectedRequest(t, fixture.handler, http.MethodGet, "http://nas.local/manage"+tt.query, nil, nil)
		if response.Code != http.StatusTemporaryRedirect || response.Header().Get("Location") != tt.want {
			t.Fatalf("redirect %q = %d %q", tt.query, response.Code, response.Header().Get("Location"))
		}
	}
	cookie, _ := loginForTest(t, fixture.handler)
	response := protectedRequest(t, fixture.handler, http.MethodGet, "http://nas.local/login?client_version=v1.0.10", nil, nil, cookie)
	if response.Header().Get("Location") != "/manage?client_version=v1.0.10" {
		t.Fatalf("logged-in redirect = %q", response.Header().Get("Location"))
	}
	response = protectedRequest(t, fixture.handler, http.MethodGet, "http://nas.local/setup?client_version=v1.0.10", nil, nil, cookie)
	if response.Header().Get("Location") != "/manage?client_version=v1.0.10" {
		t.Fatalf("configured setup redirect = %q", response.Header().Get("Location"))
	}
	unconfigured := newProtectedFixture(t, false, nil)
	response = protectedRequest(t, unconfigured.handler, http.MethodGet, "http://nas.local/manage?client_version=v1.0.10", nil, nil)
	if response.Header().Get("Location") != "/setup?client_version=v1.0.10" {
		t.Fatalf("unconfigured redirect = %q", response.Header().Get("Location"))
	}
}
