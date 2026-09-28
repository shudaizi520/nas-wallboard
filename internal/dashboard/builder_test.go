package dashboard

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/model"
)

func TestBuilderKeepsConfiguredMetricAndActivityOrder(t *testing.T) {
	cfg := config.DashboardConfig{
		Width: 560,
		Metrics: []config.MetricConfig{
			{Type: config.MetricTypeDiskTemperature, Match: config.DiskMatchConfig{Model: "ST14000NM001G-2KJ103", SizeBytes: 14000519643136}},
			{Type: config.MetricTypeCPU},
		},
		Activities: []config.ActivityConfig{
			{Type: config.ActivityTypeAppUpdates},
			{Type: config.ActivityTypeTrueNASAlerts, Limit: 2},
		},
	}
	builder, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{
		Connected: true,
		Realtime:  model.Module[model.RealtimeStatus]{Data: model.RealtimeStatus{CPUPercent: 18.4}},
		Disks: model.Module[[]model.DiskStatus]{Data: []model.DiskStatus{
			{ID: "nvme0n1", Model: "NVME", SizeBytes: 512110190592, Temperature: 48},
			{ID: "sda", Model: "ST14000NM001G-2KJ103", SizeBytes: 14000519643136, Temperature: 40},
		}},
		Apps:   model.Module[[]model.AppStatus]{Data: []model.AppStatus{{ID: "qbittorrent", State: "RUNNING", UpdateAvailable: true}}},
		Alerts: model.Module[[]model.AlertStatus]{Data: []model.AlertStatus{{ID: "a1", Level: "warning", Title: "SMART", Message: "温度偏高"}}},
	}

	view := builder.Build(snapshot, time.Unix(100, 0))
	if view.Width != 560 || view.ConnectionTone != "good" {
		t.Fatalf("view header = %#v", view)
	}
	wantMetrics := []Metric{
		{ID: "disk_temperature", Icon: "disk", Value: "40°", Tone: "neutral"},
		{ID: "cpu", Icon: "cpu", Value: "18.4%", Tone: "neutral"},
	}
	if !equalMetrics(view.Metrics, wantMetrics) {
		t.Fatalf("metrics = %#v, want %#v", view.Metrics, wantMetrics)
	}
	if len(view.Activities) != 2 || view.Activities[0].ID != "updates" || view.Activities[1].ID != "alert:a1" {
		t.Fatalf("activities = %#v", view.Activities)
	}
}

func TestBuilderFormatsCompactNASUptime(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 460, Metrics: []config.MetricConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		system model.Module[model.SystemStatus]
		want   string
	}{
		{name: "days omit noisy minutes", system: model.Module[model.SystemStatus]{Data: model.SystemStatus{UptimeSeconds: 5*24*60*60 + 6*60*60 + 54*60}}, want: "5天 6小时"},
		{name: "hours retain minutes", system: model.Module[model.SystemStatus]{Data: model.SystemStatus{UptimeSeconds: 6*60*60 + 54*60}}, want: "6小时 54分"},
		{name: "stale system hides uptime", system: model.Module[model.SystemStatus]{Stale: true, Data: model.SystemStatus{UptimeSeconds: 5 * 24 * 60 * 60}}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := builder.Build(model.Snapshot{System: tt.system}, time.Unix(100, 0))
			if view.Uptime != tt.want {
				t.Fatalf("uptime = %q, want %q", view.Uptime, tt.want)
			}
		})
	}
}

