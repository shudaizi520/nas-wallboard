package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestFactoryResetAllowsSetupWithoutRestart(t *testing.T) {
	f := newProtectedFixture(t, true, nil)
	cookie, csrf := loginForTest(t, f.handler)
	headers := map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local", "X-CSRF-Token": csrf}
	headers["X-Reauth-Token"] = reauthenticateForTest(t, f.handler, cookie, headers, "correct horse battery staple")
	reset := protectedRequest(t, f.handler, http.MethodPost, "http://nas.local/api/manage/reset", bytes.NewBufferString(`{"confirmation":"删除 NAS WALLBOARD"}`), headers, cookie)
	if reset.Code != http.StatusNoContent {
		t.Fatalf("reset failed: %d", reset.Code)
	}
	result := protectedRequest(t, f.handler, http.MethodGet, "http://nas.local/api/setup/status", nil, nil)
	var status struct {
		SetupRequired bool `json:"setup_required"`
		Configured    bool `json:"configured"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.SetupRequired || status.Configured || f.auth.Configured() {
		t.Fatal("reset leaves setup blocked")
	}
}
