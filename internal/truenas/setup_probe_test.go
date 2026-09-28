package truenas

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
)

func TestProbeResultListsEveryMissingPermissionByFeature(t *testing.T) {
	setup := SetupProbeResult{Compatible: true, Permissions: []SetupPermission{{Feature: "系统信息", OK: true}, {Feature: "告警", OK: false, Message: "缺少读取权限"}}}
	result := integrationResult(setup)
	if result.OK || result.Stage != integration.ProbeStagePermission || len(result.Checks) != 2 || result.Checks[1].Feature != "告警" {
		t.Fatalf("result = %#v", result)
	}
}

type setupProbeCaller struct {
	results map[string]json.RawMessage
	errors  map[string]error
	calls   []string
}

func (c *setupProbeCaller) Call(_ context.Context, method string, _ []any, out any) error {
	c.calls = append(c.calls, method)
	if err := c.errors[method]; err != nil {
		return err
	}
	return json.Unmarshal(c.results[method], out)
}

func TestProbeSetupCollectsVersionPermissionsAndDiscovery(t *testing.T) {
	caller := &setupProbeCaller{results: map[string]json.RawMessage{
		"system.info":                json.RawMessage(`{"version":"25.10.1","hostname":"atlas"}`),
		"pool.query":                 json.RawMessage(`[{"id":1,"name":"tank"}]`),
		"disk.query":                 json.RawMessage(`[{"identifier":"{serial}ABC","name":"sda","model":"ST14000"}]`),
		"interface.query":            json.RawMessage(`[{"id":"eno1","name":"eno1"}]`),
		"app.query":                  json.RawMessage(`[{"id":"plex","name":"Plex"}]`),
		"disk.temperatures":          json.RawMessage(`{}`),
		"alert.list":                 json.RawMessage(`[]`),
		"replication.query":          json.RawMessage(`[]`),
		"reporting.netdata_graphs":   json.RawMessage(`{}`),
		"reporting.netdata_get_data": json.RawMessage(`[]`),
	}}
	result, err := probeSetupWithCaller(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Version != "25.10.1" || len(result.Pools) != 1 || len(result.Disks) != 1 || len(result.Interfaces) != 1 || len(result.Apps) != 1 {
		t.Fatalf("result = %#v", result)
	}
	wantCalls := []string{"system.info", "pool.query", "disk.query", "interface.query", "app.query", "disk.temperatures", "alert.list", "replication.query", "reporting.netdata_graphs", "reporting.netdata_get_data"}
	if !reflect.DeepEqual(caller.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", caller.calls, wantCalls)
	}
	for _, permission := range result.Permissions {
		if !permission.OK || permission.Message != "" {
			t.Fatalf("permission = %#v", permission)
		}
	}
}

func TestProbeSetupReportsEveryMissingPermissionWithoutRawErrors(t *testing.T) {
	caller := &setupProbeCaller{
		results: map[string]json.RawMessage{"system.info": json.RawMessage(`{"version":"25.10.0"}`)},
		errors: map[string]error{
			"pool.query": errors.New("permission denied: api key TOP-SECRET"),
			"disk.query": errors.New("permission denied: raw response"),
			"app.query":  errors.New("permission denied"),
		},
	}
	result, err := probeSetupWithCaller(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Fatal("probe with missing permissions passed")
	}
	missing := map[string]bool{}
	for _, permission := range result.Permissions {
		if !permission.OK {
			missing[permission.Feature] = true
			if permission.Message != "缺少读取权限" {
				t.Fatalf("raw permission error exposed: %#v", permission)
			}
		}
	}
	for _, feature := range []string{"存储池", "硬盘", "应用"} {
		if !missing[feature] {
			t.Fatalf("missing feature %q not reported: %#v", feature, result.Permissions)
		}
	}
}

func TestProbeSetupRejectsUnsupportedTrueNASVersion(t *testing.T) {
	caller := &setupProbeCaller{results: map[string]json.RawMessage{
		"system.info": json.RawMessage(`{"version":"24.10.2"}`), "pool.query": json.RawMessage(`[]`),
		"disk.query": json.RawMessage(`[]`), "interface.query": json.RawMessage(`[]`), "app.query": json.RawMessage(`[]`),
		"disk.temperatures": json.RawMessage(`{}`), "alert.list": json.RawMessage(`[]`),
		"replication.query":        json.RawMessage(`[]`),
		"reporting.netdata_graphs": json.RawMessage(`{}`), "reporting.netdata_get_data": json.RawMessage(`[]`),
	}}
	result, err := probeSetupWithCaller(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Compatible {
		t.Fatalf("old version accepted: %#v", result)
	}
}

func TestProbeSetupAuthenticatesAndRunsOneShotProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		conn := acceptTestConn(t, w, request)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		authenticateTestConn(t, conn)
		results := map[string]any{
			"system.info": map[string]any{"version": "25.10.0"}, "pool.query": []any{}, "disk.query": []any{},
			"interface.query": []any{}, "app.query": []any{}, "disk.temperatures": map[string]any{},
			"alert.list": []any{}, "replication.query": []any{}, "reporting.netdata_graphs": map[string]any{}, "reporting.netdata_get_data": []any{},
		}
		for range results {
			probeRequest := readTestRequest(t, conn)
			result, ok := results[probeRequest.Method]
			if !ok {
				t.Errorf("unexpected probe method %q", probeRequest.Method)
				result = nil
			}
			writeTestResult(t, conn, probeRequest.ID, result)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := ProbeSetup(ctx, config.TrueNASConfig{
		URL: "ws" + strings.TrimPrefix(server.URL, "http"), Username: clientTestUsername,
		CallTimeout: config.Duration{Duration: time.Second},
	}, clientTestAPIKey)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Version != "25.10.0" {
		t.Fatalf("result = %#v", result)
	}
}
