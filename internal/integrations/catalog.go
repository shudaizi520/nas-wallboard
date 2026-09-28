package integrations

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/homeassistant"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/jellyfin"
	"example.com/nas-wallboard/internal/plex"
	"example.com/nas-wallboard/internal/qbittorrent"
	"example.com/nas-wallboard/internal/scrutiny"
	"example.com/nas-wallboard/internal/truenas"
	"example.com/nas-wallboard/internal/uptimekuma"
	"example.com/nas-wallboard/internal/weather"
)

type builtInDefinition struct {
	id           string
	metadata     integration.Metadata
	fields       []integration.Field
	capabilities []integration.Capability
	runtime      *RuntimeOptions
}

func (definition builtInDefinition) ID() string                     { return definition.id }
func (definition builtInDefinition) Metadata() integration.Metadata { return definition.metadata }
func (definition builtInDefinition) Fields() []integration.Field {
	return append([]integration.Field(nil), definition.fields...)
}
func (definition builtInDefinition) Validate(settings integration.Config) error {
	if err := integration.ValidateFields(definition.fields, settings); err != nil {
		return err
	}
	if definition.id == "qweather" {
		host := strings.TrimSpace(stringSetting(settings, "api_host", ""))
		parsed, err := url.Parse("https://" + host)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Port() != "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("和风天气 API 主机无效")
		}
		latitude, latitudeErr := strconv.ParseFloat(stringSetting(settings, "latitude", ""), 64)
		longitude, longitudeErr := strconv.ParseFloat(stringSetting(settings, "longitude", ""), 64)
		if latitudeErr != nil || longitudeErr != nil || latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
			return errors.New("天气经纬度无效")
		}
	}
	return nil
}
func (definition builtInDefinition) Test(ctx context.Context, settings integration.Config, secrets integration.Secrets) integration.ProbeResult {
	result := probeBuiltIn(ctx, definition.id, settings, secrets)
	if result.OK {
		result.Capabilities = append([]integration.Capability(nil), definition.capabilities...)
	}
	return result
}
func (definition builtInDefinition) Capabilities(integration.Config) []integration.Capability {
	return append([]integration.Capability(nil), definition.capabilities...)
}
func (definition builtInDefinition) Collector(settings integration.Config, secrets integration.Secrets) (integration.Collector, error) {
	if definition.runtime == nil || definition.runtime.Store == nil {
		return nil, errors.New("integration collector is not initialized")
	}
	return buildCollector(*definition.runtime, definition.id, definition.metadata, settings, secrets)
}

