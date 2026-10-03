package widget

import (
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/persist"
)

type Placement string
type Visibility string

const (
	PlacementMetric       Placement  = "metric"
	PlacementActivity     Placement  = "activity"
	VisibilityAlways      Visibility = "always"
	VisibilityNonEmpty    Visibility = "non_empty"
	VisibilityWarningOnly Visibility = "warning_only"
)

type Definition struct {
	ID              string              `json:"id"`
	IntegrationType string              `json:"integration_type,omitempty"`
	Placement       Placement           `json:"placement"`
	Label           string              `json:"label"`
	Visibility      Visibility          `json:"visibility"`
	Fields          []integration.Field `json:"fields,omitempty"`
	Defaults        map[string]any      `json:"defaults"`
	AllowMultiple   bool                `json:"allow_multiple,omitempty"`
	LegacyType      string              `json:"-"`
}

func BuiltInRegistry() (*Registry, error) {
	registry := NewRegistry()
	definitions := []Definition{
		{ID: "cpu", LegacyType: "cpu", IntegrationType: "truenas", Placement: PlacementMetric, Label: "处理器", Visibility: VisibilityAlways, Defaults: map[string]any{}},
		{ID: "cpu_temperature", LegacyType: "cpu_temperature", IntegrationType: "truenas", Placement: PlacementMetric, Label: "处理器温度", Visibility: VisibilityAlways, Defaults: map[string]any{}},
		{ID: "disk_temperature", LegacyType: "disk_temperature", IntegrationType: "truenas", Placement: PlacementMetric, Label: "机械硬盘", Visibility: VisibilityAlways, Fields: []integration.Field{
			{Key: "serial", Kind: integration.FieldText, Label: "硬盘序列号", Help: "可选；用于固定选择某一块硬盘。"},
			{Key: "model", Kind: integration.FieldText, Label: "硬盘型号", Help: "可选；序列号不可用时按型号匹配。"},
			{Key: "name", Kind: integration.FieldText, Label: "设备名称", Help: "可选；例如 sda。"},
			integerField("size_bytes", "硬盘容量", "可选；同型号硬盘可用容量辅助匹配。", 0, 0),
		}, AllowMultiple: true, Defaults: map[string]any{}},
		{ID: "network", LegacyType: "network", IntegrationType: "truenas", Placement: PlacementMetric, Label: "实时网速", Visibility: VisibilityAlways, Fields: []integration.Field{
			{Key: "interface", Kind: integration.FieldText, Label: "NAS 网络接口", Help: "显示 NAS 所选接口的接收/发送流量，不是路由器总流量或手机流量。自动模式每次选择收发总量最高的同一接口；固定接口缺失或数据不完整时显示不可用。"},
		}, Defaults: map[string]any{}},
		{ID: "pool_capacity", LegacyType: "pool_capacity", IntegrationType: "truenas", Placement: PlacementMetric, Label: "存储池容量", Visibility: VisibilityAlways, Fields: []integration.Field{
			{Key: "name", Kind: integration.FieldText, Label: "存储池名称", Help: "留空时显示第一个存储池。"},
			integerField("warning_percent", "提醒阈值", "使用率达到该百分比时显示提醒色。", 1, 99),
			integerField("critical_percent", "严重阈值", "使用率达到该百分比时显示严重色。", 1, 100),
		}, Defaults: map[string]any{"name": "", "warning_percent": 80, "critical_percent": 90}},
		{ID: "weather", LegacyType: "weather", IntegrationType: "qweather", Placement: PlacementActivity, Label: "天气", Visibility: VisibilityAlways, Defaults: map[string]any{}},
		{ID: "plex", LegacyType: "plex", IntegrationType: "plex", Placement: PlacementActivity, Label: "Plex 播放", Visibility: VisibilityNonEmpty, Fields: []integration.Field{integerField("limit", "最多显示", "限制同时显示的播放终端数量；0 表示不限制。", 0, 20)}, Defaults: map[string]any{"limit": 0}},
		{ID: "jellyfin", LegacyType: "jellyfin", IntegrationType: "jellyfin", Placement: PlacementActivity, Label: "Jellyfin 播放", Visibility: VisibilityNonEmpty, Fields: []integration.Field{integerField("limit", "最多显示", "限制同时显示的播放终端数量；0 表示不限制。", 0, 20)}, Defaults: map[string]any{"limit": 0}},
		{ID: "qbittorrent", LegacyType: "qbittorrent", IntegrationType: "qbittorrent", Placement: PlacementActivity, Label: "下载状态", Visibility: VisibilityNonEmpty, Defaults: map[string]any{}},
		{ID: "uptime_kuma", LegacyType: "uptime_kuma", IntegrationType: "uptime_kuma", Placement: PlacementActivity, Label: "网站状态", Visibility: VisibilityWarningOnly, Defaults: map[string]any{}},
		{ID: "home_assistant_fan", LegacyType: "home_assistant_fan", IntegrationType: "home_assistant", Placement: PlacementActivity, Label: "设备提醒", Visibility: VisibilityWarningOnly, Defaults: map[string]any{}},
		{ID: "truenas_alerts", LegacyType: "truenas_alerts", IntegrationType: "truenas", Placement: PlacementActivity, Label: "NAS 告警", Visibility: VisibilityWarningOnly, Fields: []integration.Field{integerField("limit", "最多显示", "限制同时显示的告警条数。", 1, 20)}, Defaults: map[string]any{"limit": 2}},
		{ID: "app_health", LegacyType: "app_health", IntegrationType: "truenas", Placement: PlacementActivity, Label: "应用异常", Visibility: VisibilityWarningOnly, Defaults: map[string]any{}},
		{ID: "app_updates", LegacyType: "app_updates", IntegrationType: "truenas", Placement: PlacementActivity, Label: "应用更新", Visibility: VisibilityWarningOnly, Defaults: map[string]any{}},
		{ID: "memory_pressure", LegacyType: "memory_pressure", IntegrationType: "truenas", Placement: PlacementActivity, Label: "可用内存提醒", Visibility: VisibilityWarningOnly, Fields: []integration.Field{
			integerField("warning_percent", "提醒阈值", "可用内存低于该百分比时显示提醒。", 1, 99),
			integerField("critical_percent", "严重阈值", "可用内存低于该百分比时显示严重提醒。", 1, 99),
		}, Defaults: map[string]any{"warning_percent": 15, "critical_percent": 8}},
		{ID: "smart_exceptions", LegacyType: "smart_exceptions", IntegrationType: "truenas", Placement: PlacementActivity, Label: "SMART 异常", Visibility: VisibilityWarningOnly, Defaults: map[string]any{}},
		{ID: "replication_exceptions", LegacyType: "replication_exceptions", IntegrationType: "truenas", Placement: PlacementActivity, Label: "复制任务异常", Visibility: VisibilityWarningOnly, Defaults: map[string]any{}},
	}
	for _, definition := range definitions {
		if err := registry.Register(definition); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func integerField(key, label, help string, minimum, maximum float64) integration.Field {
	field := integration.Field{Key: key, Kind: integration.FieldInteger, Label: label, Help: help, Minimum: &minimum}
	if maximum > minimum {
		field.Maximum = &maximum
	}
	return field
}

type LegacyItem struct {
	Type   string
	Limit  int
	Config map[string]any
}

type Layout struct {
	Revision string           `json:"revision,omitempty"`
	Width    int              `json:"width"`
	Widgets  []persist.Widget `json:"widgets"`
}

func DefaultTrueNASWidgets(sourceID string) []persist.Widget {
	definitions := []string{"cpu", "cpu_temperature", "network", "truenas_alerts"}
	result := make([]persist.Widget, 0, len(definitions))
	for index, definitionID := range definitions {
		configuration := map[string]any{}
		if definitionID == "truenas_alerts" {
			configuration["limit"] = 2
		}
		result = append(result, persist.Widget{
			ID: definitionID + "-1", DefinitionID: definitionID, IntegrationID: sourceID,
			Enabled: true, Order: index, Config: configuration,
		})
	}
	return result
}
