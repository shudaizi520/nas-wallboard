package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverviewAndSupportBundleRequireAuthenticationAndStayRedacted(t *testing.T) {
	fixture := newProtectedFixture(t, true, nil)
	if got := protectedRequest(t, fixture.handler, http.MethodGet, "http://nas.local/api/manage/overview", nil, nil).Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthorized overview = %d", got)
	}
	cookie, _ := loginForTest(t, fixture.handler)
	overview := protectedRequest(t, fixture.handler, http.MethodGet, "http://nas.local/api/manage/overview", nil, nil, cookie)
	if overview.Code != http.StatusOK || !strings.Contains(overview.Body.String(), `"version":"test"`) || !strings.Contains(overview.Body.String(), `"migration"`) {
		t.Fatalf("overview = %d %q", overview.Code, overview.Body.String())
	}
	bundle := protectedRequest(t, fixture.handler, http.MethodGet, "http://nas.local/api/manage/support", nil, nil, cookie)
	if bundle.Code != http.StatusOK || bundle.Header().Get("Content-Disposition") == "" || bundle.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("bundle = %d %#v", bundle.Code, bundle.Header())
	}
	archive, err := zip.NewReader(bytes.NewReader(bundle.Body.Bytes()), int64(bundle.Body.Len()))
	if err != nil || len(archive.File) != 3 {
		t.Fatalf("support ZIP = %v / %v", archive, err)
	}
}

func TestReauthenticationBackupPasswordChangeAndFactoryReset(t *testing.T) {
	fixture := newProtectedFixture(t, true, nil)
	if err := os.MkdirAll(filepath.Join(fixture.root, "secrets", "extra"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.root, "secrets", "extra", "value"), []byte("private-backup-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginForTest(t, fixture.handler)
	headers := map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local", "X-CSRF-Token": csrf}

	bad := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/reauth", bytes.NewBufferString(`{"password":"wrong password value"}`), headers, cookie)
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad reauth = %d %q", bad.Code, bad.Body.String())
	}
	token := reauthenticateForTest(t, fixture.handler, cookie, headers, "correct horse battery staple")
	backupHeaders := cloneHeaders(headers)
	backupHeaders["X-Reauth-Token"] = token
	backup := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/backup", bytes.NewBufferString(`{"password":"a separate backup password"}`), backupHeaders, cookie)
	if backup.Code != http.StatusOK || bytes.Contains(backup.Body.Bytes(), []byte("private-backup-value")) {
		t.Fatalf("backup = %d %q", backup.Code, backup.Body.String())
	}
	reused := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/backup", bytes.NewBufferString(`{"password":"a separate backup password"}`), backupHeaders, cookie)
	if reused.Code != http.StatusForbidden {
		t.Fatalf("reused reauth = %d", reused.Code)
	}

	change := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/password", bytes.NewBufferString(`{"current":"correct horse battery staple","replacement":"a different secure password"}`), headers, cookie)
	if change.Code != http.StatusNoContent {
		t.Fatalf("password change = %d %q", change.Code, change.Body.String())
	}
	newLogin := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/auth/login", bytes.NewBufferString(`{"username":"admin","password":"a different secure password"}`), map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local"})
	if newLogin.Code != http.StatusOK {
		t.Fatalf("new password login = %d %q", newLogin.Code, newLogin.Body.String())
	}
	newCookie := newLogin.Result().Cookies()[0]
	var loginBody struct {
		CSRF string `json:"csrf"`
	}
	_ = json.Unmarshal(newLogin.Body.Bytes(), &loginBody)
	newHeaders := map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local", "X-CSRF-Token": loginBody.CSRF}
	token = reauthenticateForTest(t, fixture.handler, newCookie, newHeaders, "a different secure password")
	newHeaders["X-Reauth-Token"] = token
	reset := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/reset", bytes.NewBufferString(`{"confirmation":"删除 NAS WALLBOARD"}`), newHeaders, newCookie)
	if reset.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %q", reset.Code, reset.Body.String())
	}
	if fixture.state.Snapshot().SetupComplete || fixture.auth.Configured() {
		t.Fatal("configuration survived reset")
	}
}

func reauthenticateForTest(t *testing.T, handler http.Handler, cookie *http.Cookie, headers map[string]string, password string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"password": password})
	response := protectedRequest(t, handler, http.MethodPost, "http://nas.local/api/manage/reauth", bytes.NewReader(payload), headers, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("reauth = %d %q", response.Code, response.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body.Token) != 64 {
		t.Fatalf("reauth response = %q", response.Body.String())
	}
	return body.Token
}

func cloneHeaders(input map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range input {
		result[key] = value
	}
	return result
}
