package dashboard

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/model"
)

const dash = "—"

var normalAppStates = map[string]bool{"RUNNING": true, "HEALTHY": true, "READY": true, "ONLINE": true}
var failedAppStates = map[string]bool{"CRASHED": true, "ERROR": true, "FAILED": true, "FAULTED": true, "OFFLINE": true, "STOPPED": true}

type Builder struct {
	mu             sync.RWMutex
	width          int
	metrics        []config.MetricConfig
	activities     []config.ActivityConfig
	legacyDiskMode bool
}

func New(cfg config.DashboardConfig) (*Builder, error) {
	prepared, err := prepare(cfg)
	if err != nil {
		return nil, err
	}
	return &Builder{width: prepared.width, metrics: prepared.metrics, activities: prepared.activities, legacyDiskMode: prepared.legacyDiskMode}, nil
}

type preparedConfig struct {
	width          int
	metrics        []config.MetricConfig
	activities     []config.ActivityConfig
	legacyDiskMode bool
}

func prepare(cfg config.DashboardConfig) (preparedConfig, error) {
	width := cfg.Width
	if width == 0 {
		width = config.DashboardDefaultWidth
	}
	if width < config.DashboardMinWidth || width > config.DashboardMaxWidth {
		return preparedConfig{}, fmt.Errorf("dashboard width is outside the supported range")
	}

	metrics := append([]config.MetricConfig(nil), cfg.Metrics...)
	legacyDiskMode := cfg.Metrics == nil
	if cfg.Metrics == nil {
		metrics = []config.MetricConfig{{Type: config.MetricTypeCPU}, {Type: config.MetricTypeDiskTemperature}}
	}
	activities := append([]config.ActivityConfig(nil), cfg.Activities...)
	if cfg.Activities == nil {
		activities = []config.ActivityConfig{
			{Type: config.ActivityTypeTrueNASAlerts, Limit: 2},
			{Type: config.ActivityTypeAppHealth},
			{Type: config.ActivityTypeAppUpdates},
			{Type: config.ActivityTypeHomeAssistantFan},
		}
	}

	for _, metric := range metrics {
		if metric.Type != config.MetricTypeCPU && metric.Type != config.MetricTypeCPUTemperature && metric.Type != config.MetricTypeDiskTemperature && metric.Type != config.MetricTypeNetwork && metric.Type != config.MetricTypePoolCapacity {
			return preparedConfig{}, fmt.Errorf("dashboard metric %q is not available", metric.Type)
		}
	}
	for _, activity := range activities {
		switch activity.Type {
		case config.ActivityTypeTrueNASAlerts, config.ActivityTypeAppHealth, config.ActivityTypeAppUpdates, config.ActivityTypeHomeAssistantFan, config.ActivityTypeQBittorrent, config.ActivityTypePlex, config.ActivityTypeJellyfin, config.ActivityTypeUptimeKuma, config.ActivityTypeWeather, config.ActivityTypeMemoryPressure, config.ActivityTypeSMARTExceptions, config.ActivityTypeReplicationExceptions:
		default:
			return preparedConfig{}, fmt.Errorf("dashboard activity %q is not available", activity.Type)
		}
	}

	return preparedConfig{width: width, metrics: metrics, activities: activities, legacyDiskMode: legacyDiskMode}, nil
}

func (b *Builder) Update(cfg config.DashboardConfig) error {
	prepared, err := prepare(cfg)
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.width = prepared.width
	b.metrics = prepared.metrics
	b.activities = prepared.activities
	b.legacyDiskMode = prepared.legacyDiskMode
	b.mu.Unlock()
	return nil
}

