package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"example.com/nas-wallboard/internal/auth"
	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/dashboard"
	"example.com/nas-wallboard/internal/desktop"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/persist"
	"example.com/nas-wallboard/internal/state"
	"example.com/nas-wallboard/internal/truenas"
	"example.com/nas-wallboard/internal/update"
	"example.com/nas-wallboard/internal/widget"
)

type SetupProbeFunc func(context.Context, config.TrueNASConfig, string) (truenas.SetupProbeResult, error)

type IntegrationRuntime interface {
	Apply(context.Context, []persist.Integration, []persist.Integration) error
}

type Dependencies struct {
	RuntimeStore       *state.Store
	Assets             fs.FS
	Version            string
	Builder            *dashboard.Builder
	DashboardManager   *dashboard.SettingsManager
	State              *persist.Store
	Secrets            *persist.SecretStore
	Auth               *auth.Manager
	Limiter            *auth.Limiter
	SetupProbe         SetupProbeFunc
	DataRoot           string
	Clock              func() time.Time
	Integrations       *integration.Service
	IntegrationRuntime IntegrationRuntime
	Widgets            *widget.Service
	Updates            *update.Checker
}

type server struct {
	operationMu        sync.RWMutex
	setupMu            sync.Mutex
	recoveryRequired   bool
	store              *state.Store
	assets             fs.FS
	version            string
	builder            *dashboard.Builder
	manager            *dashboard.SettingsManager
	configState        *persist.Store
	secrets            *persist.SecretStore
	auth               *auth.Manager
	limiter            *auth.Limiter
	setupProbe         SetupProbeFunc
	dataRoot           string
	clock              func() time.Time
	integrations       *integration.Service
	integrationRuntime IntegrationRuntime
	widgets            *widget.Service
	updates            *update.Checker
	startedAt          time.Time
}

func New(dependencies Dependencies) http.Handler {
	if dependencies.Limiter == nil {
		dependencies.Limiter = auth.NewLimiter()
	}
	if dependencies.SetupProbe == nil {
		dependencies.SetupProbe = truenas.ProbeSetup
	}
	if dependencies.Clock == nil {
		dependencies.Clock = time.Now
	}
	s := &server{
		store: dependencies.RuntimeStore, assets: dependencies.Assets, version: dependencies.Version,
		builder: dependencies.Builder, manager: dependencies.DashboardManager, configState: dependencies.State,
		secrets: dependencies.Secrets, auth: dependencies.Auth, limiter: dependencies.Limiter,
		setupProbe: dependencies.SetupProbe, dataRoot: dependencies.DataRoot, clock: dependencies.Clock,
		integrations: dependencies.Integrations, integrationRuntime: dependencies.IntegrationRuntime,
		widgets: dependencies.Widgets, updates: dependencies.Updates, startedAt: dependencies.Clock(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", s.status)
	mux.HandleFunc("/api/dashboard", s.dashboard)
	mux.HandleFunc("/api/setup/status", s.setupStatus)
	mux.HandleFunc("/api/setup/probe", s.setupProbeHandler)
	mux.HandleFunc("/api/setup/complete", s.setupComplete)
	mux.HandleFunc("/api/auth/login", s.login)
	mux.HandleFunc("/api/auth/logout", s.logout)
	mux.HandleFunc("/api/auth/session", s.session)
	mux.HandleFunc("/api/manage/dashboard", s.manageDashboard)
	mux.HandleFunc("/api/manage/integrations", s.manageIntegrations)
	mux.HandleFunc("/api/manage/integrations/", s.manageIntegrations)
	mux.HandleFunc("/api/manage/layout", s.manageLayout)
	mux.HandleFunc("/api/manage/overview", s.manageOverview)
	mux.HandleFunc("/api/manage/support", s.manageSupport)
	mux.HandleFunc("/api/manage/reauth", s.manageReauthenticate)
	mux.HandleFunc("/api/manage/backup", s.manageBackup)
	mux.HandleFunc("/api/manage/restore", s.manageRestore)
	mux.HandleFunc("/api/manage/password", s.managePassword)
	mux.HandleFunc("/api/manage/username", s.manageUsername)
	mux.HandleFunc("/api/manage/reset", s.manageReset)
	mux.HandleFunc("/api/manage/update", s.manageUpdate)
	mux.HandleFunc("/download/nas-wallboard-desktop.zip", s.desktopDownload)
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/readyz", s.ready)
	mux.HandleFunc("/manage", s.managePage)
	mux.HandleFunc("/setup", s.setupPage)
	mux.HandleFunc("/login", s.loginPage)
	mux.HandleFunc("/", s.static)
	return securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/manage/reset" || r.URL.Path == "/api/manage/restore" || r.URL.Path == "/api/manage/backup" {
			s.operationMu.Lock()
			defer s.operationMu.Unlock()
		} else {
			s.operationMu.RLock()
			defer s.operationMu.RUnlock()
		}
		if s.recoveryRequired {
			writeAPIError(w, http.StatusServiceUnavailable, "data_recovery_required")
			return
		}
		mux.ServeHTTP(w, r)
	}))
}

