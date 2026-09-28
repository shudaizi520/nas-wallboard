package truenas

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
	"github.com/coder/websocket"
)

var (
	errProbeConnection     = errors.New("TrueNAS connection failed")
	errProbeAuthentication = errors.New("TrueNAS authentication failed")
)

type SetupProbeResult struct {
	OK          bool              `json:"ok"`
	Compatible  bool              `json:"compatible"`
	Version     string            `json:"version,omitempty"`
	Permissions []SetupPermission `json:"permissions"`
	Pools       []SetupResource   `json:"pools"`
	Disks       []SetupResource   `json:"disks"`
	Interfaces  []SetupResource   `json:"interfaces"`
	Apps        []SetupResource   `json:"apps"`
}

type SetupPermission struct {
	Feature string   `json:"feature"`
	Methods []string `json:"methods"`
	OK      bool     `json:"ok"`
	Message string   `json:"message,omitempty"`
}

type SetupResource struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Model string `json:"model,omitempty"`
}

type setupMethod struct {
	feature string
	method  string
	target  *[]map[string]any
}

var versionNumber = regexp.MustCompile(`(?:^|[^0-9])(\d{2})\.(\d{2})(?:\.|$)`)

func ProbeSetup(ctx context.Context, cfg config.TrueNASConfig, apiKey string) (SetupProbeResult, error) {
	if err := validateSetupCandidate(cfg, apiKey); err != nil {
		return SetupProbeResult{}, err
	}
	if cfg.CallTimeout.Duration <= 0 {
		cfg.CallTimeout.Duration = 10 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, cfg.CallTimeout.Duration)
	defer cancel()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.InsecureSkipVerify} // User-selected for a local self-signed NAS.
	httpClient := &http.Client{Transport: transport}
	conn, _, err := websocket.Dial(dialCtx, cfg.URL, &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		return SetupProbeResult{}, errProbeConnection
	}
	conn.SetReadLimit(maxFrameBytes)
	client := NewClient(cfg, apiKey, slog.New(slog.NewTextHandler(io.Discard, nil)))
	client.setConnection(conn)
	readCtx, stopRead := context.WithCancel(ctx)
	defer stopRead()
	go func() { _ = client.readLoop(readCtx, conn) }()
	defer client.disconnect(conn, errors.New("setup probe closed"))

	var authResult json.RawMessage
	if err := client.callOnConnection(dialCtx, conn, "auth.login_ex", []any{map[string]any{
		"mechanism": "API_KEY_PLAIN", "username": cfg.Username, "api_key": apiKey,
	}}, &authResult); err != nil || !authenticationSucceeded(authResult) {
		return SetupProbeResult{}, errProbeAuthentication
	}
	client.markReady(conn)
	return probeSetupWithCaller(ctx, client)
}

// Probe exposes the setup probe through the common integration contract while
// preserving one check per TrueNAS feature.
func Probe(ctx context.Context, cfg config.TrueNASConfig, apiKey string) integration.ProbeResult {
	setup, err := ProbeSetup(ctx, cfg, apiKey)
	if err != nil {
		switch {
		case errors.Is(err, errProbeAuthentication):
			return integration.ProbeResult{Stage: integration.ProbeStageAuthentication, Message: "TrueNAS 身份验证失败"}
		case errors.Is(err, errProbeConnection):
			return integration.ProbeResult{Stage: integration.ProbeStageTCP, Message: "无法连接 TrueNAS"}
		default:
			return integration.ProbeFailure(err)
		}
	}
	return integrationResult(setup)
}

func integrationResult(setup SetupProbeResult) integration.ProbeResult {
	result := integration.ProbeResult{Stage: integration.ProbeStageFeature, OK: setup.OK, Message: "TrueNAS 连接成功"}
	if !setup.Compatible {
		result.Stage, result.Message = integration.ProbeStageAPIVersion, "TrueNAS 版本低于 25.10"
	}
	for _, permission := range setup.Permissions {
		result.Checks = append(result.Checks, integration.ProbeCheck{Feature: permission.Feature, OK: permission.OK, Message: permission.Message})
		if !permission.OK {
			result.Stage, result.Message = integration.ProbeStagePermission, "TrueNAS 专用账户缺少读取权限"
		}
	}
	return result
}

