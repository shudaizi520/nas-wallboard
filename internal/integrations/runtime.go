package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"example.com/nas-wallboard/internal/collector"
	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/dashboard"
	"example.com/nas-wallboard/internal/homeassistant"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/jellyfin"
	"example.com/nas-wallboard/internal/model"
	"example.com/nas-wallboard/internal/plex"
	"example.com/nas-wallboard/internal/qbittorrent"
	"example.com/nas-wallboard/internal/scrutiny"
	"example.com/nas-wallboard/internal/state"
	"example.com/nas-wallboard/internal/truenas"
	"example.com/nas-wallboard/internal/uptimekuma"
	"example.com/nas-wallboard/internal/weather"
)

type RuntimeOptions struct {
	Store  *state.Store
	Logger *slog.Logger
}

func DefaultDashboard(width int) (config.DashboardConfig, dashboard.Availability) {
	return config.DashboardConfig{
		Title: "NAS Wallboard", Language: "zh-CN", Theme: "midnight", Width: width,
		Metrics:    []config.MetricConfig{{Type: config.MetricTypeCPU}, {Type: config.MetricTypeCPUTemperature}, {Type: config.MetricTypeNetwork}},
		Activities: []config.ActivityConfig{{Type: config.ActivityTypeWeather}, {Type: config.ActivityTypePlex}, {Type: config.ActivityTypeTrueNASAlerts, Limit: 2}},
	}, dashboard.Availability{Weather: true, HomeAssistant: true, QBittorrent: true, Plex: true, Jellyfin: true, UptimeKuma: true}
}

func DefaultStaleAfter() state.StaleAfter {
	return state.StaleAfter{Realtime: 15 * time.Second, Apps: time.Minute, Alerts: time.Minute, System: 2 * time.Minute, Pools: 2 * time.Minute, Disks: 2 * time.Minute, Weather: 30 * time.Minute, Home: time.Minute, Downloads: time.Minute, Plex: time.Minute, Jellyfin: time.Minute, Monitors: 2 * time.Minute, DiskHealth: 2 * time.Minute, Memory: 2 * time.Minute, Replication: 2 * time.Minute}
}

type trueNASRuntime struct {
	client *truenas.Client
	runner *collector.Runner
}

func (runtime *trueNASRuntime) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	clientDone := make(chan error, 1)
	runnerDone := make(chan error, 1)
	go func() { clientDone <- runtime.client.Run(runCtx) }()
	go func() { runnerDone <- runtime.runner.Run(runCtx) }()
	select {
	case <-ctx.Done():
		cancel()
		<-clientDone
		<-runnerDone
		return nil
	case err := <-clientDone:
		cancel()
		<-runnerDone
		return err
	case err := <-runnerDone:
		cancel()
		<-clientDone
		return err
	}
}

