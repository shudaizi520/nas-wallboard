package truenas

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/config"
)

type fixtureCaller struct {
	responses map[string][]json.RawMessage
	calls     []string
	params    [][]any
}

func (f *fixtureCaller) Call(_ context.Context, method string, params []any, out any) error {
	f.calls = append(f.calls, method)
	f.params = append(f.params, params)
	queue := f.responses[method]
	if len(queue) == 0 {
		return fmt.Errorf("unexpected method or exhausted fixture: %s", method)
	}
	f.responses[method] = queue[1:]
	return json.Unmarshal(queue[0], out)
}

func fixture(t *testing.T, name string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func callerWith(method string, responses ...json.RawMessage) *fixtureCaller {
	return &fixtureCaller{responses: map[string][]json.RawMessage{method: responses}}
}

func TestCollectSystemUsesSanitizedFieldsAndFallbacks(t *testing.T) {
	caller := callerWith("system.info", fixture(t, "system"))
	collectors := NewCollectors(caller, nil)

	got, err := collectors.CollectSystem(context.Background())
	if err != nil {
		t.Fatalf("CollectSystem() error = %v", err)
	}
	if got.Hostname != "atlas-nas" || got.Version != "25.10.7" || got.UptimeSeconds != 93784 || got.MemoryTotalBytes != 34359738368 {
		t.Fatalf("system = %#v", got)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "must-never-escape") || strings.Contains(string(encoded), "internal_build") {
		t.Fatalf("sanitized model leaked raw fields: %s", encoded)
	}
	if !reflect.DeepEqual(caller.calls, []string{"system.info"}) {
		t.Fatalf("calls = %#v", caller.calls)
	}

	missing := callerWith("system.info", json.RawMessage(`{"hostname":"minimal"}`))
	fallback, err := NewCollectors(missing, nil).CollectSystem(context.Background())
	if err != nil || fallback.Hostname != "minimal" || fallback.MemoryTotalBytes != 0 || fallback.UptimeSeconds != 0 {
		t.Fatalf("missing-field fallback = %#v, %v", fallback, err)
	}
}

func TestCollectRealtimeNormalizesMemoryAndStartsRatesAtZero(t *testing.T) {
	caller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"reporting.netdata_graphs":   {fixture(t, "reporting_graphs")},
		"system.info":                {fixture(t, "system")},
		"reporting.netdata_get_data": {fixture(t, "realtime")},
	}}
	collectors := NewCollectors(caller, nil)
	collectors.now = func() time.Time { return time.Unix(200, 0) }

	got, err := collectors.CollectRealtime(context.Background())
	if err != nil {
		t.Fatalf("CollectRealtime() error = %v", err)
	}
	if got.CPUPercent != 14 || got.MemoryTotalBytes != 34359738368 || got.MemoryUsedBytes != 26359738368 {
		t.Fatalf("realtime = %#v", got)
	}
	if got.NetworkRxBps != 1_000_000 || got.NetworkTxBps != 500_000 {
		t.Fatalf("rates = %v/%v, want 1000000/500000", got.NetworkRxBps, got.NetworkTxBps)
	}
	encoded, _ := json.Marshal(got)
	if !strings.Contains(string(encoded), `"cpu_temperature_celsius":47`) {
		t.Fatalf("CPU temperature should use the hottest core instead of TrueNAS's broken aggregate: %s", encoded)
	}
	if !reflect.DeepEqual(caller.calls, []string{"reporting.netdata_graphs", "system.info", "reporting.netdata_get_data"}) {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if len(caller.params[2]) != 2 {
		t.Fatalf("netdata params = %#v, want graphs and time query", caller.params[2])
	}
}

func TestLatestCPUTemperatureFallsBackToAggregateWithoutCoreDimensions(t *testing.T) {
	graph := netdataWire{
		Legend: []string{"time", "cpu"},
		Data:   [][]json.RawMessage{{json.RawMessage(`200`), json.RawMessage(`43`)}},
	}

	got, found := latestCPUTemperature(graph)
	if !found || got != 43 {
		t.Fatalf("latestCPUTemperature() = %v, %t; want 43, true", got, found)
	}
}

func TestCollectPoolsNormalizesBytesPercentAndUnknownState(t *testing.T) {
	caller := callerWith("pool.query", fixture(t, "pools"))
	got, err := NewCollectors(caller, nil).CollectPools(context.Background())
	if err != nil {
		t.Fatalf("CollectPools() error = %v", err)
	}
	if len(got) != 2 || got[0].ID != "1" || got[0].UsedBytes != 1000 || got[0].TotalBytes != 4000 || got[0].UsedPercent != 25 {
		t.Fatalf("pools = %#v", got)
	}
	if got[1].Status != "UNKNOWN" || got[1].UsedPercent != 0 {
		t.Fatalf("unknown pool = %#v", got[1])
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "private-layout") {
		t.Fatalf("pool model leaked topology: %s", encoded)
	}
	if !reflect.DeepEqual(caller.calls, []string{"pool.query"}) {
		t.Fatalf("calls = %#v", caller.calls)
	}
}

