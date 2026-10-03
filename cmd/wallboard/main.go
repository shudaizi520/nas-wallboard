package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata"

	"example.com/nas-wallboard/internal/auth"
	"example.com/nas-wallboard/internal/collector"
	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/dashboard"
	"example.com/nas-wallboard/internal/homeassistant"
	"example.com/nas-wallboard/internal/httpapi"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/integrations"
	"example.com/nas-wallboard/internal/jellyfin"
	"example.com/nas-wallboard/internal/model"
	"example.com/nas-wallboard/internal/persist"
	"example.com/nas-wallboard/internal/plex"
	"example.com/nas-wallboard/internal/qbittorrent"
	"example.com/nas-wallboard/internal/scrutiny"
	"example.com/nas-wallboard/internal/state"
	"example.com/nas-wallboard/internal/truenas"
	"example.com/nas-wallboard/internal/update"
	"example.com/nas-wallboard/internal/uptimekuma"
	"example.com/nas-wallboard/internal/weather"
	"example.com/nas-wallboard/internal/widget"
	webassets "example.com/nas-wallboard/web"
)

var (
	version    = "dev"
	repository = ""
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("wallboard stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: wallboard serve | healthcheck")
	}
	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "init-data":
		flags := flag.NewFlagSet("init-data", flag.ContinueOnError)
		path := flags.String("path", "/data", "persistent data directory")
		uid := flags.Int("uid", 65532, "runtime user ID")
		gid := flags.Int("gid", 65532, "runtime group ID")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return initializeDataDirectory(*path, *uid, *gid)
	case "healthcheck":
		flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
		endpoint := flags.String("url", "http://127.0.0.1:8080/healthz", "health endpoint")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return healthcheck(ctx, &http.Client{Timeout: 5 * time.Second}, *endpoint)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := flags.String("config", "/config/config.yaml", "configuration file")
	secretPath := flags.String("secret", "/run/secrets/truenas_api_key", "TrueNAS API key file")
	homeAssistantSecretPath := flags.String("home-assistant-secret", "/run/secrets/home_assistant_token", "Home Assistant token file")
	settingsPath := flags.String("settings", "/data/dashboard.json", "editable dashboard settings file")
	if err := flags.Parse(args); err != nil {
		return err
	}

	startup, err := selectStartup(context.Background(), startupPaths{
		DataRoot: filepath.Dir(*settingsPath), Config: *configPath, TrueNASSecret: *secretPath,
		Dashboard: *settingsPath, HomeAssistantSecret: *homeAssistantSecretPath,
	})
	if err != nil {
		return fmt.Errorf("prepare application state: %w", err)
	}
	var authManager *auth.Manager
	if startup.State != nil && startup.Secrets != nil {
		dataRoot := filepath.Dir(*settingsPath)
		if err := httpapi.RecoverPendingSetup(dataRoot, startup.State, startup.Secrets); err != nil {
			return fmt.Errorf("recover setup transaction: %w", err)
		}
		authManager, err = auth.Open(filepath.Join(dataRoot, "auth.json"), time.Now)
		if err != nil {
			return fmt.Errorf("open administrator authentication: %w", err)
		}
		defer authManager.Close()
	}
	if startup.Mode == startupPublic {
		return servePublic(filepath.Dir(*settingsPath), startup, authManager)
	}
	cfg, err := config.LoadLegacy(*configPath, *secretPath)
	if err != nil {
		if startup.Mode == startupMigration || startup.Mode == startupPublic {
			return servePublic(filepath.Dir(*settingsPath), startup, authManager)
		}
		return err
	}
	dashboardBuilder, err := dashboard.New(cfg.Dashboard)
	if err != nil {
		return fmt.Errorf("configure dashboard: %w", err)
	}
	settingsManager, err := dashboard.NewSettingsManager(*settingsPath, cfg.Dashboard, dashboard.Availability{
		Weather: cfg.Weather.Enabled, HomeAssistant: cfg.HomeAssistant.Enabled, QBittorrent: cfg.QBittorrent.Enabled,
		Plex: cfg.Plex.Enabled, Jellyfin: cfg.Jellyfin.Enabled, UptimeKuma: cfg.UptimeKuma.Enabled,
	}, dashboardBuilder)
	if err != nil {
		return fmt.Errorf("configure dashboard settings: %w", err)
	}
	homeAssistantToken, err := loadHomeAssistantToken(cfg, *homeAssistantSecretPath)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("startup mode selected", "mode", startup.Mode)
	if startup.ImportError != "" {
		logger.Warn(startup.ImportError)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := state.New(version, state.StaleAfter{
		Realtime:   cfg.Refresh.Realtime.Duration,
		Apps:       cfg.Refresh.Apps.Duration,
		Alerts:     cfg.Refresh.Alerts.Duration,
		System:     cfg.Refresh.System.Duration,
		Pools:      cfg.Refresh.Pools.Duration,
		Disks:      cfg.Refresh.Disks.Duration,
		DiskHealth: cfg.Refresh.Scrutiny.Duration,
		Weather:    cfg.Refresh.Weather.Duration,
		Home:       cfg.Refresh.HomeAssistant.Duration,
		Downloads:  cfg.Refresh.QBittorrent.Duration,
		Plex:       cfg.Refresh.Plex.Duration,
		Jellyfin:   cfg.Refresh.Jellyfin.Duration,
		Monitors:   cfg.Refresh.UptimeKuma.Duration,
	}, time.Now)
	trueNASClient := truenas.NewClient(cfg.TrueNAS, cfg.APIKey, logger)
	collectors := truenas.NewCollectors(trueNASClient, cfg.Apps)
	var weatherClient *weather.Client
	if cfg.Weather.Enabled {
		weatherClient, err = weather.New(cfg.Weather, &http.Client{Timeout: cfg.Weather.CallTimeout.Duration})
		if err != nil {
			return fmt.Errorf("configure weather: %w", err)
		}
	}

	go func() {
		if err := trueNASClient.Run(ctx); err != nil {
			logger.Error("TrueNAS client stopped", "error", err)
		}
	}()

	jobs := []collector.Job{
		{Name: "realtime", Every: cfg.Refresh.Realtime.Duration, Timeout: cfg.TrueNAS.CallTimeout.Duration, Run: func(ctx context.Context) error {
			value, err := collectors.CollectRealtime(ctx)
			store.SetRealtime(value, err)
			store.Connected(err == nil)
			return err
		}},
		{Name: "system", Every: cfg.Refresh.System.Duration, Timeout: cfg.TrueNAS.CallTimeout.Duration, Run: func(ctx context.Context) error {
			value, err := collectors.CollectSystem(ctx)
			store.SetSystem(value, err)
			return err
		}},
		{Name: "pools", Every: cfg.Refresh.Pools.Duration, Timeout: cfg.TrueNAS.CallTimeout.Duration, Run: func(ctx context.Context) error {
			value, err := collectors.CollectPools(ctx)
			store.SetPools(value, err)
			return err
		}},
		{Name: "disks", Every: cfg.Refresh.Disks.Duration, Timeout: cfg.TrueNAS.CallTimeout.Duration, Run: func(ctx context.Context) error {
			value, err := collectors.CollectDisks(ctx)
			store.SetDisks(value, err)
			return err
		}},
		{Name: "apps", Every: cfg.Refresh.Apps.Duration, Timeout: cfg.TrueNAS.CallTimeout.Duration, Run: func(ctx context.Context) error {
			value, err := collectors.CollectApps(ctx)
			store.SetApps(value, err)
			return err
		}},
		{Name: "alerts", Every: cfg.Refresh.Alerts.Duration, Timeout: cfg.TrueNAS.CallTimeout.Duration, Run: func(ctx context.Context) error {
			value, err := collectors.CollectAlerts(ctx)
			store.SetAlerts(value, err)
			return err
		}},
	}
	if cfg.Weather.Enabled {
		jobs = append(jobs, collector.Job{Name: "weather", Every: cfg.Refresh.Weather.Duration, Timeout: cfg.Weather.CallTimeout.Duration, Run: func(ctx context.Context) error {
			value, err := weatherClient.Current(ctx)
			store.SetWeather(value, err)
			return err
		}})
	}
	if cfg.Scrutiny.Enabled {
		scrutinyClient, err := scrutiny.New(cfg.Scrutiny, &http.Client{Timeout: cfg.Scrutiny.CallTimeout.Duration})
		if err != nil {
			return fmt.Errorf("configure Scrutiny: %w", err)
		}
		jobs = appendScrutinyJob(jobs, cfg, store, scrutinyClient)
	}
	if cfg.HomeAssistant.Enabled {
		homeAssistantClient := homeassistant.New(cfg.HomeAssistant, homeAssistantToken, &http.Client{Timeout: cfg.HomeAssistant.CallTimeout.Duration})
		jobs = appendHomeAssistantJob(jobs, cfg, store, homeAssistantClient)
	}
	if cfg.QBittorrent.Enabled {
		qbittorrentClient, err := qbittorrent.New(cfg.QBittorrent, &http.Client{Timeout: cfg.QBittorrent.CallTimeout.Duration})
		if err != nil {
			return fmt.Errorf("configure qBittorrent: %w", err)
		}
		jobs = appendQBittorrentJob(jobs, cfg, store, qbittorrentClient)
	}
	if cfg.Plex.Enabled {
		plexClient, err := plex.New(cfg.Plex, &http.Client{Timeout: cfg.Plex.CallTimeout.Duration})
		if err != nil {
			return fmt.Errorf("configure Plex: %w", err)
		}
		jobs = appendPlexJob(jobs, cfg, store, plexClient)
	}
	if cfg.Jellyfin.Enabled {
		jellyfinClient, err := jellyfin.New(cfg.Jellyfin, &http.Client{Timeout: cfg.Jellyfin.CallTimeout.Duration})
		if err != nil {
			return fmt.Errorf("configure Jellyfin: %w", err)
		}
		jobs = appendJellyfinJob(jobs, cfg, store, jellyfinClient)
	}
	if cfg.UptimeKuma.Enabled {
		uptimeClient, err := uptimekuma.New(cfg.UptimeKuma, &http.Client{Timeout: cfg.UptimeKuma.CallTimeout.Duration})
		if err != nil {
			return fmt.Errorf("configure Uptime Kuma: %w", err)
		}
		jobs = appendUptimeKumaJob(jobs, cfg, store, uptimeClient)
	}
	runner := collector.NewRunner(jobs)
	go runner.Run(ctx)

	server := &http.Server{
		Addr: cfg.Server.Listen,
		Handler: httpapi.New(httpapi.Dependencies{
			RuntimeStore: store, Assets: webassets.FS, Version: version, Builder: dashboardBuilder,
			DashboardManager: settingsManager, State: startup.State, Secrets: startup.Secrets,
			Auth: authManager, DataRoot: filepath.Dir(*settingsPath),
		}),
		ReadHeaderTimeout: cfg.Server.ReadTimeout.Duration,
		ReadTimeout:       cfg.Server.ReadTimeout.Duration,
		WriteTimeout:      cfg.Server.WriteTimeout.Duration,
		IdleTimeout:       60 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("wallboard listening", "address", cfg.Server.Listen, "version", version)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP: %w", err)
	}
	return nil
}