func buildCollector(options RuntimeOptions, id string, metadata integration.Metadata, settings integration.Config, secrets integration.Secrets) (integration.Collector, error) {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := durationSetting(settings, "call_timeout", 5*time.Second)
	httpClient := &http.Client{Timeout: timeout}
	job := func(name string, every time.Duration, run func(context.Context) error) integration.Collector {
		return collector.NewRunner([]collector.Job{{Name: name, Every: every, MinimumEvery: metadata.MinimumRefresh, Timeout: timeout, Run: run}})
	}
	switch id {
	case "truenas":
		cfg := config.TrueNASConfig{URL: stringSetting(settings, "url", ""), Username: stringSetting(settings, "username", ""), InsecureSkipVerify: boolSetting(settings, "insecure_skip_verify"), CallTimeout: config.Duration{Duration: timeout}}
		client := truenas.NewClient(cfg, secretString(secrets, "api_key"), logger)
		readers := truenas.NewCollectors(client, nil)
		jobs := []collector.Job{
			{Name: "realtime", Every: 5 * time.Second, MinimumEvery: metadata.MinimumRefresh, Timeout: timeout, Run: func(ctx context.Context) error {
				value, err := readers.CollectRealtime(ctx)
				options.Store.SetRealtime(value, err)
				options.Store.Connected(err == nil)
				return err
			}},
			{Name: "system", Every: time.Minute, MinimumEvery: metadata.MinimumRefresh, Timeout: timeout, Run: func(ctx context.Context) error {
				value, err := readers.CollectSystem(ctx)
				options.Store.SetSystem(value, err)
				return err
			}},
			{Name: "memory", Every: time.Minute, MinimumEvery: metadata.MinimumRefresh, Timeout: timeout, Run: func(ctx context.Context) error {
				value, err := readers.CollectMemory(ctx)
				options.Store.SetMemory(value, err)
				return err
			}},
			{Name: "pools", Every: time.Minute, MinimumEvery: metadata.MinimumRefresh, Timeout: timeout, Run: func(ctx context.Context) error {
				value, err := readers.CollectPools(ctx)
				options.Store.SetPools(value, err)
				return err
			}},
			{Name: "disks", Every: time.Minute, MinimumEvery: metadata.MinimumRefresh, Timeout: timeout, Run: func(ctx context.Context) error {
				value, err := readers.CollectDisks(ctx)
				options.Store.SetDisks(value, err)
				return err
			}},
			{Name: "smart", Every: time.Minute, MinimumEvery: metadata.MinimumRefresh, Timeout: timeout, Run: func(ctx context.Context) error {
				value, err := readers.CollectDiskHealth(ctx)
				options.Store.SetDiskHealth(value, err)
				return err
			}},
			{Name: "replication", Every: time.Minute, MinimumEvery: metadata.MinimumRefresh, Timeout: timeout, Run: func(ctx context.Context) error {
				value, err := readers.CollectReplication(ctx)
				options.Store.SetReplication(value, err)
				return err
			}},
			{Name: "apps", Every: 30 * time.Second, MinimumEvery: metadata.MinimumRefresh, Timeout: timeout, Run: func(ctx context.Context) error {
				value, err := readers.CollectApps(ctx)
				options.Store.SetApps(value, err)
				return err
			}},
			{Name: "alerts", Every: 30 * time.Second, MinimumEvery: metadata.MinimumRefresh, Timeout: timeout, Run: func(ctx context.Context) error {
				value, err := readers.CollectAlerts(ctx)
				options.Store.SetAlerts(value, err)
				return err
			}},
		}
		return &trueNASRuntime{client: client, runner: collector.NewRunner(jobs)}, nil
	case "qweather":
		latitude, longitude, result := weatherCoordinates(settings)
		if !result.OK {
			return nil, errors.New("invalid weather coordinates")
		}
		apiHost := stringSetting(settings, "api_host", secretString(secrets, "api_host"))
		client, err := weather.New(config.WeatherConfig{Enabled: true, APIHost: apiHost, APIKey: secretString(secrets, "api_key"), Name: stringSetting(settings, "name", "天气"), Latitude: latitude, Longitude: longitude, Units: stringSetting(settings, "units", "metric"), CallTimeout: config.Duration{Duration: timeout}}, httpClient)
		if err != nil {
			return nil, err
		}
		return job(id, metadata.MinimumRefresh, func(ctx context.Context) error {
			value, err := client.Current(ctx)
			options.Store.SetWeather(value, err)
			return err
		}), nil
	case "plex":
		client, err := plex.New(config.PlexConfig{URL: stringSetting(settings, "url", ""), Token: secretString(secrets, "token"), CallTimeout: config.Duration{Duration: timeout}}, httpClient)
		if err != nil {
			return nil, err
		}
		return job(id, metadata.MinimumRefresh, adaptiveMedia(client.Current, options.Store.SetPlex, metadata.MinimumRefresh)), nil
	case "jellyfin":
		client, err := jellyfin.New(config.JellyfinConfig{URL: stringSetting(settings, "url", ""), Token: secretString(secrets, "token"), CallTimeout: config.Duration{Duration: timeout}}, httpClient)
		if err != nil {
			return nil, err
		}
		return job(id, metadata.MinimumRefresh, adaptiveMedia(client.Current, options.Store.SetJellyfin, metadata.MinimumRefresh)), nil
	case "qbittorrent":
		client, err := qbittorrent.New(config.QBittorrentConfig{URL: stringSetting(settings, "url", ""), Username: stringSetting(settings, "username", ""), Password: secretString(secrets, "password"), CallTimeout: config.Duration{Duration: timeout}}, httpClient)
		if err != nil {
			return nil, err
		}
		return job(id, metadata.MinimumRefresh, func(ctx context.Context) error {
			value, err := client.Current(ctx)
			options.Store.SetDownloads(value, err)
			return err
		}), nil
	case "uptime_kuma":
		client, err := uptimekuma.New(config.UptimeKumaConfig{URL: stringSetting(settings, "url", ""), APIKey: secretString(secrets, "api_key"), CallTimeout: config.Duration{Duration: timeout}}, httpClient)
		if err != nil {
			return nil, err
		}
		return job(id, metadata.MinimumRefresh, func(ctx context.Context) error {
			value, err := client.Current(ctx)
			options.Store.SetMonitors(value, err)
			return err
		}), nil
	case "home_assistant":
		cfg := config.HomeAssistantConfig{Enabled: true, URL: stringSetting(settings, "url", ""), FanEntityID: stringSetting(settings, "entity_id", ""), FanName: stringSetting(settings, "name", "设备"), RemindAfter: config.Duration{Duration: durationSetting(settings, "remind_after", 2*time.Hour)}, CallTimeout: config.Duration{Duration: timeout}}
		client := homeassistant.New(cfg, secretString(secrets, "token"), httpClient)
		options.Store.SetHome(model.FanStatus{Enabled: true, Name: cfg.FanName, State: "unavailable", RemindAfterSeconds: int64(cfg.RemindAfter.Duration / time.Second)}, nil)
		return job(id, metadata.MinimumRefresh, func(ctx context.Context) error {
			value, err := client.CurrentFan(ctx)
			options.Store.SetHome(value, err)
			return err
		}), nil
	case "scrutiny":
		client, err := scrutiny.New(config.ScrutinyConfig{Enabled: true, URL: stringSetting(settings, "url", ""), CallTimeout: config.Duration{Duration: timeout}}, httpClient)
		if err != nil {
			return nil, err
		}
		return job(id, metadata.MinimumRefresh, func(ctx context.Context) error {
			value, err := client.Summary(ctx)
			options.Store.SetDiskHealth(value, err)
			return err
		}), nil
	default:
		return nil, integration.ErrUnknownIntegration
	}
}