func (b *Builder) Build(snapshot model.Snapshot, now time.Time) View {
	b.mu.RLock()
	width := b.width
	metrics := append([]config.MetricConfig(nil), b.metrics...)
	activities := append([]config.ActivityConfig(nil), b.activities...)
	legacyDiskMode := b.legacyDiskMode
	b.mu.RUnlock()
	view := View{Width: width, ConnectionTone: "bad", Uptime: compactUptime(snapshot.System), Metrics: make([]Metric, 0, len(metrics)), Activities: []Activity{}}
	if snapshot.Connected {
		view.ConnectionTone = "good"
	}
	for _, configured := range metrics {
		switch configured.Type {
		case config.MetricTypeCPU:
			view.Metrics = append(view.Metrics, cpuMetric(snapshot.Realtime))
		case config.MetricTypeCPUTemperature:
			view.Metrics = append(view.Metrics, cpuTemperatureMetric(snapshot.Realtime))
		case config.MetricTypeDiskTemperature:
			view.Metrics = append(view.Metrics, diskMetric(snapshot.Disks, snapshot.DiskHealth, configured.Match, legacyDiskMode))
		case config.MetricTypeNetwork:
			view.Metrics = append(view.Metrics, networkMetric(snapshot.Realtime))
		case config.MetricTypePoolCapacity:
			view.Metrics = append(view.Metrics, poolCapacityMetric(snapshot.Pools, configured))
		}
	}
	for _, configured := range activities {
		switch configured.Type {
		case config.ActivityTypeTrueNASAlerts:
			view.Activities = append(view.Activities, alertActivities(snapshot.Alerts, configured.Limit)...)
		case config.ActivityTypeAppHealth:
			view.Activities = append(view.Activities, appHealthActivities(snapshot.Apps)...)
		case config.ActivityTypeAppUpdates:
			if activity, ok := appUpdatesActivity(snapshot.Apps); ok {
				view.Activities = append(view.Activities, activity)
			}
		case config.ActivityTypeHomeAssistantFan:
			if activity, ok := fanActivity(snapshot.Home, effectiveNow(snapshot, now)); ok {
				view.Activities = append(view.Activities, activity)
			}
		case config.ActivityTypeQBittorrent:
			if activity, ok := downloadsActivity(snapshot.Downloads); ok {
				view.Activities = append(view.Activities, activity)
			}
		case config.ActivityTypePlex:
			view.Activities = append(view.Activities, limitedActivities(mediaActivities(snapshot.Plex, "plex", "Plex"), configured.Limit)...)
		case config.ActivityTypeJellyfin:
			view.Activities = append(view.Activities, limitedActivities(mediaActivities(snapshot.Jellyfin, "jellyfin", "Jellyfin"), configured.Limit)...)
		case config.ActivityTypeUptimeKuma:
			if activity, ok := monitorActivity(snapshot.Monitors); ok {
				view.Activities = append(view.Activities, activity)
			}
		case config.ActivityTypeWeather:
			if activity, ok := weatherActivity(snapshot.Weather); ok {
				view.Activities = append(view.Activities, activity)
			}
		case config.ActivityTypeMemoryPressure:
			if activity, ok := memoryPressureActivity(snapshot.Memory, configured); ok {
				view.Activities = append(view.Activities, activity)
			}
		case config.ActivityTypeSMARTExceptions:
			if activity, ok := smartExceptionsActivity(snapshot.DiskHealth); ok {
				view.Activities = append(view.Activities, activity)
			}
		case config.ActivityTypeReplicationExceptions:
			if activity, ok := replicationExceptionsActivity(snapshot.Replication); ok {
				view.Activities = append(view.Activities, activity)
			}
		}
	}
	return view
}

func limitedActivities(items []Activity, limit int) []Activity {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func compactUptime(module model.Module[model.SystemStatus]) string {
	if module.Stale || module.Error != "" || module.Data.UptimeSeconds <= 0 {
		return ""
	}
	duration := time.Duration(module.Data.UptimeSeconds) * time.Second
	days := int64(duration / (24 * time.Hour))
	hours := int64(duration/time.Hour) % 24
	minutes := int64(duration/time.Minute) % 60
	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("%d天 %d小时", days, hours)
		}
		return fmt.Sprintf("%d天", days)
	}
	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%d小时 %d分", hours, minutes)
		}
		return fmt.Sprintf("%d小时", hours)
	}
	if minutes > 0 {
		return fmt.Sprintf("%d分", minutes)
	}
	return ""
}