func servePublic(dataRoot string, startup startupSelection, authManager *auth.Manager) error {
	settings := startup.State.Snapshot().Server
	width := settings.Width
	if width < config.DashboardMinWidth || width > config.DashboardMaxWidth {
		width = config.DashboardDefaultWidth
	}
	dashboardConfig, availability := integrations.DefaultDashboard(width)
	builder, err := dashboard.New(dashboardConfig)
	if err != nil {
		return fmt.Errorf("configure setup dashboard: %w", err)
	}
	manager, err := dashboard.NewSettingsManager("", dashboardConfig, availability, builder)
	if err != nil {
		return fmt.Errorf("configure setup dashboard settings: %w", err)
	}
	widgetRegistry, err := widget.BuiltInRegistry()
	if err != nil {
		return fmt.Errorf("build widget registry: %w", err)
	}
	widgetService := widget.NewService(widgetRegistry, startup.State)
	publicState := startup.State.Snapshot()
	if len(publicState.Widgets) == 0 && len(publicState.Integrations) > 0 {
		sources := map[string]string{}
		for _, source := range publicState.Integrations {
			if source.Enabled {
				sources[source.Type] = source.ID
			}
		}
		metrics := make([]widget.LegacyItem, 0, len(dashboardConfig.Metrics))
		for _, metric := range dashboardConfig.Metrics {
			item := widget.LegacyItem{Type: metric.Type}
			if metric.Type == config.MetricTypeNetwork && metric.Interface != "" {
				item.Config = map[string]any{"interface": metric.Interface}
			}
			metrics = append(metrics, item)
		}
		integrationTypes := map[string]string{
			config.ActivityTypeWeather: "qweather", config.ActivityTypePlex: "plex", config.ActivityTypeJellyfin: "jellyfin",
			config.ActivityTypeQBittorrent: "qbittorrent", config.ActivityTypeUptimeKuma: "uptime_kuma", config.ActivityTypeHomeAssistantFan: "home_assistant",
			config.ActivityTypeTrueNASAlerts: "truenas", config.ActivityTypeAppHealth: "truenas", config.ActivityTypeAppUpdates: "truenas",
		}
		activities := []widget.LegacyItem{}
		for _, activity := range dashboardConfig.Activities {
			if sources[integrationTypes[activity.Type]] != "" {
				activities = append(activities, widget.LegacyItem{Type: activity.Type, Limit: activity.Limit})
			}
		}
		if _, err := widgetService.MigrateLegacy(width, metrics, activities, sources); err != nil {
			return fmt.Errorf("initialize widget layout: %w", err)
		}
	}
	if err := widgetService.MigrateDefaults(); err != nil {
		return fmt.Errorf("migrate widget defaults: %w", err)
	}
	if len(startup.State.Snapshot().Widgets) > 0 {
		configured, configureErr := widgetService.DashboardConfig(dashboardConfig)
		if configureErr != nil {
			return fmt.Errorf("apply widget layout: %w", configureErr)
		}
		if err := builder.Update(configured); err != nil {
			return fmt.Errorf("apply widget dashboard: %w", err)
		}
	}
	runtimeStore := state.New(version, integrations.DefaultStaleAfter(), time.Now)
	listen := settings.Listen
	if listen == "" {
		listen = ":8080"
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	registry, err := integrations.BuiltInRegistry(integrations.RuntimeOptions{Store: runtimeStore, Logger: logger})
	if err != nil {
		return fmt.Errorf("build integration registry: %w", err)
	}
	runtimeManager := integration.NewManager(registry, startup.State, startup.Secrets)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runtimeManager.Start(ctx); err != nil {
		return fmt.Errorf("start integrations: %w", err)
	}
	defer runtimeManager.Close()
	integrationService := integration.NewService(registry, startup.State, startup.Secrets, integration.ServiceOptions{
		ProbeTimeout: 10 * time.Second,
		Initialize:   widgetService.AddIntegrationDefaults,
		OnChange: func(changeCtx context.Context, before, after []persist.Integration) error {
			if err := runtimeManager.Apply(changeCtx, before, after); err != nil {
				return err
			}
			configured, err := widgetService.DashboardConfig(dashboardConfig)
			if err != nil {
				return err
			}
			return builder.Update(configured)
		},
	})
	var updateChecker *update.Checker
	if repository != "" {
		updateChecker, err = update.New(repository, version, &http.Client{Timeout: 8 * time.Second}, time.Now)
		if err != nil {
			return fmt.Errorf("configure update checker: %w", err)
		}
	}
	server := &http.Server{
		Addr: listen,
		Handler: httpapi.New(httpapi.Dependencies{
			RuntimeStore: runtimeStore, Assets: webassets.FS, Version: version, Builder: builder,
			DashboardManager: manager, State: startup.State, Secrets: startup.Secrets,
			Auth: authManager, DataRoot: dataRoot, Integrations: integrationService, IntegrationRuntime: runtimeManager,
			Widgets: widgetService, Updates: updateChecker,
		}),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("wallboard setup listening", "address", listen, "version", version)
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve setup HTTP: %w", err)
		}
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

type fanReader interface {
	CurrentFan(context.Context) (model.FanStatus, error)
}

type downloadReader interface {
	Current(context.Context) (model.DownloadStatus, error)
}

type mediaReader interface {
	Current(context.Context) (model.MediaStatus, error)
}

type monitorReader interface {
	Current(context.Context) (model.MonitorStatus, error)
}

type diskHealthReader interface {
	Summary(context.Context) ([]model.DiskHealthStatus, error)
}

func loadHomeAssistantToken(cfg config.Config, path string) (string, error) {
	if !cfg.HomeAssistant.Enabled {
		return "", nil
	}
	return config.LoadHomeAssistantToken(path)
}

func appendHomeAssistantJob(jobs []collector.Job, cfg config.Config, store *state.Store, reader fanReader) []collector.Job {
	if !cfg.HomeAssistant.Enabled {
		return jobs
	}
	store.SetHome(model.FanStatus{
		Enabled:            true,
		Available:          false,
		Name:               cfg.HomeAssistant.FanName,
		State:              "unavailable",
		RemindAfterSeconds: int64(cfg.HomeAssistant.RemindAfter.Duration / time.Second),
	}, nil)
	return append(jobs, collector.Job{
		Name:    "home_assistant",
		Every:   cfg.Refresh.HomeAssistant.Duration,
		Timeout: cfg.HomeAssistant.CallTimeout.Duration,
		Run: func(ctx context.Context) error {
			value, err := reader.CurrentFan(ctx)
			store.SetHome(value, err)
			return err
		},
	})
}

func appendQBittorrentJob(jobs []collector.Job, cfg config.Config, store *state.Store, reader downloadReader) []collector.Job {
	if !cfg.QBittorrent.Enabled {
		return jobs
	}
	every := cfg.Refresh.QBittorrent.Duration
	idleTicks := int((time.Minute + every - 1) / every)
	remainingIdleTicks := 0
	hasResult := false
	last := model.DownloadStatus{}
	return append(jobs, collector.Job{
		Name:    "qbittorrent",
		Every:   every,
		Timeout: cfg.QBittorrent.CallTimeout.Duration,
		Run: func(ctx context.Context) error {
			if hasResult && last.ActiveCount == 0 && remainingIdleTicks > 0 {
				remainingIdleTicks--
				store.SetDownloads(last, nil)
				return nil
			}
			value, err := reader.Current(ctx)
			store.SetDownloads(value, err)
			if err != nil {
				remainingIdleTicks = 0
				return err
			}
			last = value
			hasResult = true
			if value.ActiveCount == 0 {
				remainingIdleTicks = idleTicks - 1
			} else {
				remainingIdleTicks = 0
			}
			return err
		},
	})
}

func appendPlexJob(jobs []collector.Job, cfg config.Config, store *state.Store, reader mediaReader) []collector.Job {
	if !cfg.Plex.Enabled {
		return jobs
	}
	every := cfg.Refresh.Plex.Duration
	idleTicks := int((time.Minute + every - 1) / every)
	remainingIdleTicks := 0
	active := false
	hasResult := false
	last := model.MediaStatus{}
	return append(jobs, collector.Job{
		Name: "plex", Every: every, Timeout: cfg.Plex.CallTimeout.Duration,
		Run: func(ctx context.Context) error {
			if hasResult && !active && remainingIdleTicks > 0 {
				remainingIdleTicks--
				store.SetPlex(last, nil)
				return nil
			}
			value, err := reader.Current(ctx)
			store.SetPlex(value, err)
			if err != nil {
				remainingIdleTicks = 0
				return err
			}
			last = value
			hasResult = true
			active = len(value.Sessions) > 0
			if active {
				remainingIdleTicks = 0
			} else {
				remainingIdleTicks = idleTicks - 1
			}
			return nil
		},
	})
}

func appendJellyfinJob(jobs []collector.Job, cfg config.Config, store *state.Store, reader mediaReader) []collector.Job {
	if !cfg.Jellyfin.Enabled {
		return jobs
	}
	return appendMediaJob(jobs, "jellyfin", cfg.Refresh.Jellyfin.Duration, cfg.Jellyfin.CallTimeout.Duration, reader, store.SetJellyfin)
}

func appendMediaJob(jobs []collector.Job, name string, every, timeout time.Duration, reader mediaReader, setter func(model.MediaStatus, error)) []collector.Job {
	return append(jobs, collector.Job{
		Name: name, Every: every, Timeout: timeout,
		Run: func(ctx context.Context) error {
			value, err := reader.Current(ctx)
			setter(value, err)
			return err
		},
	})
}

func appendUptimeKumaJob(jobs []collector.Job, cfg config.Config, store *state.Store, reader monitorReader) []collector.Job {
	if !cfg.UptimeKuma.Enabled {
		return jobs
	}
	return append(jobs, collector.Job{
		Name: "uptime_kuma", Every: cfg.Refresh.UptimeKuma.Duration, Timeout: cfg.UptimeKuma.CallTimeout.Duration,
		Run: func(ctx context.Context) error {
			value, err := reader.Current(ctx)
			store.SetMonitors(value, err)
			return err
		},
	})
}

func appendScrutinyJob(jobs []collector.Job, cfg config.Config, store *state.Store, reader diskHealthReader) []collector.Job {
	if !cfg.Scrutiny.Enabled {
		return jobs
	}
	return append(jobs, collector.Job{
		Name: "scrutiny", Every: cfg.Refresh.Scrutiny.Duration, Timeout: cfg.Scrutiny.CallTimeout.Duration,
		Run: func(ctx context.Context) error {
			value, err := reader.Summary(ctx)
			store.SetDiskHealth(value, err)
			return err
		},
	})
}
