package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPasswordRoundTripAndMalformedRecords(t *testing.T) {
	record, err := HashPassword([]byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	if record.Algorithm != "argon2id" || len(record.Salt) == 0 || len(record.Hash) == 0 {
		t.Fatalf("record = %#v", record)
	}
	if !VerifyPassword(record, []byte("correct horse battery staple")) {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword(record, []byte("wrong password")) {
		t.Fatal("invalid password accepted")
	}
	for _, malformed := range []Record{{}, {Algorithm: "argon2i"}, {Algorithm: "argon2id", Salt: "not-base64", Hash: "also-bad"}} {
		if VerifyPassword(malformed, []byte("correct horse battery staple")) {
			t.Fatalf("malformed record accepted: %#v", malformed)
		}
	}
}

func TestPasswordRequiresTwelveCharacters(t *testing.T) {
	if _, err := HashPassword([]byte("short-pass")); err == nil {
		t.Fatal("short password accepted")
	}
	if _, err := HashPassword([]byte("仅有四字")); err == nil {
		t.Fatal("four-character UTF-8 password accepted")
	}
	if _, err := HashPassword([]byte("这是一个足够长的管理员密码呀")); err != nil {
		t.Fatalf("long UTF-8 password rejected: %v", err)
	}
}

func TestManagerPersistsPasswordAndKeepsSessionsInMemory(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	path := filepath.Join(t.TempDir(), "private", "auth.json")
	manager, err := Open(path, clock)
	if err != nil {
		t.Fatal(err)
	}
	if manager.Configured() {
		t.Fatal("new manager is configured")
	}
	if err := manager.SetInitialPassword("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("auth mode = %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "correct horse") {
		t.Fatal("password persisted in clear text")
	}
	var persisted Record
	if err := json.Unmarshal(data, &persisted); err != nil || !VerifyPassword(persisted, []byte("correct horse battery staple")) {
		t.Fatalf("persisted record invalid: %v", err)
	}

	session, err := manager.Login("192.168.50.4", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if len(session.ID) != 64 || len(session.CSRFToken) != 64 || !session.Cookie.HTTPOnly || session.Cookie.SameSite != "Strict" {
		t.Fatalf("session = %#v", session)
	}
	if principal, ok := manager.Authenticate(session.ID); !ok || principal.RemoteIP != "192.168.50.4" {
		t.Fatalf("principal = %#v, %v", principal, ok)
	}
	if manager.CSRF(session.ID) != session.CSRFToken {
		t.Fatal("CSRF token changed")
	}

	restarted, err := Open(path, clock)
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.Configured() {
		t.Fatal("password did not survive restart")
	}
	if _, ok := restarted.Authenticate(session.ID); ok {
		t.Fatal("session survived process restart")
	}
}

func TestManagerSessionExpiryLogoutChangeAndCap(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	manager, err := Open(filepath.Join(t.TempDir(), "auth.json"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetInitialPassword("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	first, err := manager.Login("192.168.50.4", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	manager.Logout(first.ID)
	if _, ok := manager.Authenticate(first.ID); ok {
		t.Fatal("logout kept session")
	}

	for index := 0; index < maximumSessions+1; index++ {
		if _, err := manager.Login("192.168.50.4", "correct horse battery staple"); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	if len(manager.sessions) != maximumSessions {
		t.Fatalf("sessions = %d", len(manager.sessions))
	}
	var active string
	for id := range manager.sessions {
		active = id
		break
	}
	now = now.Add(sessionIdleTimeout + time.Second)
	if _, ok := manager.Authenticate(active); ok || len(manager.sessions) != 0 {
		t.Fatal("expired sessions survived")
	}

	current, err := manager.Login("192.168.50.5", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ChangePassword("correct horse battery staple", "a different safe password"); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.Authenticate(current.ID); ok {
		t.Fatal("password change kept old sessions")
	}
	if _, err := manager.Login("192.168.50.5", "correct horse battery staple"); err == nil {
		t.Fatal("old password still works")
	}
	if _, err := manager.Login("192.168.50.5", "a different safe password"); err != nil {
		t.Fatal(err)
	}
	manager.Close()
	if len(manager.sessions) != 0 {
		t.Fatal("Close kept sessions")
	}
}

func TestRollbackInitialPasswordReturnsManagerToUnconfiguredState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	manager, err := Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetInitialPassword("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if err := manager.RollbackInitialPassword(); err != nil {
		t.Fatal(err)
	}
	if manager.Configured() {
		t.Fatal("rollback kept manager configured")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("auth file survived rollback: %v", err)
	}
}

func TestReauthenticationTokenIsSessionBoundSingleUseAndExpires(t *testing.T) {
	now := time.Unix(100, 0)
	manager, err := Open(filepath.Join(t.TempDir(), "auth.json"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetInitialPassword("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	first, _ := manager.Login("192.168.1.2", "correct horse battery staple")
	second, _ := manager.Login("192.168.1.3", "correct horse battery staple")
	token, err := manager.Reauthenticate(first.ID, "correct horse battery staple")
	if err != nil || len(token) != 64 {
		t.Fatalf("reauth = %q / %v", token, err)
	}
	if manager.ConsumeReauthentication(second.ID, token) {
		t.Fatal("token worked for another session")
	}
	if !manager.ConsumeReauthentication(first.ID, token) || manager.ConsumeReauthentication(first.ID, token) {
		t.Fatal("token was not single use")
	}
	token, _ = manager.Reauthenticate(first.ID, "correct horse battery staple")
	now = now.Add(5*time.Minute + time.Second)
	if manager.ConsumeReauthentication(first.ID, token) {
		t.Fatal("expired token accepted")
	}
	if _, err := manager.Reauthenticate(first.ID, "wrong password"); err == nil {
		t.Fatal("wrong password reauthenticated")
	}
}

func TestLimiterScopesResetAndGarbageCollection(t *testing.T) {
	limiter := NewLimiter()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	assertAllows(t, limiter, ScopeLogin, "192.168.50.10", now, 5)
	if limiter.Allow(ScopeLogin, "192.168.50.10", now) {
		t.Fatal("sixth login attempt allowed")
	}
	assertAllows(t, limiter, ScopeSetup, "192.168.50.10", now, 10)
	if limiter.Allow(ScopeSetup, "192.168.50.10", now) {
		t.Fatal("eleventh setup write allowed")
	}
	assertAllows(t, limiter, ScopeProbe, "192.168.50.10", now, 10)
	if limiter.Allow(ScopeProbe, "192.168.50.10", now) {
		t.Fatal("eleventh probe allowed")
	}

	limiter.Reset(ScopeLogin, "192.168.50.10")
	if !limiter.Allow(ScopeLogin, "192.168.50.10", now) {
		t.Fatal("login reset did not restore login bucket")
	}
	if limiter.Allow(ScopeSetup, "192.168.50.10", now) {
		t.Fatal("login reset changed setup bucket")
	}

	if !limiter.Allow(ScopeLogin, "192.168.50.11", now.Add(11*time.Minute)) {
		t.Fatal("new IP denied")
	}
	if len(limiter.buckets) != 1 {
		t.Fatalf("stale buckets not collected: %d", len(limiter.buckets))
	}
	if !limiter.Allow(ScopeProbe, "192.168.50.11", now.Add(12*time.Minute)) {
		t.Fatal("scope-isolated probe denied")
	}
}

func assertAllows(t *testing.T, limiter *Limiter, scope, ip string, now time.Time, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		if !limiter.Allow(scope, ip, now) {
			t.Fatalf("%s attempt %d denied", scope, index+1)
		}
	}
}
