package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/dashboard"
	"example.com/nas-wallboard/internal/model"
	"example.com/nas-wallboard/internal/state"
	wallboardweb "example.com/nas-wallboard/web"
)

func apiTestStore() *state.Store {
	intervals := state.StaleAfter{Realtime: time.Second, Apps: time.Second, Alerts: time.Second, System: time.Second, Pools: time.Second, Disks: time.Second, Weather: time.Second}
	return state.New("test-version", intervals, func() time.Time { return time.Unix(100, 0).UTC() })
}

func apiTestAssets() fs.FS {
	return fstest.MapFS{
		"index.html":                         {Data: []byte("<!doctype html><title>wallboard</title>")},
		"manage.html":                        {Data: []byte(`<!doctype html><title>manage</title><a href="/download/nas-wallboard-desktop.zip">安装桌面小组件</a>`)},
		"setup.html":                         {Data: []byte("<!doctype html><title>setup</title>")},
		"login.html":                         {Data: []byte("<!doctype html><title>login</title>")},
		"styles.css":                         {Data: []byte("body{color:white}")},
		"downloads/NASWallboard.Desktop.exe": {Data: []byte("MZvalid-windows-program")},
	}
}

func request(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func apiTestHandler(t *testing.T, store *state.Store, assets fs.FS, version string) http.Handler {
	t.Helper()
	builder, err := dashboard.New(config.DashboardConfig{Width: 560})
	if err != nil {
		t.Fatal(err)
	}
	return New(Dependencies{RuntimeStore: store, Assets: assets, Version: version, Builder: builder})
}

func TestStatusEndpointIsVersionedCompactAndNeverCached(t *testing.T) {
	store := apiTestStore()
	store.SetSystem(model.SystemStatus{Hostname: "atlas"}, nil)
	handler := apiTestHandler(t, store, apiTestAssets(), "handler-version")
	recorder := request(t, handler, http.MethodGet, "/api/status")

	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/json" || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status response = %d %#v", recorder.Code, recorder.Header())
	}
	if strings.Contains(recorder.Body.String(), "\n  ") {
		t.Fatalf("status JSON is not compact: %q", recorder.Body.String())
	}
	var snapshot model.Snapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if snapshot.SchemaVersion != model.SchemaVersion || snapshot.System.Data.Hostname != "atlas" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestStatusEndpointContainsNoSecretOrRawPayload(t *testing.T) {
	store := apiTestStore()
	store.SetSystem(model.SystemStatus{Hostname: "safe"}, nil)
	handler := apiTestHandler(t, store, apiTestAssets(), "version")
	body := request(t, handler, http.MethodGet, "/api/status").Body.String()
	for _, forbidden := range []string{"api_key", "home_assistant_token", "bearer ", "fan_entity_id", "raw_payload", "stack_trace", "must-never-escape"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("status contains %q: %s", forbidden, body)
		}
	}
}

func TestManagementPageAndAPIAreLANOnly(t *testing.T) {
	fixture := newProtectedFixture(t, true, nil)
	handler := fixture.handler

	public := httptest.NewRequest(http.MethodGet, "/manage", nil)
	public.RemoteAddr = "203.0.113.10:1234"
	publicRecorder := httptest.NewRecorder()
	handler.ServeHTTP(publicRecorder, public)
	if publicRecorder.Code != http.StatusForbidden {
		t.Fatalf("public management status = %d", publicRecorder.Code)
	}

	cookie, _ := loginForTest(t, handler)
	privateRecorder := protectedRequest(t, handler, http.MethodGet, "http://nas.local/manage", nil, nil, cookie)
	if privateRecorder.Code != http.StatusOK || !strings.Contains(privateRecorder.Body.String(), "manage") {
		t.Fatalf("private management response = %d %q", privateRecorder.Code, privateRecorder.Body.String())
	}

	apiRecorder := protectedRequest(t, handler, http.MethodGet, "http://nas.local/api/manage/dashboard", nil, nil, cookie)
	if apiRecorder.Code != http.StatusOK || !strings.Contains(apiRecorder.Body.String(), `"min_width":300`) {
		t.Fatalf("management API response = %d %q", apiRecorder.Code, apiRecorder.Body.String())
	}
}

