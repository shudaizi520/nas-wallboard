package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"example.com/nas-wallboard/internal/auth"
	"example.com/nas-wallboard/internal/desktop"
)

const sessionCookieName = "wallboard_session"

func clientManagementAddress(request *http.Request, path string) string {
	version := desktop.NormalizeVersion(request.URL.Query().Get("client_version"))
	if version == "" {
		return path
	}
	return path + "?client_version=" + url.QueryEscape(version)
}

func (s *server) managePage(w http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/manage" || (request.Method != http.MethodGet && request.Method != http.MethodHead) {
		http.NotFound(w, request)
		return
	}
	if s.configState == nil || s.auth == nil || s.manager == nil {
		http.NotFound(w, request)
		return
	}
	if !s.requireTrustedLAN(w, request, false) {
		return
	}
	if !s.configState.Snapshot().SetupComplete || !s.auth.Configured() {
		http.Redirect(w, request, clientManagementAddress(request, "/setup"), http.StatusTemporaryRedirect)
		return
	}
	if _, ok := s.requestPrincipal(request); !ok {
		http.Redirect(w, request, clientManagementAddress(request, "/login"), http.StatusTemporaryRedirect)
		return
	}
	s.serveAsset(w, request, "manage.html")
}

func (s *server) setupPage(w http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/setup" || (request.Method != http.MethodGet && request.Method != http.MethodHead) {
		http.NotFound(w, request)
		return
	}
	if s.configState == nil || s.auth == nil || !s.requireTrustedLAN(w, request, false) {
		return
	}
	if s.configState.Snapshot().SetupComplete && s.auth.Configured() {
		http.Redirect(w, request, clientManagementAddress(request, "/manage"), http.StatusTemporaryRedirect)
		return
	}
	s.serveAsset(w, request, "setup.html")
}

func (s *server) loginPage(w http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/login" || (request.Method != http.MethodGet && request.Method != http.MethodHead) {
		http.NotFound(w, request)
		return
	}
	if s.configState == nil || s.auth == nil || !s.requireTrustedLAN(w, request, false) {
		return
	}
	if !s.configState.Snapshot().SetupComplete || !s.auth.Configured() {
		http.Redirect(w, request, clientManagementAddress(request, "/setup"), http.StatusTemporaryRedirect)
		return
	}
	if _, ok := s.requestPrincipal(request); ok {
		http.Redirect(w, request, clientManagementAddress(request, "/manage"), http.StatusTemporaryRedirect)
		return
	}
	s.serveAsset(w, request, "login.html")
}

func (s *server) login(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.auth == nil || s.configState == nil || !s.requireTrustedLAN(w, request, true) {
		return
	}
	if !s.configState.Snapshot().SetupComplete || !s.auth.Configured() {
		writeAPIError(w, http.StatusConflict, "setup_required")
		return
	}
	ip := requestRemoteIP(request)
	if !s.limiter.Allow(auth.ScopeLogin, ip, s.clock()) {
		w.Header().Set("Retry-After", "600")
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, request, 16*1024, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	session, err := s.auth.Login(ip, input.Username, input.Password)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	s.limiter.Reset(auth.ScopeLogin, ip)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: session.ID, Path: "/", HttpOnly: true,
		Secure: request.TLS != nil, SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "csrf": session.CSRFToken, "expires_at": session.ExpiresAt, "username": s.auth.Username()})
}

func (s *server) session(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.requireManagement(w, request)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "csrf": s.auth.CSRF(principal.SessionID), "username": principal.Username})
}

func (s *server) logout(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.requireManagement(w, request)
	if !ok || !s.requireMutation(w, request, principal) {
		return
	}
	s.auth.Logout(principal.SessionID)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Path: "/", HttpOnly: true, Secure: request.TLS != nil,
		SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) requireManagement(w http.ResponseWriter, request *http.Request) (auth.Principal, bool) {
	if s.auth == nil || s.configState == nil || !s.requireTrustedLAN(w, request, false) {
		return auth.Principal{}, false
	}
	if !s.configState.Snapshot().SetupComplete || !s.auth.Configured() {
		writeAPIError(w, http.StatusUnauthorized, "setup_required")
		return auth.Principal{}, false
	}
	principal, ok := s.requestPrincipal(request)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return auth.Principal{}, false
	}
	return principal, true
}

func (s *server) requireMutation(w http.ResponseWriter, request *http.Request, principal auth.Principal) bool {
	baseOrigin, ok := trustedBaseOrigin(request)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "invalid_host")
		return false
	}
	if request.Header.Get("Origin") != baseOrigin {
		writeAPIError(w, http.StatusForbidden, "invalid_origin")
		return false
	}
	expected := s.auth.CSRF(principal.SessionID)
	provided := request.Header.Get("X-CSRF-Token")
	if expected == "" || len(expected) != len(provided) || subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) != 1 {
		writeAPIError(w, http.StatusForbidden, "invalid_csrf")
		return false
	}
	return true
}

func (s *server) requestPrincipal(request *http.Request) (auth.Principal, bool) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return auth.Principal{}, false
	}
	return s.auth.Authenticate(cookie.Value)
}

func (s *server) requireTrustedLAN(w http.ResponseWriter, request *http.Request, mutation bool) bool {
	if !privateClient(request.RemoteAddr) {
		if mutation {
			writeAPIError(w, http.StatusForbidden, "forbidden")
		} else {
			http.Error(w, "forbidden", http.StatusForbidden)
		}
		return false
	}
	baseOrigin, ok := trustedBaseOrigin(request)
	if !ok {
		if mutation {
			writeAPIError(w, http.StatusBadRequest, "invalid_host")
		} else {
			http.Error(w, "invalid host", http.StatusBadRequest)
		}
		return false
	}
	if mutation && request.Header.Get("Origin") != baseOrigin {
		writeAPIError(w, http.StatusForbidden, "invalid_origin")
		return false
	}
	return true
}

func requestRemoteIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return strings.Trim(request.RemoteAddr, "[]")
	}
	return strings.Trim(host, "[]")
}

func decodeJSON(w http.ResponseWriter, request *http.Request, maximum int64, target any) error {
	if request.Header.Get("Content-Type") != "application/json" {
		return errors.New("content type must be application/json")
	}
	request.Body = http.MaxBytesReader(w, request.Body, maximum)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values are not allowed")
	}
	return nil
}

func writeAPIError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