func (s *server) desktopDownload(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !privateClient(request.RemoteAddr) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	origin, ok := requestOrigin(request)
	if !ok {
		http.Error(w, "invalid host", http.StatusBadRequest)
		return
	}
	if err := desktop.ValidatePackage(s.assets, origin); err != nil {
		http.Error(w, "desktop client unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="nas-wallboard-desktop.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	if request.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := desktop.WritePackage(w, s.assets, origin); err != nil {
		return
	}
}

func requestOrigin(request *http.Request) (string, bool) {
	base, ok := trustedBaseOrigin(request)
	return base + "/", ok
}

func trustedBaseOrigin(request *http.Request) (string, bool) {
	if strings.TrimSpace(request.Host) != request.Host || strings.ContainsAny(request.Host, "\r\n\t") || strings.HasSuffix(request.Host, ":") {
		return "", false
	}
	parsed, err := url.Parse("//" + request.Host)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	hostname := parsed.Hostname()
	ip := net.ParseIP(hostname)
	trustedName := strings.EqualFold(hostname, "localhost") ||
		strings.HasSuffix(strings.ToLower(hostname), ".local") ||
		!strings.Contains(hostname, ".")
	if ip != nil {
		if !ip.IsPrivate() && !ip.IsLoopback() {
			return "", false
		}
	} else if !trustedName {
		return "", false
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + request.Host, true
}

func (s *server) manageDashboard(w http.ResponseWriter, request *http.Request) {
	if s.manager == nil {
		http.NotFound(w, request)
		return
	}
	principal, ok := s.requireManagement(w, request)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	switch request.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(s.manager.View())
	case http.MethodPut:
		if !s.requireMutation(w, request, principal) {
			return
		}
		if request.Header.Get("Content-Type") != "application/json" {
			writeAPIError(w, http.StatusUnsupportedMediaType, "content_type")
			return
		}
		request.Body = http.MaxBytesReader(w, request.Body, 32*1024)
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var next dashboard.Settings
		if err := decoder.Decode(&next); err != nil {
			http.Error(w, "invalid settings", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			http.Error(w, "invalid settings", http.StatusBadRequest)
			return
		}
		if err := s.manager.Update(next); err != nil {
			http.Error(w, "invalid settings", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(s.manager.View())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) dashboard(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Wallboard-Version", s.version)
	w.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(s.builder.Build(s.store.Snapshot(), time.Now()))
}

func (s *server) status(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Wallboard-Version", s.version)
	w.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(s.store.Snapshot())
}

func (s *server) health(w http.ResponseWriter, request *http.Request) {
	jsonStatus(w, request, http.StatusOK, "ok")
}

func (s *server) ready(w http.ResponseWriter, request *http.Request) {
	if s.store.Ready() {
		jsonStatus(w, request, http.StatusOK, "ready")
		return
	}
	jsonStatus(w, request, http.StatusServiceUnavailable, "not_ready")
}

func jsonStatus(w http.ResponseWriter, request *http.Request, status int, value string) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if request.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": value})
	}
}

func (s *server) static(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
	if strings.HasPrefix(name, "downloads/") {
		http.NotFound(w, request)
		return
	}
	if name == "." || name == "" {
		name = "index.html"
		if _, err := fs.Stat(s.assets, name); err != nil {
			name = "placeholder.html"
		}
	}
	s.serveAsset(w, request, name)
}

func (s *server) serveAsset(w http.ResponseWriter, request *http.Request, name string) {
	if !fs.ValidPath(name) {
		http.NotFound(w, request)
		return
	}
	data, err := fs.ReadFile(s.assets, name)
	if err != nil {
		http.NotFound(w, request)
		return
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	switch path.Ext(name) {
	case ".html", ".css", ".js":
		w.Header().Set("Cache-Control", "no-cache")
	default:
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, request, name, time.Time{}, bytes.NewReader(data))
}

func privateClient(remoteAddress string) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback())
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if request.URL.Path == "/" && request.URL.Query().Get("desktop") == "1" && request.URL.Query().Get("preview") != "" {
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; base-uri 'none'; frame-ancestors 'self'")
		} else {
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		}
		next.ServeHTTP(w, request)
	})
}