func monitorActivity(module model.Module[model.MonitorStatus]) (Activity, bool) {
	if module.Stale || module.Error != "" {
		return Activity{ID: "uptime-unavailable", Icon: "uptime", Tone: "bad", Title: "服务监控", Value: "不可用"}, true
	}
	if len(module.Data.DownNames) == 0 {
		return Activity{}, false
	}
	return Activity{
		ID: "uptime", Icon: "uptime", Tone: "bad", Title: "服务异常",
		Value: strconv.Itoa(len(module.Data.DownNames)), Detail: strings.Join(module.Data.DownNames, " · "),
	}, true
}

func mediaActivities(module model.Module[model.MediaStatus], id, title string) []Activity {
	if module.Stale || module.Error != "" {
		return []Activity{{ID: id + "-unavailable", Icon: "play", Tone: "bad", Title: title, Value: "不可用"}}
	}
	if len(module.Data.Sessions) == 0 {
		return nil
	}
	activities := make([]Activity, 0, len(module.Data.Sessions))
	for index, session := range module.Data.Sessions {
		activityID := id
		if len(module.Data.Sessions) > 1 {
			activityID = fmt.Sprintf("%s:%d", id, index)
		}
		value := "播放"
		if session.Paused {
			value = "暂停"
		}
		details := []string{firstNonEmpty(strings.TrimSpace(session.Title), "媒体")}
		if device := strings.TrimSpace(session.Device); device != "" {
			details = append(details, device)
		}
		activities = append(activities, Activity{
			ID: activityID, Icon: "play", Tone: "active", Title: title,
			Value: value, Detail: strings.Join(details, " · "),
		})
	}
	return activities
}

func downloadsActivity(module model.Module[model.DownloadStatus]) (Activity, bool) {
	if module.Stale || module.Error != "" {
		return Activity{ID: "downloads-unavailable", Icon: "download", Tone: "bad", Title: "下载", Value: "不可用"}, true
	}
	if module.Data.ActiveCount <= 0 {
		return Activity{}, false
	}
	value := strconv.Itoa(module.Data.ActiveCount) + " 个"
	if module.Data.DownloadBps > 0 {
		value += " · " + formatBytesPerSecond(module.Data.DownloadBps)
	}
	detail := ""
	if len(module.Data.Items) > 0 {
		item := module.Data.Items[0]
		detail = firstNonEmpty(strings.TrimSpace(item.Name), "下载任务") + " · " + compactNumber(math.Round(item.ProgressPercent)) + "%"
	}
	return Activity{ID: "downloads", Icon: "download", Tone: "active", Title: "下载", Value: value, Detail: detail}, true
}

func formatBytesPerSecond(value int64) string {
	if value >= 1_000_000 {
		return compactNumber(float64(value)/1_000_000) + " MB/s"
	}
	if value >= 1_000 {
		return compactNumber(float64(value)/1_000) + " KB/s"
	}
	return strconv.FormatInt(value, 10) + " B/s"
}

func cpuMetric(module model.Module[model.RealtimeStatus]) Metric {
	metric := Metric{ID: "cpu", Icon: "cpu", Value: dash, Tone: "bad"}
	if module.Stale || module.Error != "" || math.IsNaN(module.Data.CPUPercent) || math.IsInf(module.Data.CPUPercent, 0) {
		return metric
	}
	metric.Value = compactNumber(module.Data.CPUPercent) + "%"
	metric.Tone = "neutral"
	return metric
}

func cpuTemperatureMetric(module model.Module[model.RealtimeStatus]) Metric {
	metric := Metric{ID: "cpu_temperature", Icon: "cpu", Value: dash, Tone: "bad"}
	temperature := module.Data.CPUTemperatureCelsius
	if module.Stale || module.Error != "" || temperature <= 0 || math.IsNaN(temperature) || math.IsInf(temperature, 0) {
		return metric
	}
	metric.Value = compactNumber(temperature) + "°"
	metric.Tone = "neutral"
	return metric
}

