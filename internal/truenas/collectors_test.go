package truenas

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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

func TestCollectAlertsHonorsDismissalOnEachExistingPoll(t *testing.T) {
	caller := callerWith("alert.list",
		json.RawMessage(`[
          {"uuid":"ssh","klass":"SSHLoginFailures","level":"WARNING","formatted":"3 SSH login failures in the last 24 hours","dismissed":false},
          {"uuid":"legacy","klass":"PoolStatus","level":"CRITICAL","formatted":"Pool is degraded"},
          {"uuid":"dismissed-smart","klass":"SMART","level":"CRITICAL","dismissed":true}
        ]`),
		json.RawMessage(`[
          {"uuid":"ssh","klass":"SSHLoginFailures","level":"WARNING","dismissed":true},
          {"uuid":"legacy","klass":"PoolStatus","level":"CRITICAL"},
          {"uuid":"dismissed-smart","klass":"SMART","level":"CRITICAL","dismissed":true}
        ]`),
		json.RawMessage(`[
          {"uuid":"ssh","klass":"SSHLoginFailures","level":"WARNING","dismissed":false},
          {"uuid":"legacy","klass":"PoolStatus","level":"CRITICAL"},
          {"uuid":"dismissed-smart","klass":"SMART","level":"CRITICAL","dismissed":true}
        ]`),
		json.RawMessage(`[{"uuid":"ssh","klass":"SSHLoginFailures","dismissed":true}]`),
	)
	collectors := NewCollectors(caller, nil)
	now := time.Unix(200, 0)
	collectors.now = func() time.Time { return now }
	for poll, want := range [][]string{{"ssh", "legacy"}, {"legacy"}, {"ssh", "legacy"}, {}} {
		now = now.Add(30 * time.Second)
		got, err := collectors.CollectAlerts(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(got))
		for _, alert := range got {
			ids = append(ids, alert.ID)
		}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("poll %d: visible alerts = %v, want %v", poll, ids, want)
		}
	}
	if !reflect.DeepEqual(caller.calls, []string{"alert.list", "alert.list", "alert.list", "alert.list"}) {
		t.Fatalf("dismissal filtering added NAS queries: %v", caller.calls)
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

func TestRealtimeMissingSeriesIsUnavailable(t *testing.T) {
	caller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"reporting.netdata_graphs":   {json.RawMessage(`[]`)},
		"system.info":                {json.RawMessage(`{"physmem":100}`)},
		"reporting.netdata_get_data": {json.RawMessage(`[]`)},
	}}
	c := NewCollectors(caller, nil)
	c.now = func() time.Time { return time.Unix(200, 0) }
	got, err := c.CollectRealtime(context.Background())
	if err == nil {
		t.Fatalf("missing-series reported successful false zeros = %#v", got)
	}
}

func TestLatestMetricSkipsNullAndInvalidPreservesRealZero(t *testing.T) {
	for _, tc := range []struct {
		name, rows string
		want       float64
		found      bool
	}{
		{"terminal null", `[[42.5],[null]]`, 42.5, true},
		{"invalid terminal", `[[42.5],["bad"],[null],[]]`, 42.5, true},
		{"all null", `[[null],[null]]`, 0, false},
		{"real zero", `[[42.5],[0],[null]]`, 0, true},
		{"negative invalid", `[[42.5],[-1]]`, 42.5, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			graph := netdataWire{Legend: []string{"cpu"}}
			if err := json.Unmarshal([]byte(tc.rows), &graph.Data); err != nil {
				t.Fatal(err)
			}
			got, found := latestMetric(graph, "cpu")
			if got != tc.want || found != tc.found {
				t.Fatalf("latestMetric = %v/%v, want %v/%v", got, found, tc.want, tc.found)
			}
		})
	}
}

