package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"example.com/nas-wallboard/internal/persist"
	"golang.org/x/crypto/argon2"
)

const (
	maximumAuthBytes    = 16 * 1024
	maximumSessions     = 32
	sessionIdleTimeout  = 12 * time.Hour
	reauthenticationTTL = 5 * time.Minute
)

var (
	ErrAlreadyConfigured  = errors.New("administrator password is already configured")
	ErrNotConfigured      = errors.New("administrator password is not configured")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type CookieMetadata struct {
	Name     string
	HTTPOnly bool
	SameSite string
}

type Session struct {
	ID        string
	CSRFToken string
	ExpiresAt time.Time
	Cookie    CookieMetadata
}

type Principal struct {
	SessionID string
	RemoteIP  string
	Username  string
}

type sessionState struct {
	csrfToken string
	remoteIP  string
	lastSeen  time.Time
}

type reauthenticationState struct {
	sessionID string
	expiresAt time.Time
}

type Manager struct {
	mu       sync.Mutex
	path     string
	clock    func() time.Time
	username string
	record   *Record
	sessions map[string]sessionState
	reauth   map[string]reauthenticationState
}

func Open(path string, clock func() time.Time) (*Manager, error) {
	if clock == nil {
		clock = time.Now
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve auth path: %w", err)
	}
	manager := &Manager{path: absolute, clock: clock, sessions: map[string]sessionState{}, reauth: map[string]reauthenticationState{}}
	data, err := persist.ReadLimited(absolute, maximumAuthBytes)
	if errors.Is(err, os.ErrNotExist) {
		return manager, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read auth record: %w", err)
	}
	username, record, legacy, err := decodeCredentialRecord(data)
	if err != nil {
		return nil, err
	}
	if legacy {
		if err := manager.writeRecord(username, record); err != nil {
			return nil, fmt.Errorf("migrate auth record: %w", err)
		}
	}
	manager.username = username
	manager.record = &record
	return manager, nil
}

func (m *Manager) Configured() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.record != nil
}

func (m *Manager) Username() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.username
}

func (m *Manager) SetInitialCredentials(username, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.record != nil {
		return ErrAlreadyConfigured
	}
	username, err := ValidateUsername(username)
	if err != nil {
		return err
	}
	record, err := HashPassword([]byte(password))
	if err != nil {
		return err
	}
	if err := m.writeRecord(username, record); err != nil {
		return err
	}
	m.username = username
	m.record = &record
	return nil
}

func (m *Manager) ChangePassword(current, replacement string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.record == nil {
		return ErrNotConfigured
	}
	if !VerifyPassword(*m.record, []byte(current)) {
		return ErrInvalidCredentials
	}
	record, err := HashPassword([]byte(replacement))
	if err != nil {
		return err
	}
	if err := m.writeRecord(m.username, record); err != nil {
		return err
	}
	m.record = &record
	clear(m.sessions)
	clear(m.reauth)
	return nil
}

func (m *Manager) ChangeUsername(currentPassword, replacement string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.record == nil {
		return ErrNotConfigured
	}
	if !VerifyPassword(*m.record, []byte(currentPassword)) {
		return ErrInvalidCredentials
	}
	username, err := ValidateUsername(replacement)
	if err != nil {
		return err
	}
	if err := m.writeRecord(username, *m.record); err != nil {
		return err
	}
	m.username = username
	clear(m.sessions)
	clear(m.reauth)
	return nil
}

func (m *Manager) Login(remoteIP, username, password string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.record == nil {
		return Session{}, ErrNotConfigured
	}
	if !loginCredentialsMatch(username, m.username, func() bool { return VerifyPassword(*m.record, []byte(password)) }) {
		return Session{}, ErrInvalidCredentials
	}
	now := m.clock()
	m.purgeExpired(now)
	for len(m.sessions) >= maximumSessions {
		m.evictOldest()
	}
	id, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	m.sessions[id] = sessionState{csrfToken: csrf, remoteIP: remoteIP, lastSeen: now}
	return Session{
		ID: id, CSRFToken: csrf, ExpiresAt: now.Add(sessionIdleTimeout),
		Cookie: CookieMetadata{Name: "wallboard_session", HTTPOnly: true, SameSite: "Strict"},
	}, nil
}