func TestDesktopDownloadIsLANOnlyAndSupportsGetAndHead(t *testing.T) {
	handler := apiTestHandler(t, apiTestStore(), apiTestAssets(), "version")

	public := httptest.NewRequest(http.MethodGet, "/download/nas-wallboard-desktop.zip", nil)
	public.RemoteAddr = "203.0.113.10:1234"
	publicRecorder := httptest.NewRecorder()
	handler.ServeHTTP(publicRecorder, public)
	if publicRecorder.Code != http.StatusForbidden {
		t.Fatalf("public desktop download status = %d", publicRecorder.Code)
	}

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		request := httptest.NewRequest(method, "http://10.0.0.99:18082/download/nas-wallboard-desktop.zip", nil)
		request.RemoteAddr = "192.168.50.10:1234"
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/zip" || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s desktop download = %d %#v", method, recorder.Code, recorder.Header())
		}
		if recorder.Header().Get("Content-Disposition") != `attachment; filename="nas-wallboard-desktop.zip"` {
			t.Fatalf("content disposition = %q", recorder.Header().Get("Content-Disposition"))
		}
		if method == http.MethodGet && (!bytes.HasPrefix(recorder.Body.Bytes(), []byte("PK")) || !bytes.Contains(recorder.Body.Bytes(), []byte("10.0.0.99:18082"))) {
			t.Fatalf("desktop ZIP body is invalid: %d bytes", recorder.Body.Len())
		}
		if method == http.MethodHead && recorder.Body.Len() != 0 {
			t.Fatalf("HEAD body = %d bytes", recorder.Body.Len())
		}
	}
}

func TestDesktopDownloadRejectsUnsafeHostAndMissingExecutable(t *testing.T) {
	handler := apiTestHandler(t, apiTestStore(), apiTestAssets(), "version")
	unsafe := httptest.NewRequest(http.MethodGet, "/download/nas-wallboard-desktop.zip", nil)
	unsafe.RemoteAddr = "192.168.50.10:1234"
	unsafe.Host = "user:pass@evil.example"
	unsafeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unsafeRecorder, unsafe)
	if unsafeRecorder.Code != http.StatusBadRequest {
		t.Fatalf("unsafe host status = %d", unsafeRecorder.Code)
	}

	missing := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	missingHandler := apiTestHandler(t, apiTestStore(), missing, "version")
	missingRequest := httptest.NewRequest(http.MethodGet, "http://nas.local/download/nas-wallboard-desktop.zip", nil)
	missingRequest.RemoteAddr = "192.168.50.10:1234"
	missingRecorder := httptest.NewRecorder()
	missingHandler.ServeHTTP(missingRecorder, missingRequest)
	if missingRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing executable status = %d", missingRecorder.Code)
	}
}

func TestRawDesktopExecutableIsNeverServed(t *testing.T) {
	handler := apiTestHandler(t, apiTestStore(), apiTestAssets(), "version")
	request := httptest.NewRequest(http.MethodGet, "/downloads/NASWallboard.Desktop.exe", nil)
	request.RemoteAddr = "192.168.50.10:1234"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || bytes.Contains(recorder.Body.Bytes(), []byte("MZvalid")) {
		t.Fatalf("raw executable response = %d %q", recorder.Code, recorder.Body.Bytes())
	}
}

func TestManagementAPIUpdatesReadOnlyDashboard(t *testing.T) {
	fixture := newProtectedFixture(t, true, nil)
	handler := fixture.handler
	cookie, csrf := loginForTest(t, handler)
	body := bytes.NewBufferString(`{"width":500,"metrics":["network"],"activities":["weather"]}`)
	recorder := protectedRequest(t, handler, http.MethodPut, "http://nas.local/api/manage/dashboard", body, map[string]string{
		"Content-Type": "application/json", "Origin": "http://nas.local", "X-CSRF-Token": csrf,
	}, cookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("update response = %d %q", recorder.Code, recorder.Body.String())
	}

	display := protectedRequest(t, handler, http.MethodGet, "http://nas.local/api/dashboard", nil, nil)
	var view dashboard.View
	if err := json.Unmarshal(display.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Width != 500 || len(view.Metrics) != 1 || view.Metrics[0].ID != "network" {
		t.Fatalf("display view = %#v", view)
	}
}

func TestManagementWriteRequiresSameSiteHeader(t *testing.T) {
	fixture := newProtectedFixture(t, true, nil)
	handler := fixture.handler
	cookie, csrf := loginForTest(t, handler)
	update := httptest.NewRequest(http.MethodPut, "http://nas.local/api/manage/dashboard", bytes.NewBufferString(`{"width":460}`))
	update.RemoteAddr = "192.168.50.10:1234"
	update.AddCookie(cookie)
	update.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, update)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("write without management header = %d", recorder.Code)
	}
}

