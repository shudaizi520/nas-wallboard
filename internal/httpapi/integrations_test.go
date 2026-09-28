package httpapi

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/integrations"
	"example.com/nas-wallboard/internal/persist"
)

func integrationFixture(t *testing.T) protectedFixture {
	t.Helper()
	return newProtectedFixtureWithIntegrations(t, true, nil, nil, func(state *persist.Store, secrets *persist.SecretStore) *integration.Service {
		registry, err := integrations.BuiltInRegistry()
		if err != nil {
			t.Fatal(err)
		}
		return integration.NewService(registry, state, secrets, integration.ServiceOptions{ProbeTimeout: time.Second})
	})
}

func TestIntegrationAPIProbeCRUDRedactionAndAuthentication(t *testing.T) {
	plex := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Plex-Token") != "plex-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"MediaContainer":{"Metadata":[]}}`))
	}))
	defer plex.Close()
	fixture := integrationFixture(t)
	unauthorized := protectedRequest(t, fixture.handler, http.MethodGet, "http://nas.local/api/manage/integrations", nil, nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized = %d", unauthorized.Code)
	}
	cookie, csrf := loginForTest(t, fixture.handler)
	headers := map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local", "X-CSRF-Token": csrf}

	catalog := protectedRequest(t, fixture.handler, http.MethodGet, "http://nas.local/api/manage/integrations", nil, nil, cookie)
	if catalog.Code != http.StatusOK || !strings.Contains(catalog.Body.String(), `"id":"plex"`) {
		t.Fatalf("catalog = %d %q", catalog.Code, catalog.Body.String())
	}
	payload := fmt.Sprintf(`{"type":"plex","config":{"url":%q},"secrets":{"token":"plex-secret"}}`, plex.URL)
	probe := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/integrations/probe", bytes.NewBufferString(payload), headers, cookie)
	if probe.Code != http.StatusOK || len(fixture.state.Snapshot().Integrations) != 0 {
		t.Fatalf("probe = %d %q", probe.Code, probe.Body.String())
	}
	create := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/integrations", bytes.NewBufferString(payload), headers, cookie)
	if create.Code != http.StatusCreated || strings.Contains(create.Body.String(), "plex-secret") {
		t.Fatalf("create = %d %q", create.Code, create.Body.String())
	}
	instance := fixture.state.Snapshot().Integrations[0]
	updatePayload := fmt.Sprintf(`{"type":"plex","config":{"url":%q},"secrets":{"token":"%s"}}`, plex.URL, integration.MaskedSecret)
	update := protectedRequest(t, fixture.handler, http.MethodPut, "http://nas.local/api/manage/integrations/"+instance.ID, bytes.NewBufferString(updatePayload), headers, cookie)
	if update.Code != http.StatusOK || strings.Contains(update.Body.String(), "plex-secret") {
		t.Fatalf("update = %d %q", update.Code, update.Body.String())
	}
	for _, action := range []string{"disable", "enable"} {
		response := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/integrations/"+instance.ID+"/"+action, bytes.NewBufferString(`{}`), headers, cookie)
		if response.Code != http.StatusOK {
			t.Fatalf("%s = %d %q", action, response.Code, response.Body.String())
		}
	}
	remove := protectedRequest(t, fixture.handler, http.MethodDelete, "http://nas.local/api/manage/integrations/"+instance.ID, bytes.NewBufferString(fmt.Sprintf(`{"confirm":%q}`, instance.ID)), headers, cookie)
	if remove.Code != http.StatusNoContent || len(fixture.state.Snapshot().Integrations) != 0 {
		t.Fatalf("remove = %d %q", remove.Code, remove.Body.String())
	}
}

func TestIntegrationAPIRejectsCSRFUnknownFieldsLargeBodiesAndRateLimits(t *testing.T) {
	fixture := integrationFixture(t)
	cookie, csrf := loginForTest(t, fixture.handler)
	withoutCSRF := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/integrations/probe", bytes.NewBufferString(`{}`), map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local"}, cookie)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("CSRF = %d", withoutCSRF.Code)
	}
	headers := map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local", "X-CSRF-Token": csrf}
	unknown := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/integrations/probe", bytes.NewBufferString(`{"type":"plex","config":{"url":"http://plex.local","unknown":true},"secrets":{"token":"x"}}`), headers, cookie)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown = %d %q", unknown.Code, unknown.Body.String())
	}
	large := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/integrations/probe", bytes.NewBufferString(`{"type":"`+strings.Repeat("x", 70*1024)+`"}`), headers, cookie)
	if large.Code != http.StatusBadRequest {
		t.Fatalf("large = %d", large.Code)
	}
	for attempt := 0; attempt < 29; attempt++ {
		_ = protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/integrations/probe", bytes.NewBufferString(`{"type":"missing","config":{},"secrets":{}}`), headers, cookie)
	}
	limited := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/integrations/probe", bytes.NewBufferString(`{"type":"missing","config":{},"secrets":{}}`), headers, cookie)
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
		t.Fatalf("limited = %d %#v", limited.Code, limited.Header())
	}
}