type mediaCurrent func(context.Context) (model.MediaStatus, error)

func adaptiveMedia(current mediaCurrent, set func(model.MediaStatus, error), every time.Duration) func(context.Context) error {
	idleTicks := max(1, int((time.Minute+every-1)/every))
	remaining := 0
	last := model.MediaStatus{}
	hasResult := false
	return func(ctx context.Context) error {
		if hasResult && len(last.Sessions) == 0 && remaining > 0 {
			remaining--
			set(last, nil)
			return nil
		}
		value, err := current(ctx)
		set(value, err)
		if err != nil {
			remaining = 0
			return err
		}
		last, hasResult = value, true
		if len(value.Sessions) == 0 {
			remaining = idleTicks - 1
		} else {
			remaining = 0
		}
		return nil
	}
}

func weatherCoordinates(settings integration.Config) (float64, float64, integration.ProbeResult) {
	latitude, latitudeErr := coordinateSetting(settings, "latitude")
	longitude, longitudeErr := coordinateSetting(settings, "longitude")
	if latitudeErr != nil || longitudeErr != nil || latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
		return 0, 0, integration.ProbeResult{Stage: integration.ProbeStageFeature, Message: "天气经纬度无效"}
	}
	return latitude, longitude, integration.ProbeResult{OK: true}
}

func coordinateSetting(settings integration.Config, key string) (float64, error) {
	switch value := settings[key].(type) {
	case string:
		return strconv.ParseFloat(value, 64)
	case json.Number:
		return value.Float64()
	case float64:
		return value, nil
	default:
		return 0, errors.New("coordinate must be text or a number")
	}
}
