package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/auth"
	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/dashboard"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/persist"
	"example.com/nas-wallboard/internal/truenas"
	"example.com/nas-wallboard/internal/widget"
)

type protectedFixture struct {
	handler http.Handler
	state   *persist.Store
	secrets *persist.SecretStore
	auth    *auth.Manager
	builder *dashboard.Builder
	root    string
}

func newProtectedFixture(t *testing.T, configured bool, probe SetupProbeFunc) protectedFixture {
	return newProtectedFixtureWithRuntime(t, configured, probe, nil)
}

func newProtectedFixtureWithRuntime(t *testing.T, configured bool, probe SetupProbeFunc, runtime IntegrationRuntime) protectedFixture {
	return newProtectedFixtureWithIntegrations(t, configured, probe, runtime, nil)
}

func newProtectedFixtureWithIntegrations(t *testing.T, configured bool, probe SetupProbeFunc, runtime IntegrationRuntime, build func(*persist.Store, *persist.SecretStore) *integration.Service) protectedFixture {
	t.Helper()
	root := t.TempDir()
	publicState, err := persist.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := persist.OpenSecrets(root)
	if err != nil {
		t.Fatal(err)
	}
	authManager, err := auth.Open(filepath.Join(root, "auth.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		if err := authManager.SetInitialPassword("correct horse battery staple"); err != nil {
			t.Fatal(err)
		}
		if err := publicState.Update(func(value *persist.State) error {
			value.SetupComplete = true
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	base := config.DashboardConfig{Width: 460, Metrics: []config.MetricConfig{{Type: config.MetricTypeCPU}}}
	builder, err := dashboard.New(base)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := dashboard.NewSettingsManager("", base, dashboard.Availability{Weather: true}, builder)
	if err != nil {
		t.Fatal(err)
	}
	if probe == nil {
		probe = func(_ context.Context, _ config.TrueNASConfig, _ string) (truenas.SetupProbeResult, error) {
			return successfulSetupProbe(), nil
		}
	}
	var integrationService *integration.Service
	if build != nil {
		integrationService = build(publicState, secrets)
	}
	widgetRegistry, err := widget.BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	widgetService := widget.NewService(widgetRegistry, publicState)
	return protectedFixture{
		handler: New(Dependencies{
			RuntimeStore: apiTestStore(), Assets: apiTestAssets(), Version: "test", Builder: builder,
			DashboardManager: manager, State: publicState, Secrets: secrets, Auth: authManager,
			Limiter: auth.NewLimiter(), DataRoot: root, Clock: time.Now, SetupProbe: probe, IntegrationRuntime: runtime, Integrations: integrationService,
			Widgets: widgetService,
		}),
		state: publicState, secrets: secrets, auth: authManager, builder: builder, root: root,
	}
}

func TestManagementRedirectsAndPublicDashboardPolicy(t *testing.T) {
	unconfigured := newProtectedFixture(t, false, nil)
	response := protectedRequest(t, unconfigured.handler, http.MethodGet, "http://nas.local/manage", nil, nil)
	if response.Code != http.StatusTemporaryRedirect || response.Header().Get("Location") != "/setup" {
		t.Fatalf("unconfigured manage = %d %#v", response.Code, response.Header())
	}
	if got := protectedRequest(t, unconfigured.handler, http.MethodGet, "http://nas.local/api/dashboard", nil, nil).Code; got != http.StatusOK {
		t.Fatalf("public dashboard = %d", got)
	}

	configured := newProtectedFixture(t, true, nil)
	response = protectedRequest(t, configured.handler, http.MethodGet, "http://nas.local/manage", nil, nil)
	if response.Code != http.StatusTemporaryRedirect || response.Header().Get("Location") != "/login" {
		t.Fatalf("configured manage = %d %#v", response.Code, response.Header())
	}
	api := protectedRequest(t, configured.handler, http.MethodGet, "http://nas.local/api/manage/dashboard", nil, map[string]string{"X-Wallboard-Manage": "1"})
	if api.Code != http.StatusUnauthorized || strings.Contains(strings.ToLower(api.Body.String()), "html") {
		t.Fatalf("unauthorized API = %d %q", api.Code, api.Body.String())
	}
}

func TestLoginSessionCSRFAndLogout(t *testing.T) {
	fixture := newProtectedFixture(t, true, nil)
	cookie, csrf := loginForTest(t, fixture.handler)

	session := protectedRequest(t, fixture.handler, http.MethodGet, "http://nas.local/api/auth/session", nil, nil, cookie)
	if session.Code != http.StatusOK || !strings.Contains(session.Body.String(), csrf) {
		t.Fatalf("session response = %d %q", session.Code, session.Body.String())
	}
	body := bytes.NewBufferString(`{"width":500,"metrics":["cpu"],"activities":[]}`)
	update := protectedRequest(t, fixture.handler, http.MethodPut, "http://nas.local/api/manage/dashboard", body, map[string]string{
		"Content-Type": "application/json", "Origin": "http://nas.local", "X-CSRF-Token": csrf,
	}, cookie)
	if update.Code != http.StatusOK {
		t.Fatalf("authenticated update = %d %q", update.Code, update.Body.String())
	}

	logout := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/auth/logout", nil, map[string]string{
		"Origin": "http://nas.local", "X-CSRF-Token": csrf,
	}, cookie)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout = %d %q", logout.Code, logout.Body.String())
	}
	if got := protectedRequest(t, fixture.handler, http.MethodGet, "http://nas.local/api/auth/session", nil, nil, cookie).Code; got != http.StatusUnauthorized {
		t.Fatalf("logged-out session = %d", got)
	}
}