func probeSetupWithCaller(ctx context.Context, caller Caller) (SetupProbeResult, error) {
	result := SetupProbeResult{
		Permissions: []SetupPermission{}, Pools: []SetupResource{}, Disks: []SetupResource{},
		Interfaces: []SetupResource{}, Apps: []SetupResource{},
	}
	var system map[string]any
	systemErr := caller.Call(ctx, "system.info", []any{}, &system)
	if systemErr == nil {
		result.Version = stringValue(system["version"])
		result.Compatible = supportedVersion(result.Version)
	}
	result.Permissions = append(result.Permissions, permission("系统信息", "system.info", systemErr))

	var pools, disks, interfaces, apps []map[string]any
	methods := []setupMethod{
		{feature: "存储池", method: "pool.query", target: &pools},
		{feature: "硬盘", method: "disk.query", target: &disks},
		{feature: "网络接口", method: "interface.query", target: &interfaces},
		{feature: "应用", method: "app.query", target: &apps},
	}
	allAllowed := systemErr == nil
	for _, item := range methods {
		err := caller.Call(ctx, item.method, []any{}, item.target)
		result.Permissions = append(result.Permissions, permission(item.feature, item.method, err))
		allAllowed = allAllowed && err == nil
	}
	for _, item := range []struct{ feature, method string }{{"硬盘温度", "disk.temperatures"}, {"告警", "alert.list"}, {"复制任务", "replication.query"}, {"实时性能元数据", "reporting.netdata_graphs"}} {
		var ignored any
		err := caller.Call(ctx, item.method, []any{}, &ignored)
		result.Permissions = append(result.Permissions, permission(item.feature, item.method, err))
		allAllowed = allAllowed && err == nil
	}
	var realtimeIgnored any
	now := time.Now().Unix()
	realtimeErr := caller.Call(ctx, "reporting.netdata_get_data", []any{
		[]any{map[string]any{"name": "cpu"}},
		map[string]any{"start": now - 10, "end": now, "aggregate": false},
	}, &realtimeIgnored)
	result.Permissions = append(result.Permissions, permission("实时性能数据", "reporting.netdata_get_data", realtimeErr))
	allAllowed = allAllowed && realtimeErr == nil
	result.Pools = setupResources(pools, "name")
	result.Disks = setupResources(disks, "name")
	result.Interfaces = setupResources(interfaces, "name")
	result.Apps = setupResources(apps, "name")
	result.OK = allAllowed && result.Compatible
	return result, nil
}

func validateSetupCandidate(cfg config.TrueNASConfig, apiKey string) error {
	parsed, err := url.Parse(cfg.URL)
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("TrueNAS 地址无效")
	}
	if strings.TrimSpace(cfg.Username) == "" || strings.TrimSpace(apiKey) == "" {
		return errors.New("TrueNAS 用户名和 API Key 不能为空")
	}
	return nil
}

func permission(feature, method string, err error) SetupPermission {
	value := SetupPermission{Feature: feature, Methods: []string{method}, OK: err == nil}
	if err != nil {
		value.Message = "缺少读取权限"
	}
	return value
}

func supportedVersion(value string) bool {
	parts := versionNumber.FindStringSubmatch(value)
	if len(parts) != 3 {
		return false
	}
	return parts[1] > "25" || parts[1] == "25" && parts[2] >= "10"
}

func setupResources(values []map[string]any, nameKey string) []SetupResource {
	result := make([]SetupResource, 0, len(values))
	for _, value := range values {
		name := stringValue(value[nameKey])
		id := stringValue(value["id"])
		if id == "" {
			id = stringValue(value["identifier"])
		}
		if id == "" {
			id = name
		}
		result = append(result, SetupResource{ID: id, Name: name, Model: stringValue(value["model"])})
	}
	return result
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strings.TrimSuffix(fmt.Sprintf("%.6f", typed), ".000000")
	default:
		return ""
	}
}