func TestBuilderShowsNetworkDiskHealthAndWeather(t *testing.T) {
	match := config.DiskMatchConfig{Model: "ST14000NM001G-2KJ103", SizeBytes: 14000519643136}
	builder, err := New(config.DashboardConfig{
		Width:      560,
		Metrics:    []config.MetricConfig{{Type: config.MetricTypeCPU}, {Type: "cpu_temperature"}, {Type: config.MetricTypeDiskTemperature, Match: match}, {Type: config.MetricTypeNetwork}},
		Activities: []config.ActivityConfig{{Type: config.ActivityTypeWeather}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var realtime model.RealtimeStatus
	if err := json.Unmarshal([]byte(`{"cpu_percent":9,"cpu_temperature_celsius":56,"network_rx_bps":986700,"network_tx_bps":977800}`), &realtime); err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{
		Connected:  true,
		Realtime:   model.Module[model.RealtimeStatus]{Data: realtime},
		Disks:      model.Module[[]model.DiskStatus]{Data: []model.DiskStatus{{Name: "sda", Model: match.Model, SizeBytes: match.SizeBytes, Temperature: 39}}},
		DiskHealth: model.Module[[]model.DiskHealthStatus]{UpdatedAt: time.Unix(50, 0), Data: []model.DiskHealthStatus{{Name: "sda", Model: match.Model, SizeBytes: match.SizeBytes, State: "healthy"}}},
		Weather:    model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Name: "深圳", Temperature: 29.4, Condition: "多云", ConditionCode: "101", RainSummary: "未来两小时无降水", Source: "和风天气"}},
	}
	view := builder.Build(snapshot, time.Unix(100, 0))
	wantMetrics := []Metric{
		{ID: "cpu", Icon: "cpu", Value: "9%", Tone: "neutral"},
		{ID: "cpu_temperature", Icon: "cpu", Value: "56°", Tone: "neutral"},
		{ID: "disk_temperature", Icon: "disk", Value: "39°", Tone: "neutral"},
		{ID: "network", Icon: "network", Value: "↓ 1 MB/s ↑ 1 MB/s", Tone: "neutral"},
	}
	if !equalMetrics(view.Metrics, wantMetrics) {
		t.Fatalf("metrics = %#v, want %#v", view.Metrics, wantMetrics)
	}
	wantWeather := Activity{ID: "weather", Icon: "weather-cloudy", Tone: "active", Value: "29° · 多云", Detail: "深圳 · 两小时无雨"}
	if len(view.Activities) != 1 || view.Activities[0] != wantWeather {
		t.Fatalf("activities = %#v, want %#v", view.Activities, wantWeather)
	}
}

func TestBuilderUpdateChangesLayoutWithoutRestart(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 460, Metrics: []config.MetricConfig{{Type: config.MetricTypeCPU}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Update(config.DashboardConfig{Width: 520, Metrics: []config.MetricConfig{{Type: config.MetricTypeNetwork}}}); err != nil {
		t.Fatal(err)
	}
	view := builder.Build(model.Snapshot{Realtime: model.Module[model.RealtimeStatus]{Data: model.RealtimeStatus{NetworkRxBps: 1_000_000, NetworkTxBps: 2_000_000}}}, time.Now())
	if view.Width != 520 || len(view.Metrics) != 1 || view.Metrics[0].ID != "network" {
		t.Fatalf("updated view = %#v", view)
	}
}

func TestBuilderHighlightsSMARTFailureAndWeatherWarning(t *testing.T) {
	match := config.DiskMatchConfig{Model: "ST14000NM001G-2KJ103", SizeBytes: 14000519643136}
	builder, err := New(config.DashboardConfig{
		Width:      560,
		Metrics:    []config.MetricConfig{{Type: config.MetricTypeDiskTemperature, Match: match}},
		Activities: []config.ActivityConfig{{Type: config.ActivityTypeWeather}},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{
		Disks:      model.Module[[]model.DiskStatus]{Data: []model.DiskStatus{{Name: "sda", Model: match.Model, SizeBytes: match.SizeBytes, Temperature: 42}}},
		DiskHealth: model.Module[[]model.DiskHealthStatus]{UpdatedAt: time.Unix(50, 0), Data: []model.DiskHealthStatus{{Name: "sda", Model: match.Model, SizeBytes: match.SizeBytes, State: "failed"}}},
		Weather:    model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Name: "深圳", Temperature: 31, Condition: "雷阵雨", ConditionCode: "302", RainSummary: "正在降雨", Source: "和风天气", Warnings: []model.WeatherWarning{{Title: "深圳市气象台发布高温黄色预警", Severity: "severe", Color: "orange"}}}},
	}
	view := builder.Build(snapshot, time.Unix(100, 0))
	if len(view.Metrics) != 1 || view.Metrics[0] != (Metric{ID: "disk_temperature", Icon: "disk", Value: "42° · SMART", Tone: "bad"}) {
		t.Fatalf("metrics = %#v", view.Metrics)
	}
	wantWeather := Activity{ID: "weather", Icon: "weather-storm", Tone: "bad", Value: "31° · 雷阵雨", Detail: "深圳 · 高温黄色预警"}
	if len(view.Activities) != 1 || view.Activities[0] != wantWeather {
		t.Fatalf("activities = %#v, want %#v", view.Activities, wantWeather)
	}
}

func TestWeatherActivityUsesConditionSpecificIcon(t *testing.T) {
	tests := []struct {
		code string
		text string
		want string
	}{
		{code: "100", text: "晴", want: "weather-sunny"},
		{code: "104", text: "阴", want: "weather-cloudy"},
		{code: "305", text: "小雨", want: "weather-rain"},
		{code: "302", text: "雷阵雨", want: "weather-storm"},
		{code: "400", text: "小雪", want: "weather-snow"},
		{code: "501", text: "雾", want: "weather-fog"},
		{code: "999", text: "未知", want: "weather-cloudy"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			activity, ok := weatherActivity(model.Module[model.WeatherStatus]{Data: model.WeatherStatus{
				Enabled: true, Temperature: 29, Condition: tt.text, ConditionCode: tt.code,
			}})
			if !ok || activity.Icon != tt.want {
				t.Fatalf("weather icon = %q, want %q", activity.Icon, tt.want)
			}
		})
	}
}

func TestBuilderLimitsAndSortsActionableAlerts(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{{Type: config.ActivityTypeTrueNASAlerts, Limit: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{Alerts: model.Module[[]model.AlertStatus]{Data: []model.AlertStatus{
		{ID: "info", Level: "info", Title: "完成"},
		{ID: "warning", Level: "warning", Title: "温度", Message: "偏高"},
		{ID: "critical", Level: "critical", Title: "存储池", Message: "降级"},
	}}}

	view := builder.Build(snapshot, time.Time{})
	if len(view.Activities) != 1 || view.Activities[0] != (Activity{ID: "alert:critical", Icon: "alert", Tone: "bad", Title: "存储池", Detail: "降级"}) {
		t.Fatalf("activities = %#v", view.Activities)
	}
}

func TestBuilderShowsOnlyAbnormalAppsAndCollapsesUpdates(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{
		{Type: config.ActivityTypeAppHealth}, {Type: config.ActivityTypeAppUpdates},
	}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{Apps: model.Module[[]model.AppStatus]{Data: []model.AppStatus{
		{ID: "plex", Name: "Plex", State: "RUNNING", UpdateAvailable: true},
		{ID: "immich", Name: "Immich", State: "STOPPED", UpdateAvailable: true},
		{ID: "future", Name: "Future", State: "MYSTERY"},
	}}}

	view := builder.Build(snapshot, time.Time{})
	if len(view.Activities) != 3 || view.Activities[0].ID != "app:immich" || view.Activities[0].Tone != "bad" || view.Activities[1].ID != "app:future" || view.Activities[1].Tone != "neutral" || view.Activities[2].ID != "updates" || view.Activities[2].Value != "2" {
		t.Fatalf("activities = %#v", view.Activities)
	}
}

func TestBuilderFanHidesOffAndWarnsAfterThreshold(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{{Type: config.ActivityTypeHomeAssistantFan}}})
	if err != nil {
		t.Fatal(err)
	}
	off := model.Snapshot{Home: model.Module[model.FanStatus]{Data: model.FanStatus{Enabled: true, Available: true, Name: "客厅风扇", State: "off"}}}
	if got := builder.Build(off, time.Unix(100, 0)).Activities; len(got) != 0 {
		t.Fatalf("off fan activities = %#v", got)
	}

	started := time.Date(2026, 9, 27, 6, 30, 0, 0, time.UTC)
	percentage := 42.0
	oscillating := true
	on := model.Snapshot{Home: model.Module[model.FanStatus]{Data: model.FanStatus{
		Enabled: true, Available: true, Name: "客厅风扇", State: "on", OnSince: &started,
		Percentage: &percentage, PresetMode: "nature", Oscillating: &oscillating, RemindAfterSeconds: 7200,
	}}}
	got := builder.Build(on, time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)).Activities
	want := Activity{ID: "fan", Icon: "fan", Tone: "warn", Title: "客厅风扇", Value: "2小时30分", Detail: "42% · nature · 摇头"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("on fan = %#v, want %#v", got, want)
	}
}

