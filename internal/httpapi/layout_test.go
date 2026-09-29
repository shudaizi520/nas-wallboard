package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/auth"
	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/dashboard"
	"example.com/nas-wallboard/internal/persist"
	"example.com/nas-wallboard/internal/widget"
)

func layoutFixture(t *testing.T) (http.Handler, *persist.Store, *dashboard.Builder) {
	t.Helper()
	root := t.TempDir()
	stateStore, err := persist.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := stateStore.Update(func(state *persist.State) error {
		state.SetupComplete = true
		state.Server.Width = 360
		state.Integrations = []persist.Integration{
			{ID: "truenas-main", Type: "truenas", Enabled: true},
			{ID: "plex-main", Type: "plex", Enabled: true},
		}
		state.Widgets = []persist.Widget{
			{ID: "cpu-1", DefinitionID: "cpu", IntegrationID: "truenas-main", Enabled: true, Order: 0},
			{ID: "plex-1", DefinitionID: "plex", IntegrationID: "plex-main", Enabled: true, Order: 1},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	authManager, err := auth.Open(filepath.Join(root, "auth.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := authManager.SetInitialCredentials("admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	base := config.DashboardConfig{Width: 360, Metrics: []config.MetricConfig{{Type: config.MetricTypeCPU}}, Activities: []config.ActivityConfig{{Type: config.ActivityTypePlex}}}
	builder, err := dashboard.New(base)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := dashboard.NewSettingsManager("", base, dashboard.Availability{Plex: true}, builder)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := widget.BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	widgets := widget.NewService(registry, stateStore)
	return New(Dependencies{
		RuntimeStore: apiTestStore(), Assets: apiTestAssets(), Version: "test", Builder: builder,
		DashboardManager: manager, State: stateStore, Auth: authManager, Limiter: auth.NewLimiter(),
		Clock: time.Now, DataRoot: root, Widgets: widgets,
	}), stateStore, builder
}

func TestLayoutAPIIsAuthenticatedAndPersistsStableInstances(t *testing.T) {
	handler, stateStore, builder := layoutFixture(t)
	if got := protectedRequest(t, handler, http.MethodGet, "http://nas.local/api/manage/layout", nil, nil).Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthorized = %d", got)
	}
	cookie, csrf := loginForTest(t, handler)
	get := protectedRequest(t, handler, http.MethodGet, "http://nas.local/api/manage/layout", nil, nil, cookie)
	if get.Code != http.StatusOK {
		t.Fatalf("GET = %d %q", get.Code, get.Body.String())
	}
	var view struct {
		Catalog []widget.Definition   `json:"catalog"`
		Sources []persist.Integration `json:"sources"`
		Layout  widget.Layout         `json:"layout"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Catalog) == 0 || len(view.Sources) != 2 || len(view.Layout.Widgets) != 2 {
		t.Fatalf("view = %#v", view)
	}
	next := widget.Layout{Width: 500, Widgets: []persist.Widget{
		{ID: "plex-1", DefinitionID: "plex", IntegrationID: "plex-main", Enabled: true, Order: 0, Config: map[string]any{"limit": 4}},
		{ID: "cpu-1", DefinitionID: "cpu", IntegrationID: "truenas-main", Enabled: true, Order: 1},
	}}
	payload, _ := json.Marshal(next)
	put := protectedRequest(t, handler, http.MethodPut, "http://nas.local/api/manage/layout", bytes.NewReader(payload), map[string]string{
		"Content-Type": "application/json", "Origin": "http://nas.local", "X-CSRF-Token": csrf,
	}, cookie)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT = %d %q", put.Code, put.Body.String())
	}
	stored := stateStore.Snapshot()
	if stored.Server.Width != 500 || stored.Widgets[0].ID != "plex-1" || stored.Widgets[1].ID != "cpu-1" {
		t.Fatalf("stored = %#v", stored)
	}
	viewAfter := builder.Build(apiTestStore().Snapshot(), time.Now())
	if viewAfter.Width != 500 || len(viewAfter.Metrics) != 1 || viewAfter.Metrics[0].ID != "cpu" {
		t.Fatalf("dashboard = %#v", viewAfter)
	}
}

func TestLayoutAPIRejectsMissingCSRFUnknownFieldsAndInvalidSource(t *testing.T) {
	handler, _, _ := layoutFixture(t)
	cookie, csrf := loginForTest(t, handler)
	validHeaders := map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local", "X-CSRF-Token": csrf}
	missingCSRF := protectedRequest(t, handler, http.MethodPut, "http://nas.local/api/manage/layout", bytes.NewBufferString(`{"width":360,"widgets":[]}`), map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local"}, cookie)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF = %d", missingCSRF.Code)
	}
	unknown := protectedRequest(t, handler, http.MethodPut, "http://nas.local/api/manage/layout", bytes.NewBufferString(`{"width":360,"widgets":[],"secret":"x"}`), validHeaders, cookie)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown = %d %q", unknown.Code, unknown.Body.String())
	}
	invalid := protectedRequest(t, handler, http.MethodPut, "http://nas.local/api/manage/layout", bytes.NewBufferString(`{"width":360,"widgets":[{"id":"plex-1","definition_id":"plex","integration_id":"missing","enabled":true,"order":0}]}`), validHeaders, cookie)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid source = %d %q", invalid.Code, invalid.Body.String())
	}
}