func networkMetric(module model.Module[model.RealtimeStatus]) Metric {
	metric := Metric{ID: "network", Icon: "network", Value: dash, Tone: "bad"}
	if module.Stale || module.Error != "" || invalidNumber(module.Data.NetworkRxBps) || invalidNumber(module.Data.NetworkTxBps) {
		return metric
	}
	metric.Value = "↓ " + compactRate(module.Data.NetworkRxBps) + " ↑ " + compactRate(module.Data.NetworkTxBps)
	metric.Tone = "neutral"
	return metric
}

func compactRate(value float64) string {
	if value >= 950_000 {
		return compactNumber(value/1_000_000) + " MB/s"
	}
	if value >= 1_000 {
		return compactNumber(value/1_000) + " KB/s"
	}
	return compactNumber(value) + " B/s"
}

func invalidNumber(value float64) bool {
	return value < 0 || math.IsNaN(value) || math.IsInf(value, 0)
}

func poolCapacityMetric(module model.Module[[]model.PoolStatus], configured config.MetricConfig) Metric {
	metric := Metric{ID: config.MetricTypePoolCapacity, Icon: "disk", Value: dash, Tone: "bad"}
	if module.Stale || module.Error != "" || len(module.Data) == 0 {
		return metric
	}
	selected := module.Data[0]
	if configured.Name != "" {
		found := false
		for _, pool := range module.Data {
			if pool.Name == configured.Name {
				selected = pool
				found = true
				break
			}
		}
		if !found {
			return metric
		}
	}
	warning, critical := configured.WarningPercent, configured.CriticalPercent
	if warning <= 0 {
		warning = 80
	}
	if critical <= 0 {
		critical = 90
	}
	metric.Value = firstNonEmpty(strings.TrimSpace(selected.Name), "存储池") + " · " + compactNumber(selected.UsedPercent) + "%"
	metric.Tone = "neutral"
	if !selected.Healthy || strings.ToUpper(selected.Status) != "ONLINE" || selected.UsedPercent >= critical {
		metric.Tone = "bad"
	} else if selected.UsedPercent >= warning {
		metric.Tone = "warn"
	}
	return metric
}

func memoryPressureActivity(module model.Module[model.MemoryStatus], configured config.ActivityConfig) (Activity, bool) {
	if module.Stale || module.Error != "" || module.Data.TotalBytes == 0 {
		return Activity{ID: "memory-pressure", Icon: "cpu", Tone: "bad", Title: "可用内存", Value: "不可用"}, true
	}
	warning, critical := configured.WarningPercent, configured.CriticalPercent
	if warning <= 0 {
		warning = 15
	}
	if critical <= 0 {
		critical = 8
	}
	if module.Data.AvailablePercent > warning {
		return Activity{}, false
	}
	tone := "warn"
	if module.Data.AvailablePercent <= critical {
		tone = "bad"
	}
	return Activity{ID: "memory-pressure", Icon: "cpu", Tone: tone, Title: "可用内存", Value: "剩余 " + compactNumber(module.Data.AvailablePercent) + "%", Detail: "可用 " + compactBytes(module.Data.AvailableBytes)}, true
}

func smartExceptionsActivity(module model.Module[[]model.DiskHealthStatus]) (Activity, bool) {
	if module.Stale || module.Error != "" {
		return Activity{ID: "smart-exceptions", Icon: "disk", Tone: "bad", Title: "硬盘健康", Value: "不可用"}, true
	}
	failed, unknown := 0, 0
	for _, disk := range module.Data {
		switch disk.State {
		case "failed":
			failed++
		case "healthy":
		default:
			unknown++
		}
	}
	if failed == 0 && unknown == 0 {
		return Activity{}, false
	}
	activity := Activity{ID: "smart-exceptions", Icon: "disk", Tone: "warn", Title: "硬盘健康"}
	if failed > 0 {
		activity.Tone = "bad"
		activity.Value = fmt.Sprintf("%d 个故障", failed)
	} else {
		activity.Value = fmt.Sprintf("%d 个未知", unknown)
	}
	if unknown > 0 && failed > 0 {
		activity.Detail = fmt.Sprintf("另有 %d 个状态未知", unknown)
	}
	return activity, true
}

