package model

import "time"

const SchemaVersion = 6

type Module[T any] struct {
	Data      T         `json:"data"`
	UpdatedAt time.Time `json:"updated_at"`
	Stale     bool      `json:"stale"`
	Error     string    `json:"error,omitempty"`
	Partial   bool      `json:"partial,omitempty"`
}

type Snapshot struct {
	SchemaVersion      int                        `json:"schema_version"`
	Version            string                     `json:"version"`
	ServerTime         time.Time                  `json:"server_time"`
	SnapshotAt         time.Time                  `json:"snapshot_at"`
	Connected          bool                       `json:"connected"`
	System             Module[SystemStatus]       `json:"system"`
	Realtime           Module[RealtimeStatus]     `json:"realtime"`
	Pools              Module[[]PoolStatus]       `json:"pools"`
	Disks              Module[[]DiskStatus]       `json:"disks"`
	Apps               Module[[]AppStatus]        `json:"apps"`
	Alerts             Module[[]AlertStatus]      `json:"alerts"`
	Weather            Module[WeatherStatus]      `json:"weather"`
	Home               Module[FanStatus]          `json:"home"`
	HomePower          Module[PowerStatus]        `json:"home_power"`
	Downloads          Module[DownloadStatus]     `json:"downloads"`
	Plex               Module[MediaStatus]        `json:"plex"`
	Jellyfin           Module[MediaStatus]        `json:"jellyfin"`
	Monitors           Module[MonitorStatus]      `json:"monitors"`
	DiskHealth         Module[[]DiskHealthStatus] `json:"disk_health"`
	TrueNASDiskHealth  Module[[]DiskHealthStatus] `json:"truenas_disk_health"`
	ScrutinyDiskHealth Module[[]DiskHealthStatus] `json:"scrutiny_disk_health"`
	Memory             Module[MemoryStatus]       `json:"memory"`
	Replication        Module[ReplicationStatus]  `json:"replication"`
}

type SystemStatus struct {
	Hostname         string `json:"hostname"`
	Version          string `json:"version"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
	MemoryTotalBytes uint64 `json:"memory_total_bytes"`
}

type RealtimeStatus struct {
	CPUPercent            float64 `json:"cpu_percent"`
	CPUTemperatureCelsius float64 `json:"cpu_temperature_celsius"`
	MemoryUsedBytes       uint64  `json:"memory_used_bytes"`
	MemoryTotalBytes      uint64  `json:"memory_total_bytes"`
	NetworkRxBps          float64 `json:"network_rx_bps"`
	NetworkTxBps          float64 `json:"network_tx_bps"`
}

type PoolStatus struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	Healthy      bool    `json:"healthy"`
	UsedBytes    uint64  `json:"used_bytes"`
	TotalBytes   uint64  `json:"total_bytes"`
	UsedPercent  float64 `json:"used_percent"`
	ScanState    string  `json:"scan_state,omitempty"`
	ScanProgress float64 `json:"scan_progress,omitempty"`
}

type DiskStatus struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Temperature float64 `json:"temperature_celsius"`
	Model       string  `json:"-"`
	Serial      string  `json:"-"`
	SizeBytes   uint64  `json:"-"`
}

type AppStatus struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	State           string `json:"state"`
	UpdateAvailable bool   `json:"update_available"`
	Link            string `json:"link,omitempty"`
}

type AlertStatus struct {
	ID         string    `json:"id"`
	Level      string    `json:"level"`
	Title      string    `json:"title"`
	Message    string    `json:"message"`
	OccurredAt time.Time `json:"occurred_at"`
}

type WeatherStatus struct {
	Enabled                 bool              `json:"enabled"`
	Name                    string            `json:"name"`
	Temperature             float64           `json:"temperature"`
	FeelsLike               *float64          `json:"feels_like,omitempty"`
	HumidityPercent         *float64          `json:"humidity_percent,omitempty"`
	WindScale               *int              `json:"wind_scale,omitempty"`
	WindGustMetersPerSecond *float64          `json:"wind_gust_meters_per_second,omitempty"`
	UVIndex                 *float64          `json:"uv_index,omitempty"`
	Condition               string            `json:"condition"`
	ConditionCode           string            `json:"condition_code"`
	RainSummary             string            `json:"rain_summary,omitempty"`
	Warnings                []WeatherWarning  `json:"warnings"`
	Forecasts               []WeatherForecast `json:"forecasts,omitempty"`
	Source                  string            `json:"source"`
	Units                   string            `json:"units"`
}

type WeatherForecast struct {
	Condition                string  `json:"condition"`
	ConditionCode            string  `json:"condition_code"`
	TemperatureMin           float64 `json:"temperature_min"`
	TemperatureMax           float64 `json:"temperature_max"`
	PrecipitationProbability float64 `json:"precipitation_probability"`
}

type WeatherWarning struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Severity string `json:"severity"`
	Color    string `json:"color"`
}

type DiskHealthStatus struct {
	Name      string `json:"name"`
	Model     string `json:"model"`
	SizeBytes uint64 `json:"size_bytes"`
	State     string `json:"state"`
}

type MemoryStatus struct {
	TotalBytes       uint64  `json:"total_bytes"`
	AvailableBytes   uint64  `json:"available_bytes"`
	AvailablePercent float64 `json:"available_percent"`
}

type ReplicationStatus struct {
	Total    int `json:"total"`
	Enabled  int `json:"enabled"`
	Disabled int `json:"disabled"`
	NeverRun int `json:"never_run"`
	Failed   int `json:"failed"`
	Running  int `json:"running"`
}

type FanStatus struct {
	Enabled            bool       `json:"enabled"`
	Available          bool       `json:"available"`
	Name               string     `json:"name,omitempty"`
	State              string     `json:"state"`
	OnSince            *time.Time `json:"on_since,omitempty"`
	Percentage         *float64   `json:"percentage,omitempty"`
	PresetMode         string     `json:"preset_mode,omitempty"`
	Oscillating        *bool      `json:"oscillating,omitempty"`
	RemindAfterSeconds int64      `json:"remind_after_seconds"`
}

type PowerStatus struct {
	Available bool    `json:"available"`
	Watts     float64 `json:"watts"`
}

type DownloadStatus struct {
	ActiveCount int            `json:"active_count"`
	DownloadBps int64          `json:"download_bps"`
	Items       []DownloadItem `json:"items"`
}

type DownloadItem struct {
	Name            string  `json:"name"`
	ProgressPercent float64 `json:"progress_percent"`
	DownloadBps     int64   `json:"download_bps"`
}

type MediaStatus struct {
	Sessions []MediaSession `json:"sessions"`
}

type MediaSession struct {
	Title          string `json:"title"`
	Device         string `json:"device,omitempty"`
	User           string `json:"user,omitempty"`
	Paused         bool   `json:"paused"`
	SessionID      string `json:"-"`
	ProgressMillis int64  `json:"-"`
}

type MonitorStatus struct {
	Total     int      `json:"total"`
	DownNames []string `json:"down_names"`
}