func BuiltInRegistry(options ...RuntimeOptions) (*integration.Registry, error) {
	registry := integration.NewRegistry()
	for _, raw := range builtIns() {
		definition := raw
		if len(options) > 0 {
			configured := raw.(builtInDefinition)
			configured.runtime = &options[0]
			definition = configured
		}
		if err := registry.Register(definition); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func builtIns() []integration.Definition {
	return []integration.Definition{
		definition("truenas", "TrueNAS 系统", "读取系统、存储、网络、应用、告警和保护任务状态。", "server", "系统", 5*time.Second, true, []string{"truenas"},
			[]integration.Field{urlField("url", "TrueNAS 地址", "填写 TrueNAS WebSocket API 地址，例如 wss://nas.local/api/current。"), textField("username", "专用账户", "填写只授予读取权限的 TrueNAS 服务账户。"), boolField("insecure_skip_verify", "自签名证书", "仅在局域网使用自签名证书时跳过证书链验证。", false), durationField("call_timeout", "请求超时", "限制单次 TrueNAS 请求等待时间。", "10s"), secretField("api_key", "API 密钥", "填写专用 TrueNAS 账户创建的 API Key。")},
			capabilities("system", "系统状态", "metric", "storage", "存储状态", "metric", "alerts", "系统告警", "activity", "memory", "内存压力", "activity", "pool_capacity", "存储池容量", "metric", "smart", "硬盘健康", "activity", "replication", "复制任务", "activity", "app_exceptions", "应用异常", "activity")),
		definition("qweather", "和风天气", "显示实时天气、未来两小时降雨和气象预警。", "weather", "环境", 10*time.Minute, false, nil,
			[]integration.Field{textField("api_host", "API 主机", "填写和风天气控制台分配的专属 API 主机名。"), textField("latitude", "纬度", "填写天气位置的十进制纬度。"), textField("longitude", "经度", "填写天气位置的十进制经度。"), textDefaultField("name", "显示位置", "填写小组件中显示的地区名称。", "天气"), selectField("units", "计量单位", "选择温度和风速使用的计量单位。", "metric", integration.Option{Value: "metric", Label: "公制"}, integration.Option{Value: "imperial", Label: "英制"}), durationField("call_timeout", "请求超时", "限制单次天气请求等待时间。", "8s"), secretField("api_key", "API 密钥", "填写和风天气项目使用的 API Key。")},
			capabilities("weather", "天气动态", "activity")),
		definition("plex", "Plex 媒体", "为每个播放终端分别显示当前播放会话。", "play", "媒体", 15*time.Second, false, []string{"plex"}, mediaFields("Plex", "token", "访问令牌"), capabilities("sessions", "播放会话", "activity")),
		definition("jellyfin", "Jellyfin 媒体", "为每个 Jellyfin 播放终端分别显示当前会话。", "play", "媒体", 15*time.Second, false, []string{"jellyfin"}, mediaFields("Jellyfin", "token", "访问令牌"), capabilities("sessions", "播放会话", "activity")),
		definition("qbittorrent", "qBittorrent 下载", "显示活动下载、进度、速度和预计完成时间。", "download", "下载", 15*time.Second, false, []string{"qbittorrent"},
			[]integration.Field{urlField("url", "服务地址", "填写 qBittorrent Web UI 的局域网地址。"), textField("username", "账户名称", "填写只用于读取下载状态的账户。"), durationField("call_timeout", "请求超时", "限制单次下载状态请求等待时间。", "5s"), secretField("password", "账户密码", "填写 qBittorrent Web UI 账户密码。")}, capabilities("downloads", "下载状态", "activity")),
		definition("uptime_kuma", "Uptime Kuma 监控", "显示不可用的监控项和监控数据源状态。", "uptime", "监控", 30*time.Second, false, []string{"uptime-kuma", "uptime_kuma"},
			[]integration.Field{urlField("url", "服务地址", "填写 Uptime Kuma 的局域网地址。"), durationField("call_timeout", "请求超时", "限制单次监控请求等待时间。", "5s"), secretField("api_key", "API 密钥", "填写 Metrics 页面使用的 API Key。")}, capabilities("monitors", "网站状态", "activity")),
		definition("home_assistant", "Home Assistant 实体", "只读显示实体状态，可用于风扇长时间运行提醒。", "home", "智能家居", 30*time.Second, false, []string{"home-assistant", "home_assistant"},
			[]integration.Field{urlField("url", "服务地址", "填写 Home Assistant 的局域网地址。"), entityField("entity_id", "实体编号", "填写要读取的实体，例如 fan.living_room。"), textDefaultField("name", "显示名称", "填写桌面上显示的设备名称。", "设备"), durationField("remind_after", "提醒时长", "实体持续开启超过此时长后显示提醒。", "2h"), durationField("call_timeout", "请求超时", "限制单次实体请求等待时间。", "5s"), secretField("token", "长期令牌", "填写只读 Home Assistant 长期访问令牌。")}, capabilities("entity", "实体状态", "activity")),
		definition("scrutiny", "Scrutiny 硬盘健康", "读取 Scrutiny 汇总的硬盘 SMART 健康状态。", "disk", "存储", 30*time.Minute, false, []string{"scrutiny"},
			[]integration.Field{urlField("url", "服务地址", "填写 Scrutiny Web 服务的局域网地址。"), durationField("call_timeout", "请求超时", "限制单次硬盘健康请求等待时间。", "5s")}, capabilities("smart", "硬盘健康", "activity")),
	}
}

func definition(id, name, description, icon, category string, refresh time.Duration, required bool, hints []string, fields []integration.Field, caps []integration.Capability) integration.Definition {
	return builtInDefinition{id: id, metadata: integration.Metadata{Name: name, Description: description, Icon: icon, Category: category, MinimumRefresh: refresh, SingleInstance: true, Required: required, DiscoveryHints: hints}, fields: fields, capabilities: caps}
}

func urlField(key, label, help string) integration.Field {
	return integration.Field{Key: key, Kind: integration.FieldURL, Label: label, Help: help, Required: true}
}
func textField(key, label, help string) integration.Field {
	return integration.Field{Key: key, Kind: integration.FieldText, Label: label, Help: help, Required: true}
}
func textDefaultField(key, label, help, value string) integration.Field {
	return integration.Field{Key: key, Kind: integration.FieldText, Label: label, Help: help, Default: value}
}
func boolField(key, label, help string, value bool) integration.Field {
	return integration.Field{Key: key, Kind: integration.FieldBoolean, Label: label, Help: help, Default: value}
}
func durationField(key, label, help, value string) integration.Field {
	return integration.Field{Key: key, Kind: integration.FieldDuration, Label: label, Help: help, Default: value}
}
func entityField(key, label, help string) integration.Field {
	return integration.Field{Key: key, Kind: integration.FieldEntityID, Label: label, Help: help, Required: true}
}
func secretField(key, label, help string) integration.Field {
	return integration.Field{Key: key, Kind: integration.FieldSecret, Label: label, Help: help, Required: true}
}
func selectField(key, label, help, value string, options ...integration.Option) integration.Field {
	return integration.Field{Key: key, Kind: integration.FieldSelect, Label: label, Help: help, Default: value, Options: options}
}

func mediaFields(name, secretKey, secretLabel string) []integration.Field {
	return []integration.Field{urlField("url", "服务地址", fmt.Sprintf("填写 %s 服务的局域网地址。", name)), durationField("call_timeout", "请求超时", "限制单次播放会话请求等待时间。", "5s"), secretField(secretKey, secretLabel, fmt.Sprintf("填写 %s 生成的只读访问令牌。", name))}
}

func capabilities(values ...string) []integration.Capability {
	result := make([]integration.Capability, 0, len(values)/3)
	for index := 0; index < len(values); index += 3 {
		result = append(result, integration.Capability{ID: values[index], Label: values[index+1], Kind: values[index+2]})
	}
	return result
}

func probeBuiltIn(ctx context.Context, id string, settings integration.Config, secrets integration.Secrets) integration.ProbeResult {
	switch id {
	case "truenas":
		return truenas.Probe(ctx, config.TrueNASConfig{URL: stringSetting(settings, "url", ""), Username: stringSetting(settings, "username", ""), InsecureSkipVerify: boolSetting(settings, "insecure_skip_verify"), CallTimeout: config.Duration{Duration: durationSetting(settings, "call_timeout", 10*time.Second)}}, secretString(secrets, "api_key"))
	case "qweather":
		latitude, latitudeErr := strconv.ParseFloat(stringSetting(settings, "latitude", ""), 64)
		longitude, longitudeErr := strconv.ParseFloat(stringSetting(settings, "longitude", ""), 64)
		if latitudeErr != nil || longitudeErr != nil || latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
			return integration.ProbeResult{Stage: integration.ProbeStageFeature, Message: "天气经纬度无效"}
		}
		client, err := weather.New(config.WeatherConfig{Enabled: true, APIHost: stringSetting(settings, "api_host", ""), APIKey: secretString(secrets, "api_key"), Name: stringSetting(settings, "name", "天气"), Latitude: latitude, Longitude: longitude, Units: stringSetting(settings, "units", "metric"), CallTimeout: config.Duration{Duration: durationSetting(settings, "call_timeout", 8*time.Second)}}, nil)
		if err != nil {
			return integration.ProbeFailure(err)
		}
		return client.Probe(ctx)
	case "plex":
		client, err := plex.New(config.PlexConfig{URL: stringSetting(settings, "url", ""), Token: secretString(secrets, "token"), CallTimeout: config.Duration{Duration: durationSetting(settings, "call_timeout", 5*time.Second)}}, nil)
		if err != nil {
			return integration.ProbeFailure(err)
		}
		return client.Probe(ctx)
	case "jellyfin":
		client, err := jellyfin.New(config.JellyfinConfig{URL: stringSetting(settings, "url", ""), Token: secretString(secrets, "token"), CallTimeout: config.Duration{Duration: durationSetting(settings, "call_timeout", 5*time.Second)}}, nil)
		if err != nil {
			return integration.ProbeFailure(err)
		}
		return client.Probe(ctx)
	case "qbittorrent":
		client, err := qbittorrent.New(config.QBittorrentConfig{URL: stringSetting(settings, "url", ""), Username: stringSetting(settings, "username", ""), Password: secretString(secrets, "password"), CallTimeout: config.Duration{Duration: durationSetting(settings, "call_timeout", 5*time.Second)}}, nil)
		if err != nil {
			return integration.ProbeFailure(err)
		}
		return client.Probe(ctx)
	case "uptime_kuma":
		client, err := uptimekuma.New(config.UptimeKumaConfig{URL: stringSetting(settings, "url", ""), APIKey: secretString(secrets, "api_key"), CallTimeout: config.Duration{Duration: durationSetting(settings, "call_timeout", 5*time.Second)}}, nil)
		if err != nil {
			return integration.ProbeFailure(err)
		}
		return client.Probe(ctx)
	case "home_assistant":
		client := homeassistant.New(config.HomeAssistantConfig{URL: stringSetting(settings, "url", ""), FanEntityID: stringSetting(settings, "entity_id", ""), FanName: stringSetting(settings, "name", "设备"), RemindAfter: config.Duration{Duration: durationSetting(settings, "remind_after", 2*time.Hour)}, CallTimeout: config.Duration{Duration: durationSetting(settings, "call_timeout", 5*time.Second)}}, secretString(secrets, "token"), nil)
		return client.Probe(ctx)
	case "scrutiny":
		client, err := scrutiny.New(config.ScrutinyConfig{URL: stringSetting(settings, "url", ""), CallTimeout: config.Duration{Duration: durationSetting(settings, "call_timeout", 5*time.Second)}}, nil)
		if err != nil {
			return integration.ProbeFailure(err)
		}
		return client.Probe(ctx)
	default:
		return integration.ProbeResult{Stage: integration.ProbeStageFeature, Message: "未知集成"}
	}
}

func stringSetting(settings integration.Config, key, fallback string) string {
	if value, ok := settings[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

func boolSetting(settings integration.Config, key string) bool {
	value, _ := settings[key].(bool)
	return value
}

func durationSetting(settings integration.Config, key string, fallback time.Duration) time.Duration {
	value, ok := settings[key].(string)
	if !ok {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func secretString(secrets integration.Secrets, key string) string { return string(secrets[key]) }