func replicationExceptionsActivity(module model.Module[model.ReplicationStatus]) (Activity, bool) {
	if module.Stale || module.Error != "" {
		return Activity{ID: "replication-exceptions", Icon: "alert", Tone: "bad", Title: "复制任务", Value: "不可用"}, true
	}
	if module.Data.Failed == 0 && module.Data.NeverRun == 0 {
		return Activity{}, false
	}
	activity := Activity{ID: "replication-exceptions", Icon: "alert", Tone: "warn", Title: "复制任务"}
	if module.Data.Failed > 0 {
		activity.Tone = "bad"
		activity.Value = fmt.Sprintf("%d 个失败", module.Data.Failed)
	} else {
		activity.Value = fmt.Sprintf("%d 个从未运行", module.Data.NeverRun)
	}
	if module.Data.Failed > 0 && module.Data.NeverRun > 0 {
		activity.Detail = fmt.Sprintf("%d 个从未运行", module.Data.NeverRun)
	}
	return activity, true
}

func compactBytes(value uint64) string {
	const (
		gib = uint64(1 << 30)
		mib = uint64(1 << 20)
	)
	if value >= gib {
		return compactNumber(float64(value)/float64(gib)) + " GB"
	}
	if value >= mib {
		return compactNumber(float64(value)/float64(mib)) + " MB"
	}
	return strconv.FormatUint(value, 10) + " B"
}

func diskMetric(module model.Module[[]model.DiskStatus], health model.Module[[]model.DiskHealthStatus], match config.DiskMatchConfig, legacy bool) Metric {
	metric := Metric{ID: "disk_temperature", Icon: "disk", Value: dash, Tone: "bad"}
	if module.Stale || module.Error != "" || len(module.Data) == 0 {
		return metric
	}
	var selected model.DiskStatus
	if legacy {
		selected = module.Data[0]
		for _, disk := range module.Data[1:] {
			if disk.Temperature > selected.Temperature {
				selected = disk
			}
		}
	} else {
		var err error
		selected, err = SelectDisk(module.Data, match)
		if err != nil {
			return metric
		}
	}
	if math.IsNaN(selected.Temperature) || math.IsInf(selected.Temperature, 0) {
		return metric
	}
	metric.Value = compactNumber(selected.Temperature) + "°"
	metric.Tone = "neutral"
	if state, configured := selectedDiskHealth(health, selected); configured {
		switch state {
		case "healthy":
			metric.Tone = "neutral"
		case "failed":
			metric.Value += " · SMART"
			metric.Tone = "bad"
		default:
			metric.Value += " · 未知"
			metric.Tone = "warn"
		}
	}
	return metric
}

func selectedDiskHealth(module model.Module[[]model.DiskHealthStatus], disk model.DiskStatus) (string, bool) {
	configured := !module.UpdatedAt.IsZero() || module.Error != "" || len(module.Data) > 0
	if !configured {
		return "", false
	}
	if module.Stale || module.Error != "" {
		return "unknown", true
	}
	matches := make([]model.DiskHealthStatus, 0, 1)
	for _, candidate := range module.Data {
		if candidate.Model == disk.Model && candidate.SizeBytes == disk.SizeBytes {
			matches = append(matches, candidate)
		}
	}
	if len(matches) > 1 && disk.Name != "" {
		for _, candidate := range matches {
			if candidate.Name == disk.Name {
				return candidate.State, true
			}
		}
	}
	if len(matches) == 1 {
		return matches[0].State, true
	}
	return "unknown", true
}