func TestScriptsAndStylesRevalidateWhileWallpaperStaysImmutable(t *testing.T) {
	handler := apiTestHandler(t, apiTestStore(), apiTestAssets(), "version")
	asset := request(t, handler, http.MethodGet, "/styles.css")
	if asset.Code != http.StatusOK || asset.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("asset response = %d %#v", asset.Code, asset.Header())
	}
	index := request(t, handler, http.MethodGet, "/")
	if index.Code != http.StatusOK || index.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(index.Body.String(), "wallboard") {
		t.Fatalf("index response = %d %#v %q", index.Code, index.Header(), index.Body.String())
	}
}

func TestEmbeddedWallpaperIsServedAsPNGAndImmutable(t *testing.T) {
	handler := apiTestHandler(t, apiTestStore(), wallboardweb.FS, "version")
	recorder := request(t, handler, http.MethodGet, "/wallpaper.png")

	if recorder.Code != http.StatusOK {
		t.Fatalf("wallpaper status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("wallpaper content type = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("wallpaper cache control = %q", got)
	}
	if !bytes.HasPrefix(recorder.Body.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("wallpaper does not have PNG signature")
	}
	if recorder.Body.Len() < 1_000_000 {
		t.Fatalf("wallpaper payload is unexpectedly small: %d bytes", recorder.Body.Len())
	}
}

func TestEmbeddedBrandIconIsServedAsSVGAndImmutable(t *testing.T) {
	handler := apiTestHandler(t, apiTestStore(), wallboardweb.FS, "version")
	recorder := request(t, handler, http.MethodGet, "/icon.svg")

	if recorder.Code != http.StatusOK {
		t.Fatalf("icon status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "image/svg+xml") {
		t.Fatalf("icon content type = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("icon cache control = %q", got)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("<svg")) || recorder.Body.Len() < 500 {
		t.Fatalf("icon payload is invalid: %d bytes", recorder.Body.Len())
	}
}

func TestEveryRouteHasSecurityHeaders(t *testing.T) {
	handler := apiTestHandler(t, apiTestStore(), apiTestAssets(), "version")
	for _, path := range []string{"/", "/styles.css", "/api/status", "/api/dashboard", "/healthz", "/readyz", "/missing"} {
		recorder := request(t, handler, http.MethodGet, path)
		if recorder.Header().Get("X-Content-Type-Options") != "nosniff" || recorder.Header().Get("Referrer-Policy") != "no-referrer" || recorder.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s missing security headers: %#v", path, recorder.Header())
		}
	}
}

func TestDesktopPreviewAllowsOnlySameOriginFraming(t *testing.T) {
	handler := apiTestHandler(t, apiTestStore(), apiTestAssets(), "version")

	preview := request(t, handler, http.MethodGet, "/?desktop=1&preview=1")
	if got := preview.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("preview X-Frame-Options = %q", got)
	}
	if got := preview.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'self'") {
		t.Fatalf("preview Content-Security-Policy = %q", got)
	}

	regular := request(t, handler, http.MethodGet, "/")
	if got := regular.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("regular X-Frame-Options = %q", got)
	}
	if got := regular.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") {
		t.Fatalf("regular Content-Security-Policy = %q", got)
	}
}