func TestRealtimeRequiredMetricsAndTotalCPU(t *testing.T) {
	for _, tc := range []struct {
		name, cpu, memory, rx, tx string
		wantError                 bool
		wantCPU                   float64
	}{
		{"total percent", "42.5", "20", "0", "0", false, 42.5},
		{"true zero", "0", "0", "0", "0", false, 0},
		{"missing cpu", "null", "20", "1", "1", true, 0},
		{"missing rx", "42.5", "20", "null", "1", true, 0},
		{"missing tx", "42.5", "20", "1", "null", true, 0},
		{"missing memory", "42.5", "null", "1", "1", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &fixtureCaller{responses: map[string][]json.RawMessage{
				"reporting.netdata_graphs": {json.RawMessage(`[{"name":"interface","identifiers":["eth0"]}]`)},
				"system.info":              {json.RawMessage(`{"physmem":100}`)},
				"reporting.netdata_get_data": {json.RawMessage(fmt.Sprintf(`[
				{"name":"cpu","legend":["cpu","cpu0","cpu1"],"data":[[%s,99,99],[null,null,null]]},
				{"name":"memory","legend":["available"],"data":[[%s],[null]]},
				{"name":"interface","identifier":"eth0","legend":["received","sent"],"data":[[%s,%s],[null,null]]}]`, tc.cpu, tc.memory, tc.rx, tc.tx))},
			}}
			got, err := NewCollectors(caller, nil).CollectRealtime(context.Background())
			if (err != nil) != tc.wantError {
				t.Fatalf("realtime = %#v, err = %v", got, err)
			}
			if !tc.wantError && got.CPUPercent != tc.wantCPU {
				t.Fatalf("total CPU = %v, want %v", got.CPUPercent, tc.wantCPU)
			}
		})
	}
}

func TestCollectMemoryAllNullIsUnavailable(t *testing.T) {
	caller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"system.info":                {json.RawMessage(`{"physmem":100}`)},
		"reporting.netdata_get_data": {json.RawMessage(`[{"name":"memory","legend":["available"],"data":[[null],[null]]}]`)},
	}}
	if got, err := NewCollectors(caller, nil).CollectMemory(context.Background()); err == nil {
		t.Fatalf("all-null memory falsely successful: %#v", got)
	}
}

