package httpapi

import (
	"bytes"
	"context"
	"example.com/nas-wallboard/internal/persist"
	"example.com/nas-wallboard/internal/support"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEncryptedRestoreRoundTripAndWrongPasswordDoesNotChangeState(t *testing.T) {
	f := newProtectedFixture(t, true, nil)
	_ = f.state.Update(func(s *persist.State) error { s.Server.Title = "saved title"; return nil })
	var backup bytes.Buffer
	if err := support.WriteEncryptedBackup(&backup, f.root, "separate backup password"); err != nil {
		t.Fatal(err)
	}
	_ = f.state.Update(func(s *persist.State) error { s.Server.Title = "current title"; return nil })
	cookie, csrf := loginForTest(t, f.handler)
	// A complete uploaded body must not authorize mutation after cancellation.
	headers := map[string]string{"Origin": "http://nas.local", "X-CSRF-Token": csrf, "Content-Type": "application/json"}
	headers["X-Reauth-Token"] = reauthenticateForTest(t, f.handler, cookie, headers, "correct horse battery staple")
	var cancelledBody bytes.Buffer
	cancelledWriter := multipart.NewWriter(&cancelledBody)
	_ = cancelledWriter.WriteField("password", "separate backup password")
	_ = cancelledWriter.WriteField("confirmation", support.RestoreConfirmation)
	cancelledFile, _ := cancelledWriter.CreateFormFile("backup", "backup.age")
	_, _ = cancelledFile.Write(backup.Bytes())
	_ = cancelledWriter.Close()
	request := httptest.NewRequest(http.MethodPost, "http://nas.local/api/manage/restore", &cancelledBody)
	request.RemoteAddr = "192.168.50.10:1234"
	request.AddCookie(cookie)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	request.Header.Set("Content-Type", cancelledWriter.FormDataContentType())
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(ctx)
	result := httptest.NewRecorder()
	f.handler.ServeHTTP(result, request)
	if result.Code != http.StatusRequestTimeout || f.state.Snapshot().Server.Title != "current title" {
		t.Fatal("cancelled restore changed configuration")
	}
	for _, password := range []string{"wrong", "separate backup password"} {
		headers := map[string]string{"Origin": "http://nas.local", "X-CSRF-Token": csrf, "Content-Type": "application/json"}
		headers["X-Reauth-Token"] = reauthenticateForTest(t, f.handler, cookie, headers, "correct horse battery staple")
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		_ = writer.WriteField("password", password)
		_ = writer.WriteField("confirmation", support.RestoreConfirmation)
		file, _ := writer.CreateFormFile("backup", "backup.age")
		_, _ = file.Write(backup.Bytes())
		_ = writer.Close()
		headers["Content-Type"] = writer.FormDataContentType()
		result := protectedRequest(t, f.handler, http.MethodPost, "http://nas.local/api/manage/restore", &body, headers, cookie)
		if password == "wrong" {
			if result.Code != http.StatusBadRequest || f.state.Snapshot().Server.Title != "current title" {
				t.Fatal("invalid backup changed state")
			}
			continue
		}
		if result.Code != http.StatusNoContent || f.state.Snapshot().Server.Title != "saved title" {
			t.Fatalf("restore failed: %d %s", result.Code, result.Body.String())
		}
		if protectedRequest(t, f.handler, http.MethodGet, "http://nas.local/api/manage/overview", nil, nil, cookie).Code != http.StatusUnauthorized {
			t.Fatal("restore did not revoke session")
		}
		_, _ = loginForTest(t, f.handler)
	}
}