func weatherActivity(module model.Module[model.WeatherStatus]) (Activity, bool) {
	weather := module.Data
	if !weather.Enabled {
		return Activity{}, false
	}
	location := strings.TrimSpace(weather.Name)
	if module.Stale || module.Error != "" || invalidNumber(weather.Temperature) || strings.TrimSpace(weather.Condition) == "" {
		return Activity{ID: "weather", Icon: "weather-cloudy", Tone: "bad", Value: "不可用", Detail: location}, true
	}
	value := compactNumber(math.Round(weather.Temperature)) + "° · " + strings.TrimSpace(weather.Condition)
	detail := compactRainSummary(weather.RainSummary)
	tone := "active"
	if len(weather.Warnings) > 0 {
		warning := weather.Warnings[0]
		detail = firstNonEmpty(compactWeatherWarningTitle(warning.Title), detail)
		tone = weatherWarningTone(warning)
	}
	detail = strings.Join(nonEmptyStrings(location, detail), " · ")
	return Activity{ID: "weather", Icon: weatherIcon(weather.ConditionCode), Tone: tone, Value: value, Detail: detail}, true
}

func compactWeatherWarningTitle(value string) string {
	title := strings.TrimSpace(value)
	if index := strings.LastIndex(title, "发布"); index >= 0 {
		if summary := strings.TrimSpace(title[index+len("发布"):]); summary != "" {
			return summary
		}
	}
	return title
}

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func weatherIcon(code string) string {
	value, err := strconv.Atoi(strings.TrimSpace(code))
	if err != nil {
		return "weather-cloudy"
	}
	switch {
	case value == 100 || value == 150 || value == 900:
		return "weather-sunny"
	case value >= 101 && value <= 104 || value >= 151 && value <= 153:
		return "weather-cloudy"
	case value >= 302 && value <= 304:
		return "weather-storm"
	case value >= 300 && value <= 399:
		return "weather-rain"
	case value >= 400 && value <= 499 || value == 901:
		return "weather-snow"
	case value >= 500 && value <= 515:
		return "weather-fog"
	default:
		return "weather-cloudy"
	}
}

func compactRainSummary(value string) string {
	summary := strings.TrimSpace(value)
	switch summary {
	case "未来两小时无降水", "未来2小时无降水":
		return "两小时无雨"
	default:
		return summary
	}
}

func weatherWarningTone(warning model.WeatherWarning) string {
	color := strings.ToLower(warning.Color)
	severity := strings.ToLower(warning.Severity)
	if color == "red" || color == "orange" || color == "purple" || color == "black" || severity == "severe" || severity == "extreme" {
		return "bad"
	}
	return "warn"
}

type rankedActivity struct {
	index    int
	rank     int
	activity Activity
}

func alertActivities(module model.Module[[]model.AlertStatus], limit int) []Activity {
	if module.Stale || module.Error != "" {
		return []Activity{{ID: "alerts-unavailable", Icon: "alert", Tone: "bad", Title: "NAS 告警", Value: "不可用"}}
	}
	if limit <= 0 {
		limit = 2
	}
	ranked := make([]rankedActivity, 0, len(module.Data))
	for index, alert := range module.Data {
		level := strings.ToLower(alert.Level)
		rank, tone := alertSeverity(level)
		if rank == 0 {
			continue
		}
		title := alert.Title
		if title == "" {
			title = "告警"
		}
		ranked = append(ranked, rankedActivity{index: index, rank: rank, activity: Activity{
			ID: "alert:" + firstNonEmpty(alert.ID, alert.Title, strconv.Itoa(index)), Icon: "alert", Tone: tone,
			Title: title, Detail: alert.Message,
		}})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].rank == ranked[j].rank {
			return ranked[i].index < ranked[j].index
		}
		return ranked[i].rank > ranked[j].rank
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	activities := make([]Activity, len(ranked))
	for index := range ranked {
		activities[index] = ranked[index].activity
	}
	return activities
}

