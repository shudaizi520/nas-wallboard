package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagerMigratesLegacyCredentialRecordWithoutChangingPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	legacy, err := HashPassword([]byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	legacyBytes, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	legacyBytes = append(legacyBytes, '\n')
	if err := os.WriteFile(path, legacyBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	manager, err := Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if manager.Username() != "admin" {
		t.Fatalf("username = %q, want admin", manager.Username())
	}
	if _, err := manager.Login("192.168.50.4", "ADMIN", "correct horse battery staple"); err != nil {
		t.Fatalf("migrated login failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		SchemaVersion int    `json:"schema_version"`
		Username      string `json:"username"`
		Password      Record `json:"password"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.SchemaVersion != 1 || persisted.Username != "admin" || persisted.Password != legacy {
		t.Fatalf("migrated credential = %#v", persisted)
	}
}

func TestManagerRejectsInvalidCredentialRecordsWithoutOverwritingThem(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "malformed current record", data: `{"schema_version":1,"username":"admin","password":{}}`},
		{name: "unsupported future record", data: `{"schema_version":2,"username":"admin","password":{"algorithm":"argon2id"}}`},
		{name: "invalid json", data: `{"schema_version":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			original := []byte(tt.data)
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path, time.Now); err == nil {
				t.Fatal("invalid credential record accepted")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(original) {
				t.Fatalf("record overwritten: got %q, want %q", got, original)
			}
		})
	}

	t.Run("valid legacy record with trailing JSON", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "auth.json")
		record, err := HashPassword([]byte("correct horse battery staple"))
		if err != nil {
			t.Fatal(err)
		}
		original, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		original = append(original, []byte("\n{}\n")...)
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(path, time.Now); err == nil {
			t.Fatal("credential record with trailing JSON accepted")
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(original) {
			t.Fatal("credential record with trailing JSON was overwritten")
		}
	})
}

func TestValidateUsernameNormalizesAllowedIdentifiers(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "  owner  ", want: "owner"},
		{input: "管理员_01", want: "管理员_01"},
		{input: "nas.owner-2", want: "nas.owner-2"},
		{input: strings.Repeat("甲", 32), want: strings.Repeat("甲", 32)},
	}
	for _, tt := range tests {
		if got, err := ValidateUsername(tt.input); err != nil || got != tt.want {
			t.Errorf("ValidateUsername(%q) = %q, %v; want %q", tt.input, got, err, tt.want)
		}
	}
}

func TestValidateUsernameRejectsUnsafeOrOutOfRangeIdentifiers(t *testing.T) {
	for _, input := range []string{"", "  ", "ab", strings.Repeat("a", 33), "admin/name", `admin"name`, "admin name", "admin!", "admin\nname"} {
		if got, err := ValidateUsername(input); err == nil {
			t.Errorf("ValidateUsername(%q) = %q, want error", input, got)
		}
	}
}

func TestManagerUsesCaseInsensitiveUsernameAndGenericCredentialFailures(t *testing.T) {
	manager, err := Open(filepath.Join(t.TempDir(), "auth.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetInitialCredentials("Owner.Name", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Login("192.168.50.4", "owner.name", "correct horse battery staple"); err != nil {
		t.Fatalf("case-insensitive login failed: %v", err)
	}
	for _, attempt := range []struct {
		username string
		password string
	}{
		{username: "wrong", password: "correct horse battery staple"},
		{username: "Owner.Name", password: "wrong password"},
	} {
		if _, err := manager.Login("192.168.50.4", attempt.username, attempt.password); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Login(%q) error = %v, want ErrInvalidCredentials", attempt.username, err)
		}
	}
}

func TestLoginCredentialCheckAlwaysPerformsPasswordVerification(t *testing.T) {
	called := false
	matched := loginCredentialsMatch("somebody", "admin", func() bool {
		called = true
		return true
	})
	if matched {
		t.Fatal("wrong username matched")
	}
	if !called {
		t.Fatal("wrong username skipped password verification")
	}
}

func TestChangeUsernamePreservesPasswordAndInvalidatesAuthenticationState(t *testing.T) {
	manager, err := Open(filepath.Join(t.TempDir(), "auth.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetInitialCredentials("admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	first, _ := manager.Login("192.168.50.4", "admin", "correct horse battery staple")
	second, _ := manager.Login("192.168.50.5", "admin", "correct horse battery staple")
	token, err := manager.Reauthenticate(first.ID, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ChangeUsername("wrong password", "owner"); !errors.Is(err, ErrInvalidCredentials) || manager.Username() != "admin" {
		t.Fatalf("failed change = %v, username %q", err, manager.Username())
	}
	if err := manager.ChangeUsername("correct horse battery staple", "Owner"); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.Authenticate(first.ID); ok {
		t.Fatal("username change kept first session")
	}
	if _, ok := manager.Authenticate(second.ID); ok {
		t.Fatal("username change kept second session")
	}
	if manager.ConsumeReauthentication(first.ID, token) {
		t.Fatal("username change kept reauthentication token")
	}
	if _, err := manager.Login("192.168.50.4", "admin", "correct horse battery staple"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old username error = %v", err)
	}
	session, err := manager.Login("192.168.50.4", "owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	principal, ok := manager.Authenticate(session.ID)
	if !ok || principal.Username != "Owner" {
		t.Fatalf("principal = %#v, authenticated %v", principal, ok)
	}
}

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
	if err := manager.SetInitialCredentials("admin", "correct horse battery staple"); err != nil {
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
	var persisted struct {
		SchemaVersion int    `json:"schema_version"`
		Username      string `json:"username"`
		Password      Record `json:"password"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil || persisted.SchemaVersion != 1 || persisted.Username != "admin" || !VerifyPassword(persisted.Password, []byte("correct horse battery staple")) {
		t.Fatalf("persisted record invalid: %v", err)
	}

	session, err := manager.Login("192.168.50.4", "admin", "correct horse battery staple")
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
	if err := manager.SetInitialCredentials("admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	first, err := manager.Login("192.168.50.4", "admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	manager.Logout(first.ID)
	if _, ok := manager.Authenticate(first.ID); ok {
		t.Fatal("logout kept session")
	}

	for index := 0; index < maximumSessions+1; index++ {
		if _, err := manager.Login("192.168.50.4", "admin", "correct horse battery staple"); err != nil {
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

	current, err := manager.Login("192.168.50.5", "admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ChangePassword("correct horse battery staple", "a different safe password"); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.Authenticate(current.ID); ok {
		t.Fatal("password change kept old sessions")
	}
	if _, err := manager.Login("192.168.50.5", "admin", "correct horse battery staple"); err == nil {
		t.Fatal("old password still works")
	}
	if _, err := manager.Login("192.168.50.5", "admin", "a different safe password"); err != nil {
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
	if err := manager.SetInitialCredentials("admin", "correct horse battery staple"); err != nil {
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
	if err := manager.SetInitialCredentials("admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	first, _ := manager.Login("192.168.1.2", "admin", "correct horse battery staple")
	second, _ := manager.Login("192.168.1.3", "admin", "correct horse battery staple")
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
