package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/truenas"
)

func TestSetupRechecksStateAfterConcurrentProbe(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{}, 2)
	first, second := make(chan struct{}), make(chan struct{})
	fixture := newProtectedFixture(t, false, func(ctx context.Context, _ config.TrueNASConfig, _ string) (truenas.SetupProbeResult, error) {
		sequence := calls.Add(1)
		entered <- struct{}{}
		release := first
		if sequence == 2 {
			release = second
		}
		select {
		case <-release:
			return successfulSetupProbe(), nil
		case <-ctx.Done():
			return truenas.SetupProbeResult{}, ctx.Err()
		}
	})
	payload := `{"url":"wss://nas.local/api/current","username":"wallboard","api_key":"fixture-only","administrator_username":"admin","password":"correct horse battery staple","password_confirmation":"correct horse battery staple"}`
	headers := map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local"}
	results := make(chan int, 2)
	start := func() {
		go func() {
			result := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/setup/complete", bytes.NewBufferString(payload), headers)
			if result.Code == http.StatusConflict && !strings.Contains(result.Body.String(), "already_configured") {
				results <- -result.Code
				return
			}
			results <- result.Code
		}()
	}
	await := func() {
		t.Helper()
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("setup probe blocked before commit")
		}
	}
	start()
	await()
	start()
	await()
	close(first)
	select {
	case result := <-results:
		if result != http.StatusCreated {
			t.Fatalf("first setup = %d", result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("first setup deadlocked")
	}
	close(second)
	select {
	case result := <-results:
		if result != http.StatusConflict {
			t.Fatalf("second setup must recheck before owning journal: %d", result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("second setup deadlocked")
	}
	if _, err := os.Stat(filepath.Join(fixture.root, setupJournalName)); !os.IsNotExist(err) {
		t.Fatalf("journal remains after completed transaction: %v", err)
	}
	state := fixture.state.Snapshot()
	if !state.SetupComplete || !fixture.auth.Configured() || len(state.Integrations) != 1 {
		t.Fatal("concurrent setup damaged committed state")
	}
	if _, err := fixture.secrets.Read(state.Integrations[0].SecretRefs["api_key"]); err != nil {
		t.Fatalf("losing setup discarded committed secret: %v", err)
	}
}

func TestCredentialVerificationSharesReauthenticationBudget(t *testing.T) {
	fixture := newProtectedFixture(t, true, nil)
	cookie, csrf := loginForTest(t, fixture.handler)
	headers := map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local", "X-CSRF-Token": csrf}
	paths := []string{"password", "username", "reauth"}
	bodies := []string{`{"current":"wrong credential","replacement":"another valid password"}`, `{"current_password":"wrong credential","username":"Owner"}`, `{"password":"wrong credential"}`}
	for i := 0; i < 8; i++ {
		response := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/"+paths[i%3], bytes.NewBufferString(bodies[i%3]), headers, cookie)
		if i < 5 {
			if response.Code != http.StatusUnauthorized {
				t.Errorf("attempt %d: invalid credentials = %d", i+1, response.Code)
			}
		} else if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "600" {
			t.Errorf("attempt %d bypassed shared budget: %d", i+1, response.Code)
		}
	}
}

func TestSuccessfulCredentialChangeResetsSharedBudget(t *testing.T) {
	for _, kind := range []string{"password", "username"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newProtectedFixture(t, true, nil)
			cookie, csrf := loginForTest(t, fixture.handler)
			headers := map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local", "X-CSRF-Token": csrf}
			for i := 0; i < 4; i++ {
				protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/reauth", bytes.NewBufferString(`{"password":"wrong credential"}`), headers, cookie)
			}
			body := `{"current":"correct horse battery staple","replacement":"another valid password"}`
			if kind == "username" {
				body = `{"current_password":"correct horse battery staple","username":"Owner"}`
			}
			response := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/"+kind, bytes.NewBufferString(body), headers, cookie)
			if response.Code != http.StatusOK && response.Code != http.StatusNoContent {
				t.Fatalf("credential change = %d", response.Code)
			}
			// The real auth manager creates a new session because changing credentials revokes old sessions.
			username, password := "admin", "another valid password"
			if kind == "username" {
				username, password = "Owner", "correct horse battery staple"
			}
			session, err := fixture.auth.Login("192.168.50.20", username, password)
			if err != nil {
				t.Fatal(err)
			}
			cookie.Value, headers["X-CSRF-Token"] = session.ID, session.CSRFToken
			for i := 0; i < 5; i++ {
				result := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/reauth", bytes.NewBufferString(`{"password":"wrong credential"}`), headers, cookie)
				if result.Code != http.StatusUnauthorized {
					t.Fatalf("successful %s failed to reset budget: attempt %d = %d", kind, i+1, result.Code)
				}
			}
		})
	}
}

func TestCredentialFormatErrorsLeaveVerificationBudgetAvailable(t *testing.T) {
	fixture := newProtectedFixture(t, true, nil)
	cookie, csrf := loginForTest(t, fixture.handler)
	headers := map[string]string{"Content-Type": "application/json", "Origin": "http://nas.local", "X-CSRF-Token": csrf}
	for i := 0; i < 6; i++ {
		for _, input := range []struct{ path, body string }{
			{"password", `{"current":"wrong credential","replacement":"short"}`},
			{"username", `{"current_password":"wrong credential","username":"bad/name"}`},
			{"reauth", `{"password":`},
		} {
			response := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/"+input.path, bytes.NewBufferString(input.body), headers, cookie)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("format failure %s = %d", input.path, response.Code)
			}
		}
	}
	for i := 0; i < 5; i++ {
		response := protectedRequest(t, fixture.handler, http.MethodPost, "http://nas.local/api/manage/reauth", bytes.NewBufferString(`{"password":"wrong credential"}`), headers, cookie)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("format failures consumed verification budget: attempt %d = %d", i+1, response.Code)
		}
	}
}