func alertSeverity(level string) (int, string) {
	switch level {
	case "critical":
		return 4, "bad"
	case "error", "faulted":
		return 3, "bad"
	case "warning", "degraded":
		return 2, "warn"
	default:
		return 0, ""
	}
}

func appHealthActivities(module model.Module[[]model.AppStatus]) []Activity {
	if module.Stale || module.Error != "" {
		return []Activity{{ID: "apps-unavailable", Icon: "app", Tone: "bad", Title: "应用", Value: "不可用"}}
	}
	activities := make([]Activity, 0)
	for index, app := range module.Data {
		state := strings.ToUpper(app.State)
		if normalAppStates[state] {
			continue
		}
		if state == "" {
			state = dash
		}
		tone := "neutral"
		if failedAppStates[state] {
			tone = "bad"
		}
		activities = append(activities, Activity{
			ID: "app:" + firstNonEmpty(app.ID, app.Name, strconv.Itoa(index)), Icon: "app", Tone: tone,
			Title: firstNonEmpty(app.Name, app.ID, "应用"), Value: state,
		})
	}
	return activities
}

func appUpdatesActivity(module model.Module[[]model.AppStatus]) (Activity, bool) {
	if module.Stale || module.Error != "" {
		return Activity{ID: "updates-unavailable", Icon: "updates", Tone: "bad", Title: "应用更新", Value: "不可用"}, true
	}
	count := 0
	for _, app := range module.Data {
		if app.UpdateAvailable {
			count++
		}
	}
	if count == 0 {
		return Activity{}, false
	}
	return Activity{ID: "updates", Icon: "updates", Tone: "active", Title: "应用更新", Value: strconv.Itoa(count)}, true
}

func fanActivity(module model.Module[model.FanStatus], now time.Time) (Activity, bool) {
	fan := module.Data
	if !fan.Enabled {
		return Activity{}, false
	}
	title := firstNonEmpty(fan.Name, "风扇")
	if module.Stale || module.Error != "" || !fan.Available || (fan.State != "on" && fan.State != "off") {
		return Activity{ID: "fan", Icon: "fan", Tone: "bad", Title: title, Value: "不可用"}, true
	}
	if fan.State == "off" {
		return Activity{}, false
	}

	elapsed := time.Duration(-1)
	if fan.OnSince != nil && !now.IsZero() {
		elapsed = now.Sub(*fan.OnSince)
	}
	warning := elapsed >= 0 && fan.RemindAfterSeconds > 0 && elapsed >= time.Duration(fan.RemindAfterSeconds)*time.Second
	tone := "active"
	if warning {
		tone = "warn"
	}
	details := make([]string, 0, 3)
	if fan.Percentage != nil {
		details = append(details, strconv.Itoa(int(math.Round(*fan.Percentage)))+"%")
	}
	if fan.PresetMode != "" {
		details = append(details, fan.PresetMode)
	}
	if fan.Oscillating != nil && *fan.Oscillating {
		details = append(details, "摇头")
	}
	value := formatElapsed(elapsed)
	if value == "" {
		value = "开启"
	}
	return Activity{ID: "fan", Icon: "fan", Tone: tone, Title: title, Value: value, Detail: strings.Join(details, " · ")}, true
}

func effectiveNow(snapshot model.Snapshot, supplied time.Time) time.Time {
	if !supplied.IsZero() {
		return supplied
	}
	if !snapshot.ServerTime.IsZero() {
		return snapshot.ServerTime
	}
	return time.Now()
}

func compactNumber(value float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(value, 'f', 1, 64), ".0")
}

func formatElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		return ""
	}
	minutes := int(elapsed / time.Minute)
	hours := minutes / 60
	remaining := minutes % 60
	if hours > 0 {
		value := strconv.Itoa(hours) + "小时"
		if remaining > 0 {
			value += strconv.Itoa(remaining) + "分"
		}
		return value
	}
	return strconv.Itoa(minutes) + "分钟"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