func TestCollectDisksSortsAndSkipsMissingTemperature(t *testing.T) {
	caller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"disk.temperatures": {fixture(t, "disks")},
		"disk.query":        {json.RawMessage(`[]`)},
	}}
	got, err := NewCollectors(caller, nil).CollectDisks(context.Background())
	if err != nil {
		t.Fatalf("CollectDisks() error = %v", err)
	}
	if len(got) != 2 || got[0].Name != "nvme0n1" || got[0].Temperature != 47.5 || got[1].Name != "sdb" {
		t.Fatalf("disks = %#v", got)
	}
	if !reflect.DeepEqual(caller.calls, []string{"disk.temperatures", "disk.query"}) {
		t.Fatalf("calls = %#v", caller.calls)
	}
}

func TestCollectDisksJoinsStableIdentity(t *testing.T) {
	caller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"disk.temperatures": {json.RawMessage(`{"sda":40,"nvme0n1":48}`)},
		"disk.query": {json.RawMessage(`[
          {"name":"sda","model":"ST14000NM001G-2KJ103","serial":"private-hdd-serial","size":14000519643136},
          {"name":"nvme0n1","model":"E2M2 64GB","serial":"private-nvme-serial","size":61865982976}
        ]`)},
	}}

	got, err := NewCollectors(caller, nil).CollectDisks(context.Background())
	if err != nil {
		t.Fatalf("CollectDisks() error = %v", err)
	}
	if len(got) != 2 || got[1].ID != "sda" || got[1].Model != "ST14000NM001G-2KJ103" || got[1].Serial != "private-hdd-serial" || got[1].SizeBytes != 14000519643136 {
		t.Fatalf("disks = %#v", got)
	}
}

func TestCollectAppsUsesSelectedOrderNamesAndUnknownState(t *testing.T) {
	selected := []config.AppConfig{
		{ID: "plex", Name: "Plex", Sort: 20, Link: "https://plex.example.invalid"},
		{ID: "missing", Name: "Missing", Sort: 30},
		{ID: "photos", Name: "Photos", Sort: 10},
	}
	caller := callerWith("app.query", fixture(t, "apps"))
	got, err := NewCollectors(caller, selected).CollectApps(context.Background())
	if err != nil {
		t.Fatalf("CollectApps() error = %v", err)
	}
	if len(got) != 3 || got[0].ID != "photos" || got[0].State != "UNKNOWN" || got[1].ID != "plex" || !got[1].UpdateAvailable || got[2].ID != "missing" || got[2].State != "UNKNOWN" {
		t.Fatalf("apps = %#v", got)
	}
	if got[1].Name != "Plex" || got[1].Link != "https://plex.example.invalid" {
		t.Fatalf("display overrides missing: %#v", got[1])
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "discard-me") || strings.Contains(string(encoded), "not-selected") {
		t.Fatalf("app model leaked raw fields: %s", encoded)
	}
	if !reflect.DeepEqual(caller.calls, []string{"app.query"}) {
		t.Fatalf("calls = %#v", caller.calls)
	}
}

func TestCollectAppsWithoutLegacySelectionReturnsAllAppsForPublicWidgets(t *testing.T) {
	caller := callerWith("app.query", fixture(t, "apps"))
	got, err := NewCollectors(caller, nil).CollectApps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID > got[1].ID || got[1].ID > got[2].ID {
		t.Fatalf("public apps = %#v", got)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "discard-me") {
		t.Fatalf("public apps leaked raw field: %s", encoded)
	}
}

func TestCollectAlertsMapsSeverityAndTimestamps(t *testing.T) {
	caller := callerWith("alert.list", fixture(t, "alerts"))
	got, err := NewCollectors(caller, nil).CollectAlerts(context.Background())
	if err != nil {
		t.Fatalf("CollectAlerts() error = %v", err)
	}
	if len(got) != 3 || got[0].Level != "critical" || got[1].Level != "warning" || got[2].Level != "unknown" {
		t.Fatalf("alerts = %#v", got)
	}
	if got[0].Title != "PoolStatus" || got[0].OccurredAt.Format(time.RFC3339) != "2026-09-27T06:30:00Z" || got[1].OccurredAt.IsZero() {
		t.Fatalf("alert normalization = %#v", got)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "secret_detail") || strings.Contains(string(encoded), "discard-me") {
		t.Fatalf("alert model leaked raw fields: %s", encoded)
	}
	if !reflect.DeepEqual(caller.calls, []string{"alert.list"}) {
		t.Fatalf("calls = %#v", caller.calls)
	}
}