func TestHealthAndReadinessAreIndependent(t *testing.T) {
	store := apiTestStore()
	handler := apiTestHandler(t, store, apiTestAssets(), "version")
	if got := request(t, handler, http.MethodGet, "/healthz").Code; got != http.StatusOK {
		t.Fatalf("health status = %d", got)
	}
	if got := request(t, handler, http.MethodGet, "/readyz").Code; got != http.StatusServiceUnavailable {
		t.Fatalf("initial ready status = %d", got)
	}
	store.Connected(true)
	store.SetSystem(model.SystemStatus{}, nil)
	store.SetRealtime(model.RealtimeStatus{}, nil)
	store.SetPools(nil, nil)
	if got := request(t, handler, http.MethodGet, "/readyz").Code; got != http.StatusOK {
		t.Fatalf("ready status = %d", got)
	}
}

func TestDashboardEndpointIsOrderedAndContainsNoSourceSecrets(t *testing.T) {
	store := apiTestStore()
	store.Connected(true)
	store.SetRealtime(model.RealtimeStatus{CPUPercent: 18.4}, nil)
	store.SetDisks([]model.DiskStatus{
		{ID: "nvme0n1", Name: "nvme0n1", Model: "private-nvme-model", Serial: "private-nvme-serial", SizeBytes: 512110190592, Temperature: 48},
		{ID: "sda", Name: "sda", Model: "ST14000NM001G-2KJ103", Serial: "private-hdd-serial", SizeBytes: 14000519643136, Temperature: 40},
	}, nil)
	store.SetApps([]model.AppStatus{{ID: "qbittorrent", Name: "qBittorrent", State: "RUNNING", UpdateAvailable: true, Link: "http://private-app:8080"}}, nil)
	store.SetAlerts([]model.AlertStatus{{ID: "a1", Level: "warning", Title: "SMART", Message: "温度偏高"}}, nil)
	builder, err := dashboard.New(config.DashboardConfig{
		Width: 560,
		Metrics: []config.MetricConfig{
			{Type: config.MetricTypeDiskTemperature, Match: config.DiskMatchConfig{Model: "ST14000NM001G-2KJ103", SizeBytes: 14000519643136}},
			{Type: config.MetricTypeCPU},
		},
		Activities: []config.ActivityConfig{{Type: config.ActivityTypeAppUpdates}, {Type: config.ActivityTypeTrueNASAlerts, Limit: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Dependencies{RuntimeStore: store, Assets: apiTestAssets(), Version: "version", Builder: builder})
	recorder := request(t, handler, http.MethodGet, "/api/dashboard")
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/json" || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("dashboard response = %d %#v", recorder.Code, recorder.Header())
	}
	var view dashboard.View
	if err := json.Unmarshal(recorder.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if view.Width != 560 || len(view.Metrics) != 2 || view.Metrics[0].ID != "disk_temperature" || view.Metrics[1].ID != "cpu" || len(view.Activities) != 2 || view.Activities[0].ID != "updates" || view.Activities[1].ID != "alert:a1" {
		t.Fatalf("dashboard view = %#v", view)
	}
	body := strings.ToLower(recorder.Body.String())
	for _, forbidden := range []string{"private-", "st14000", "14000519643136", "http://", "username", "token", "cookie", "raw", "timeout"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("dashboard contains %q: %s", forbidden, body)
		}
	}
}

func TestExternalSourceFailuresDoNotLeakThroughEitherAPI(t *testing.T) {
	store := apiTestStore()
	secretError := errors.New("upstream https://private.example failed with token super-secret and cookie SID=secret")
	store.SetDownloads(model.DownloadStatus{}, secretError)
	store.SetPlex(model.MediaStatus{}, secretError)
	store.SetJellyfin(model.MediaStatus{}, secretError)
	store.SetMonitors(model.MonitorStatus{}, secretError)
	builder, err := dashboard.New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{
		{Type: config.ActivityTypeQBittorrent}, {Type: config.ActivityTypePlex}, {Type: config.ActivityTypeJellyfin}, {Type: config.ActivityTypeUptimeKuma},
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Dependencies{RuntimeStore: store, Assets: apiTestAssets(), Version: "version", Builder: builder})
	for _, endpoint := range []string{"/api/status", "/api/dashboard"} {
		body := strings.ToLower(request(t, handler, http.MethodGet, endpoint).Body.String())
		for _, forbidden := range []string{"private.example", "super-secret", "sid=secret", "https://"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s contains %q: %s", endpoint, forbidden, body)
			}
		}
	}
}