func TestBuilderIsolatesStaleAndAmbiguousMetrics(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Metrics: []config.MetricConfig{
		{Type: config.MetricTypeCPU},
		{Type: config.MetricTypeDiskTemperature, Match: config.DiskMatchConfig{Model: "same", SizeBytes: 100}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{
		Connected: true,
		Realtime:  model.Module[model.RealtimeStatus]{Data: model.RealtimeStatus{CPUPercent: 99}, Stale: true, Error: "timeout"},
		Disks: model.Module[[]model.DiskStatus]{Data: []model.DiskStatus{
			{ID: "sda", Model: "same", SizeBytes: 100, Temperature: 40},
			{ID: "sdb", Model: "same", SizeBytes: 100, Temperature: 41},
		}},
	}

	view := builder.Build(snapshot, time.Time{})
	want := []Metric{
		{ID: "cpu", Icon: "cpu", Value: "—", Tone: "bad"},
		{ID: "disk_temperature", Icon: "disk", Value: "—", Tone: "bad"},
	}
	if !equalMetrics(view.Metrics, want) {
		t.Fatalf("metrics = %#v, want %#v", view.Metrics, want)
	}
}

func TestBuilderShowsConfiguredFanAsUnavailableWhenStale(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{{Type: config.ActivityTypeHomeAssistantFan}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{Home: model.Module[model.FanStatus]{
		Data: model.FanStatus{Enabled: true, Available: true, Name: "客厅风扇", State: "off"}, Stale: true, Error: "timeout",
	}}
	got := builder.Build(snapshot, time.Time{}).Activities
	want := Activity{ID: "fan", Icon: "fan", Tone: "bad", Title: "客厅风扇", Value: "不可用"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("stale fan = %#v, want %#v", got, want)
	}
}

func TestBuilderShowsActiveDownloadsAndHidesIdle(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{{Type: config.ActivityTypeQBittorrent}}})
	if err != nil {
		t.Fatal(err)
	}
	idle := model.Snapshot{Downloads: model.Module[model.DownloadStatus]{Data: model.DownloadStatus{}}}
	if got := builder.Build(idle, time.Time{}).Activities; len(got) != 0 {
		t.Fatalf("idle downloads = %#v", got)
	}

	active := model.Snapshot{Downloads: model.Module[model.DownloadStatus]{Data: model.DownloadStatus{
		ActiveCount: 2,
		DownloadBps: 12_500_000,
		Items:       []model.DownloadItem{{Name: "Ubuntu.iso", ProgressPercent: 68.4, DownloadBps: 12_500_000}},
	}}}
	got := builder.Build(active, time.Time{}).Activities
	want := Activity{ID: "downloads", Icon: "download", Tone: "active", Title: "下载", Value: "2 个 · 12.5 MB/s", Detail: "Ubuntu.iso · 68%"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("downloads = %#v, want %#v", got, want)
	}
}

func TestBuilderShowsDownloadsUnavailableWithoutHidingOtherActivities(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{{Type: config.ActivityTypeQBittorrent}, {Type: config.ActivityTypeAppUpdates}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{
		Downloads: model.Module[model.DownloadStatus]{Stale: true, Error: "unauthorized"},
		Apps:      model.Module[[]model.AppStatus]{Data: []model.AppStatus{{ID: "plex", UpdateAvailable: true}}},
	}
	got := builder.Build(snapshot, time.Time{}).Activities
	if len(got) != 2 || got[0] != (Activity{ID: "downloads-unavailable", Icon: "download", Tone: "bad", Title: "下载", Value: "不可用"}) || got[1].ID != "updates" {
		t.Fatalf("activities = %#v", got)
	}
}

func TestBuilderShowsPlaybackAndHidesIdleMedia(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{{Type: config.ActivityTypePlex}, {Type: config.ActivityTypeJellyfin}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{
		Plex:     model.Module[model.MediaStatus]{Data: model.MediaStatus{Sessions: []model.MediaSession{{Title: "剧集 · 第一集", Device: "客厅电视", Paused: true}}}},
		Jellyfin: model.Module[model.MediaStatus]{Data: model.MediaStatus{}},
	}
	got := builder.Build(snapshot, time.Time{}).Activities
	want := Activity{ID: "plex", Icon: "play", Tone: "active", Title: "Plex", Value: "暂停", Detail: "剧集 · 第一集 · 客厅电视"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("media activities = %#v, want %#v", got, want)
	}
}

func TestBuilderShowsEachMediaSessionInItsOwnActivity(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{{Type: config.ActivityTypePlex}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{Plex: model.Module[model.MediaStatus]{Data: model.MediaStatus{Sessions: []model.MediaSession{
		{Title: "电影", Device: "客厅电视"},
		{Title: "音乐", Device: "卧室音箱", Paused: true},
	}}}}

	got := builder.Build(snapshot, time.Time{}).Activities
	want := []Activity{
		{ID: "plex:0", Icon: "play", Tone: "active", Title: "Plex", Value: "播放", Detail: "电影 · 客厅电视"},
		{ID: "plex:1", Icon: "play", Tone: "active", Title: "Plex", Value: "暂停", Detail: "音乐 · 卧室音箱"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("media activities = %#v, want %#v", got, want)
	}
}

func TestBuilderAppliesPerWidgetMediaSessionLimitWithoutMergingSessions(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{{Type: config.ActivityTypePlex, Limit: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{Plex: model.Module[model.MediaStatus]{Data: model.MediaStatus{Sessions: []model.MediaSession{
		{Title: "第一首", Device: "电视"}, {Title: "第二首", Device: "手机"}, {Title: "第三首", Device: "音箱"},
	}}}}
	got := builder.Build(snapshot, time.Time{}).Activities
	if len(got) != 2 || got[0].ID != "plex:0" || got[1].ID != "plex:1" || got[0].Detail == got[1].Detail {
		t.Fatalf("limited sessions = %#v", got)
	}
}

func TestBuilderShowsSelectedPoolCapacityWithConfigurableThresholds(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Metrics: []config.MetricConfig{{
		Type: config.MetricTypePoolCapacity, Name: "archive", WarningPercent: 80, CriticalPercent: 90,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{Pools: model.Module[[]model.PoolStatus]{Data: []model.PoolStatus{
		{Name: "tank", Status: "ONLINE", Healthy: true, UsedPercent: 55},
		{Name: "archive", Status: "ONLINE", Healthy: true, UsedBytes: 920, TotalBytes: 1000, UsedPercent: 92},
	}}}
	got := builder.Build(snapshot, time.Time{}).Metrics
	want := Metric{ID: "pool_capacity", Icon: "disk", Value: "archive · 92%", Tone: "bad"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("pool metric = %#v, want %#v", got, want)
	}

	degraded := snapshot
	degraded.Pools.Data[1].UsedPercent = 20
	degraded.Pools.Data[1].Status = "DEGRADED"
	if got := builder.Build(degraded, time.Time{}).Metrics[0]; got.Tone != "bad" {
		t.Fatalf("degraded pool tone = %#v", got)
	}
}

func TestBuilderEmitsOnlyActionableMemorySMARTAndReplicationWarnings(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{
		{Type: config.ActivityTypeMemoryPressure, WarningPercent: 15, CriticalPercent: 8},
		{Type: config.ActivityTypeSMARTExceptions},
		{Type: config.ActivityTypeReplicationExceptions},
	}})
	if err != nil {
		t.Fatal(err)
	}
	healthy := model.Snapshot{
		Memory:      model.Module[model.MemoryStatus]{Data: model.MemoryStatus{TotalBytes: 100, AvailableBytes: 40, AvailablePercent: 40}},
		DiskHealth:  model.Module[[]model.DiskHealthStatus]{Data: []model.DiskHealthStatus{{State: "healthy"}}},
		Replication: model.Module[model.ReplicationStatus]{Data: model.ReplicationStatus{Total: 2, Enabled: 1, Disabled: 1}},
	}
	if got := builder.Build(healthy, time.Time{}).Activities; len(got) != 0 {
		t.Fatalf("healthy snapshot produced cards: %#v", got)
	}

	warning := healthy
	warning.Memory.Data = model.MemoryStatus{TotalBytes: 32 << 30, AvailableBytes: 4 << 30, AvailablePercent: 12.5}
	warning.DiskHealth.Data = []model.DiskHealthStatus{{State: "healthy"}, {State: "failed"}, {State: "unknown"}}
	warning.Replication.Data = model.ReplicationStatus{Total: 4, Enabled: 3, Disabled: 1, NeverRun: 1, Failed: 1}
	got := builder.Build(warning, time.Time{}).Activities
	want := []Activity{
		{ID: "memory-pressure", Icon: "cpu", Tone: "warn", Title: "可用内存", Value: "剩余 12.5%", Detail: "可用 4 GB"},
		{ID: "smart-exceptions", Icon: "disk", Tone: "bad", Title: "硬盘健康", Value: "1 个故障", Detail: "另有 1 个状态未知"},
		{ID: "replication-exceptions", Icon: "alert", Tone: "bad", Title: "复制任务", Value: "1 个失败", Detail: "1 个从未运行"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %#v, want %#v", got, want)
	}
}

func TestBuilderKeepsMediaSourceFailuresIndependent(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{{Type: config.ActivityTypePlex}, {Type: config.ActivityTypeJellyfin}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{
		Plex: model.Module[model.MediaStatus]{Stale: true, Error: "timeout"},
		Jellyfin: model.Module[model.MediaStatus]{Data: model.MediaStatus{Sessions: []model.MediaSession{
			{Title: "电影", Device: "网页"}, {Title: "剧集", Device: "电视"},
		}}},
	}
	got := builder.Build(snapshot, time.Time{}).Activities
	want := []Activity{
		{ID: "plex-unavailable", Icon: "play", Tone: "bad", Title: "Plex", Value: "不可用"},
		{ID: "jellyfin:0", Icon: "play", Tone: "active", Title: "Jellyfin", Value: "播放", Detail: "电影 · 网页"},
		{ID: "jellyfin:1", Icon: "play", Tone: "active", Title: "Jellyfin", Value: "播放", Detail: "剧集 · 电视"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("media activities = %#v", got)
	}
}

func TestBuilderShowsOnlyFailedMonitors(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{{Type: config.ActivityTypeUptimeKuma}}})
	if err != nil {
		t.Fatal(err)
	}
	healthy := model.Snapshot{Monitors: model.Module[model.MonitorStatus]{Data: model.MonitorStatus{Total: 8}}}
	if got := builder.Build(healthy, time.Time{}).Activities; len(got) != 0 {
		t.Fatalf("healthy monitors = %#v", got)
	}
	down := model.Snapshot{Monitors: model.Module[model.MonitorStatus]{Data: model.MonitorStatus{Total: 8, DownNames: []string{"博客", "路由器"}}}}
	got := builder.Build(down, time.Time{}).Activities
	want := Activity{ID: "uptime", Icon: "uptime", Tone: "bad", Title: "服务异常", Value: "2", Detail: "博客 · 路由器"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("monitor activity = %#v, want %#v", got, want)
	}
}

func TestBuilderShowsMonitorSourceUnavailable(t *testing.T) {
	builder, err := New(config.DashboardConfig{Width: 560, Activities: []config.ActivityConfig{{Type: config.ActivityTypeUptimeKuma}}})
	if err != nil {
		t.Fatal(err)
	}
	got := builder.Build(model.Snapshot{Monitors: model.Module[model.MonitorStatus]{Stale: true, Error: "timeout"}}, time.Time{}).Activities
	want := Activity{ID: "uptime-unavailable", Icon: "uptime", Tone: "bad", Title: "服务监控", Value: "不可用"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("monitor activity = %#v, want %#v", got, want)
	}
}

func equalMetrics(got, want []Metric) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