func TestRealtimeCachesGraphMetadataAndMemoryTotal(t *testing.T) {
	caller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"reporting.netdata_graphs":   {fixture(t, "reporting_graphs")},
		"system.info":                {fixture(t, "system")},
		"reporting.netdata_get_data": {fixture(t, "realtime"), fixture(t, "realtime")},
	}}
	c := NewCollectors(caller, nil)
	c.now = func() time.Time { return time.Unix(200, 0) }
	if _, err := c.CollectRealtime(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CollectRealtime(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"reporting.netdata_graphs", "system.info", "reporting.netdata_get_data", "reporting.netdata_get_data"}
	if !reflect.DeepEqual(caller.calls, want) {
		t.Fatalf("calls = %#v, want %#v", caller.calls, want)
	}
}

func TestRealtimeMissingSeriesDefaultsToZero(t *testing.T) {
	caller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"reporting.netdata_graphs":   {json.RawMessage(`[]`)},
		"system.info":                {json.RawMessage(`{"physmem":100}`)},
		"reporting.netdata_get_data": {json.RawMessage(`[]`)},
	}}
	c := NewCollectors(caller, nil)
	c.now = func() time.Time { return time.Unix(200, 0) }
	got, err := c.CollectRealtime(context.Background())
	if err != nil || got.CPUPercent != 0 || got.MemoryUsedBytes != 0 || got.NetworkRxBps != 0 || got.NetworkTxBps != 0 {
		t.Fatalf("missing-series fallback = %#v, %v", got, err)
	}
}

func TestCollectMemoryReturnsOnlyReducedCapacityAndPressure(t *testing.T) {
	caller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"system.info":                {json.RawMessage(`{"physmem":34359738368}`)},
		"reporting.netdata_get_data": {fixture(t, "memory")},
	}}
	collectors := NewCollectors(caller, nil)
	collectors.now = func() time.Time { return time.Unix(200, 0) }
	got, err := collectors.CollectMemory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalBytes != 34359738368 || got.AvailableBytes != 2147483648 || got.AvailablePercent != 6.25 {
		t.Fatalf("memory = %#v", got)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "must-never-escape") {
		t.Fatalf("memory leaked source fields: %s", encoded)
	}

	partialCaller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"system.info":                {json.RawMessage(`{"physmem":null}`)},
		"reporting.netdata_get_data": {json.RawMessage(`[{"name":"memory","legend":["available"],"data":[["bad"]]}]`)},
	}}
	partial, err := NewCollectors(partialCaller, nil).CollectMemory(context.Background())
	if err == nil || partial.TotalBytes != 0 || partial.AvailablePercent != 0 {
		t.Fatalf("partial memory = %#v / %v", partial, err)
	}
}

func TestCollectDiskHealthReducesActiveSMARTAlertsWithoutLeakingDetails(t *testing.T) {
	caller := callerWith("alert.list", json.RawMessage(`[
      {"uuid":"one","klass":"SMART","formatted":"Device private-serial-a failed health checks"},
      {"uuid":"two","klass":"SmartdAlert","formatted":"Device private-serial-b reports an error"},
      {"uuid":"three","klass":"PoolStatus","formatted":"Pool is degraded"}
    ]`))
	got, err := NewCollectors(caller, nil).CollectDiskHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "SMART 告警" || got[0].State != "failed" || got[1].State != "failed" {
		t.Fatalf("SMART summary = %#v", got)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "private-") || strings.Contains(string(encoded), "Device") {
		t.Fatalf("SMART model leaked raw alert details: %s", encoded)
	}
}

func TestCollectReplicationCountsDisabledNeverRunFailedAndRunningWithoutTaskDetails(t *testing.T) {
	caller := callerWith("replication.query", fixture(t, "replication"))
	got, err := NewCollectors(caller, nil).CollectReplication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 5 || got.Enabled != 4 || got.Disabled != 1 || got.NeverRun != 1 || got.Failed != 1 || got.Running != 1 {
		t.Fatalf("replication = %#v", got)
	}
	encoded, _ := json.Marshal(got)
	for _, forbidden := range []string{"private", "destination", "remote"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("replication leaked %q: %s", forbidden, encoded)
		}
	}

	malformed := callerWith("replication.query", json.RawMessage(`{"not":"an array"}`))
	if _, err := NewCollectors(malformed, nil).CollectReplication(context.Background()); err == nil {
		t.Fatal("malformed replication response accepted")
	}
}