func TestSuccessfulSourceReadsSharedWithBoundedExpiry(t *testing.T) {
	caller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"system.info":                {fixture(t, "system"), fixture(t, "system")},
		"reporting.netdata_graphs":   {fixture(t, "reporting_graphs")},
		"reporting.netdata_get_data": {fixture(t, "realtime"), fixture(t, "memory")},
		"alert.list":                 {json.RawMessage(`[{"uuid":"smart","klass":"SMART","dismissed":true}]`), json.RawMessage(`[]`)},
	}}
	c := NewCollectors(caller, nil)
	now := time.Unix(200, 0)
	c.now = func() time.Time { return now }
	ctx := context.Background()
	if _, err := c.CollectSystem(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CollectRealtime(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := c.CollectMemory(ctx); err != nil || got.AvailableBytes != 8_000_000_000 {
		t.Fatalf("reused memory = %#v/%v", got, err)
	}
	if got, err := c.CollectAlerts(ctx); err != nil || len(got) != 0 {
		t.Fatalf("regular alerts = %#v/%v", got, err)
	}
	if got, err := c.CollectDiskHealth(ctx); err != nil || len(got) != 1 {
		t.Fatalf("dismissed SMART = %#v/%v", got, err)
	}
	if len(caller.calls) != 4 {
		t.Fatalf("duplicate upstream reads: %v", caller.calls)
	}
	now = now.Add(5 * time.Second)
	if _, err := c.CollectMemory(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(25 * time.Second)
	if _, err := c.CollectDiskHealth(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	if _, err := c.CollectSystem(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(caller.calls, []string{"system.info", "reporting.netdata_graphs", "reporting.netdata_get_data", "alert.list", "reporting.netdata_get_data", "alert.list", "system.info"}) {
		t.Fatalf("expired-source calls = %v", caller.calls)
	}
}

func TestConcurrentAlertAndSMARTShareOneRawRead(t *testing.T) {
	caller := callerWith("alert.list", json.RawMessage(`[{"klass":"SMART","dismissed":true}]`))
	c := NewCollectors(caller, nil)
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); _, err := c.CollectAlerts(context.Background()); errors <- err }()
	go func() { defer wg.Done(); _, err := c.CollectDiskHealth(context.Background()); errors <- err }()
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(caller.calls) != 1 {
		t.Fatalf("concurrent raw reads = %v", caller.calls)
	}
}

func TestCachedSourceFreshnessSurvivesReuseAndFailedExpiry(t *testing.T) {
	caller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"system.info":                {json.RawMessage(`{"physmem":100}`), json.RawMessage(`{"physmem":200}`)},
		"reporting.netdata_get_data": {json.RawMessage(`[{"name":"memory","legend":["available"],"data":[[20]]}]`), json.RawMessage(`[{"name":"memory","legend":["available"],"data":[[null]]}]`), json.RawMessage(`[{"name":"memory","legend":["available"],"data":[[40]]}]`)},
		"alert.list":                 {json.RawMessage(`[{"klass":"SMART","dismissed":true}]`), json.RawMessage(`{"invalid":"not an array"}`), json.RawMessage(`[]`)},
	}}
	c := NewCollectors(caller, nil)
	now := time.Unix(200, 0)
	c.now = func() time.Time { return now }
	initial := now
	ctx := context.Background()
	if _, err := c.CollectSystem(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CollectMemory(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CollectAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, err := c.CollectSystem(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CollectMemory(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CollectDiskHealth(ctx); err != nil {
		t.Fatal(err)
	}
	for _, module := range []string{"system", "memory", "alerts", "smart"} {
		if got := c.SourceReadAt(module); !got.Equal(initial) {
			t.Fatalf("reused %s timestamp = %v, want %v", module, got, initial)
		}
	}
	now = initial.Add(30 * time.Second)
	if _, err := c.CollectAlerts(ctx); err == nil {
		t.Fatal("expired malformed alerts succeeded")
	}
	if got := c.SourceReadAt("alerts"); !got.Equal(initial) {
		t.Fatalf("failed alert advanced timestamp: %v", got)
	}
	now = initial.Add(time.Minute)
	if _, err := c.CollectDiskHealth(ctx); err != nil {
		t.Fatal(err)
	}
	if got := c.SourceReadAt("smart"); !got.Equal(now) {
		t.Fatalf("successful retry timestamp = %v, want %v", got, now)
	}
	if _, err := c.CollectMemory(ctx); err == nil {
		t.Fatal("expired null memory succeeded")
	}
	if got := c.SourceReadAt("memory"); !got.Equal(initial) {
		t.Fatalf("failed memory advanced timestamp: %v", got)
	}
	now = initial.Add(time.Minute + 5*time.Second)
	if _, err := c.CollectMemory(ctx); err != nil {
		t.Fatal(err)
	}
	if got := c.SourceReadAt("memory"); !got.Equal(initial.Add(time.Minute)) {
		t.Fatalf("memory recovery timestamp = %v, want oldest constituent %v", got, initial.Add(time.Minute))
	}
	now = now.Add(time.Minute)
	if _, err := c.CollectSystem(ctx); err == nil {
		t.Fatal("expired failed system read used old cache as success")
	}
	if got := c.SourceReadAt("system"); !got.Equal(initial) {
		t.Fatalf("failed system advanced timestamp: %v", got)
	}
}

func TestConcurrentSystemAndMemoryCoalesceSuccessfulSourceReads(t *testing.T) {
	caller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"system.info":                {json.RawMessage(`{"physmem":100}`)},
		"reporting.netdata_get_data": {json.RawMessage(`[{"name":"memory","legend":["available"],"data":[[25]]}]`)},
	}}
	c := NewCollectors(caller, nil)
	var wg sync.WaitGroup
	errors := make(chan error, 12)
	for i := 0; i < 6; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _, err := c.CollectSystem(context.Background()); errors <- err }()
		go func() {
			defer wg.Done()
			got, err := c.CollectMemory(context.Background())
			if err == nil && got.AvailablePercent != 25 {
				err = fmt.Errorf("available percent = %v, want 25", got.AvailablePercent)
			}
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(caller.calls, []string{"system.info", "reporting.netdata_get_data"}) {
		t.Fatalf("concurrent source calls=%v", caller.calls)
	}
}

func TestExpiredSystemFailureDoesNotIncreaseRealtimeQueryFrequency(t *testing.T) {
	caller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"system.info":              {json.RawMessage(`{"physmem":100}`), json.RawMessage(`[]`), json.RawMessage(`{"physmem":200}`)},
		"reporting.netdata_graphs": {json.RawMessage(`[]`)},
	}}
	c := NewCollectors(caller, nil)
	now := time.Unix(200, 0)
	c.now = func() time.Time { return now }
	if _, err := c.CollectSystem(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	for i := 0; i < 12; i++ {
		if _, err := c.CollectRealtime(context.Background()); err == nil {
			t.Fatal("failed expired metadata reported fresh realtime")
		}
		now = now.Add(5 * time.Second)
	}
	if !reflect.DeepEqual(caller.calls, []string{"system.info", "reporting.netdata_graphs", "system.info"}) {
		t.Fatalf("system failure increased NAS query rate: %v", caller.calls)
	}
	if got, err := c.CollectSystem(context.Background()); err != nil || got.MemoryTotalBytes != 200 {
		t.Fatalf("system recovery = %#v/%v", got, err)
	}
}

func TestSharedAlertFailuresAreRetriedOnlyAtOriginalCadence(t *testing.T) {
	caller := callerWith("alert.list", json.RawMessage(`{}`), json.RawMessage(`[]`))
	c := NewCollectors(caller, nil)
	now := time.Unix(200, 0)
	c.now = func() time.Time { return now }
	for i := 0; i < 6; i++ {
		if _, err := c.CollectAlerts(context.Background()); err == nil {
			t.Fatal("failed alerts reported success")
		}
		if _, err := c.CollectDiskHealth(context.Background()); err == nil {
			t.Fatal("failed SMART reported success")
		}
		now = now.Add(5 * time.Second)
	}
	if len(caller.calls) != 1 {
		t.Fatalf("failure requests exceeded alert cadence: %v", caller.calls)
	}
	if !c.SourceReadAt("alerts").IsZero() || !c.SourceReadAt("smart").IsZero() {
		t.Fatal("initial failure fabricated successful source timestamp")
	}
	if _, err := c.CollectDiskHealth(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := c.SourceReadAt("smart"); !got.Equal(now) {
		t.Fatalf("recovery timestamp=%v, want%v", got, now)
	}
	if len(caller.calls) != 2 {
		t.Fatalf("recovery calls=%v", caller.calls)
	}
}

func TestMissingMemoryFailureDoesNotCauseRepeatedGraphReadsInsideCadence(t *testing.T) {
	caller := &fixtureCaller{responses: map[string][]json.RawMessage{
		"system.info":                {json.RawMessage(`{"physmem":100}`)},
		"reporting.netdata_get_data": {json.RawMessage(`[{"name":"memory","legend":["available"],"data":[[null]]}]`), json.RawMessage(`[{"name":"memory","legend":["available"],"data":[[0]]}]`)},
	}}
	c := NewCollectors(caller, nil)
	now := time.Unix(200, 0)
	c.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if _, err := c.CollectMemory(context.Background()); err == nil {
			t.Fatal("all-null memory reported success")
		}
		now = now.Add(time.Second)
	}
	if len(caller.calls) != 2 {
		t.Fatalf("failure requests exceeded memory source cadence: %v", caller.calls)
	}
	if !c.SourceReadAt("memory").IsZero() {
		t.Fatal("initial memory failure fabricated success timestamp")
	}
	if got, err := c.CollectMemory(context.Background()); err != nil || got.AvailableBytes != 0 || got.AvailablePercent != 0 {
		t.Fatalf("real-zero recovery=%#v/%v", got, err)
	}
	if len(caller.calls) != 3 {
		t.Fatalf("recovery calls=%v", caller.calls)
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
      {"uuid":"two","klass":"SmartdAlert","formatted":"Device private-serial-b reports an error","dismissed":true},
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