func TestCSRFOriginHostAndLegacyHeaderDoNotBypassPolicy(t *testing.T) {
	fixture := newProtectedFixture(t, true, nil)
	cookie, csrf := loginForTest(t, fixture.handler)
	tests := []struct {
		name    string
		url     string
		headers map[string]string
		cookie  *http.Cookie
		want    int
	}{
		{"missing origin", "http://nas.local/api/manage/dashboard", map[string]string{"X-CSRF-Token": csrf}, cookie, http.StatusForbidden},
		{"wrong origin", "http://nas.local/api/manage/dashboard", map[string]string{"Origin": "http://evil.local", "X-CSRF-Token": csrf}, cookie, http.StatusForbidden},
		{"wrong csrf", "http://nas.local/api/manage/dashboard", map[string]string{"Origin": "http://nas.local", "X-CSRF-Token": "wrong"}, cookie, http.StatusForbidden},
		{"unsafe host", "http://public.example/api/manage/dashboard", map[string]string{"Origin": "http://public.example", "X-CSRF-Token": csrf}, cookie, http.StatusBadRequest},
		{"legacy header", "http://nas.local/api/manage/dashboard", map[string]string{"Origin": "http://nas.local", "X-Wallboard-Manage": "1"}, nil, http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := protectedRequest(t, fixture.handler, http.MethodPut, test.url, bytes.NewBufferString(`{"width":460}`), test.headers, test.cookie)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestLoginRateLimitAndCookieSecurity(t *testing.T) {
	fixture := newProtectedFixture(t, true, nil)
	for attempt := 1; attempt <= 6; attempt++ {
		response := protectedRequest(t, fixture.handler, http.MethodPost, "https://nas.local/api/auth/login", bytes.NewBufferString(`{"password":"bad password value"}`), map[string]string{
			"Content-Type": "application/json", "Origin": "https://nas.local",
		})
		want := http.StatusUnauthorized
		if attempt == 6 {
			want = http.StatusTooManyRequests
			if response.Header().Get("Retry-After") == "" {
				t.Fatal("rate limit omitted Retry-After")
			}
		}
		if response.Code != want {
			t.Fatalf("attempt %d = %d %q", attempt, response.Code, response.Body.String())
		}
	}

	otherIP := httptest.NewRequest(http.MethodPost, "https://nas.local/api/auth/login", bytes.NewBufferString(`{"password":"correct horse battery staple"}`))
	otherIP.RemoteAddr = "10.0.0.99:1234"
	otherIP.Header.Set("Content-Type", "application/json")
	otherIP.Header.Set("Origin", "https://nas.local")
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, otherIP)
	if recorder.Code != http.StatusOK {
		t.Fatalf("other IP login = %d %q", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" {
		t.Fatalf("cookie = %#v", cookies)
	}
}

func loginForTest(t *testing.T, handler http.Handler) (*http.Cookie, string) {
	t.Helper()
	response := protectedRequest(t, handler, http.MethodPost, "http://nas.local/api/auth/login", bytes.NewBufferString(`{"password":"correct horse battery staple"}`), map[string]string{
		"Content-Type": "application/json", "Origin": "http://nas.local",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("login = %d %q", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookies = %#v", cookies)
	}
	var result struct {
		CSRF string `json:"csrf"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || len(result.CSRF) != 64 {
		t.Fatalf("login response = %q / %v", response.Body.String(), err)
	}
	return cookies[0], result.CSRF
}

func protectedRequest(t *testing.T, handler http.Handler, method, target string, body io.Reader, headers map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, body)
	request.RemoteAddr = "192.168.50.10:1234"
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	for _, cookie := range cookies {
		if cookie != nil {
			request.AddCookie(cookie)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