func loginCredentialsMatch(username, expectedUsername string, verifyPassword func() bool) bool {
	passwordMatches := verifyPassword()
	usernameMatches := strings.EqualFold(strings.TrimSpace(username), expectedUsername)
	return usernameMatches && passwordMatches
}

func (m *Manager) Authenticate(cookie string) (Principal, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	m.purgeExpired(now)
	state, ok := m.sessions[cookie]
	if !ok {
		return Principal{}, false
	}
	state.lastSeen = now
	m.sessions[cookie] = state
	return Principal{SessionID: cookie, RemoteIP: state.remoteIP, Username: m.username}, true
}

func (m *Manager) CSRF(sessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeExpired(m.clock())
	return m.sessions[sessionID].csrfToken
}

func (m *Manager) Logout(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
	m.deleteReauthenticationForSession(sessionID)
}

// Reauthenticate verifies the administrator password and creates a short-lived,
// single-use token bound to the current session. The token is kept only in
// memory and is intended for backup and destructive management actions.
func (m *Manager) Reauthenticate(sessionID, password string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	m.purgeExpired(now)
	if _, ok := m.sessions[sessionID]; !ok || m.record == nil || !VerifyPassword(*m.record, []byte(password)) {
		return "", ErrInvalidCredentials
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	m.reauth[token] = reauthenticationState{sessionID: sessionID, expiresAt: now.Add(reauthenticationTTL)}
	return token, nil
}

func (m *Manager) ConsumeReauthentication(sessionID, token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	m.purgeExpired(now)
	state, ok := m.reauth[token]
	if !ok || state.sessionID != sessionID || !now.Before(state.expiresAt) {
		return false
	}
	delete(m.reauth, token)
	return true
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	clear(m.sessions)
	clear(m.reauth)
}

// Reload revokes all sessions and replaces credentials after a data reset or restore.
func (m *Manager) Reload() error {
	loaded, err := Open(m.path, m.clock)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.username, m.record = loaded.username, loaded.record
	clear(m.sessions)
	clear(m.reauth)
	return nil
}

// RollbackInitialPassword removes an administrator record created by an
// incomplete first-run transaction. It must only be used while setup is not
// committed in the application state.
func (m *Manager) RollbackInitialPassword() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.Remove(m.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	m.record = nil
	m.username = ""
	clear(m.sessions)
	clear(m.reauth)
	return nil
}

func (m *Manager) writeRecord(username string, record Record) error {
	data, err := encodeCredentialRecord(username, record)
	if err != nil {
		return err
	}
	return persist.WriteAtomic(m.path, data, 0o600)
}

func (m *Manager) purgeExpired(now time.Time) {
	for id, state := range m.sessions {
		if !now.Before(state.lastSeen.Add(sessionIdleTimeout)) {
			delete(m.sessions, id)
			m.deleteReauthenticationForSession(id)
		}
	}
	for token, state := range m.reauth {
		if !now.Before(state.expiresAt) {
			delete(m.reauth, token)
		}
	}
}

func (m *Manager) deleteReauthenticationForSession(sessionID string) {
	for token, state := range m.reauth {
		if state.sessionID == sessionID {
			delete(m.reauth, token)
		}
	}
}

func (m *Manager) evictOldest() {
	var oldestID string
	var oldest time.Time
	for id, state := range m.sessions {
		if oldestID == "" || state.lastSeen.Before(oldest) {
			oldestID, oldest = id, state.lastSeen
		}
	}
	delete(m.sessions, oldestID)
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func validRecord(record Record) bool {
	salt, saltErr := base64.RawStdEncoding.DecodeString(record.Salt)
	hash, hashErr := base64.RawStdEncoding.DecodeString(record.Hash)
	return record.Algorithm == "argon2id" && record.Version == argon2.Version && record.Memory >= 8*1024 && record.Memory <= 128*1024 &&
		record.Iterations >= 1 && record.Iterations <= 10 && record.Parallelism >= 1 && record.Parallelism <= 8 &&
		saltErr == nil && len(salt) == passwordSaltBytes && hashErr == nil && len(hash) == passwordHashBytes
}
