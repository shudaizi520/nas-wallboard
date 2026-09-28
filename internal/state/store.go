package state

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"example.com/nas-wallboard/internal/model"
)

type StaleAfter struct {
	Realtime    time.Duration
	Apps        time.Duration
	Alerts      time.Duration
	System      time.Duration
	Pools       time.Duration
	Disks       time.Duration
	Weather     time.Duration
	Home        time.Duration
	Downloads   time.Duration
	Plex        time.Duration
	Jellyfin    time.Duration
	Monitors    time.Duration
	DiskHealth  time.Duration
	Memory      time.Duration
	Replication time.Duration
}

type Store struct {
	mu         sync.RWMutex
	version    string
	staleAfter StaleAfter
	now        func() time.Time
	connected  bool

	system      model.Module[model.SystemStatus]
	realtime    model.Module[model.RealtimeStatus]
	pools       model.Module[[]model.PoolStatus]
	disks       model.Module[[]model.DiskStatus]
	apps        model.Module[[]model.AppStatus]
	alerts      model.Module[[]model.AlertStatus]
	weather     model.Module[model.WeatherStatus]
	home        model.Module[model.FanStatus]
	downloads   model.Module[model.DownloadStatus]
	plex        model.Module[model.MediaStatus]
	jellyfin    model.Module[model.MediaStatus]
	monitors    model.Module[model.MonitorStatus]
	diskHealth  model.Module[[]model.DiskHealthStatus]
	memory      model.Module[model.MemoryStatus]
	replication model.Module[model.ReplicationStatus]
}

func New(version string, staleAfter StaleAfter, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{version: version, staleAfter: staleAfter, now: now}
}

func (s *Store) Connected(connected bool) {
	s.mu.Lock()
	s.connected = connected
	s.mu.Unlock()
}

func (s *Store) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected && !s.system.UpdatedAt.IsZero() && !s.realtime.UpdatedAt.IsZero() && !s.pools.UpdatedAt.IsZero()
}

func (s *Store) SetSystem(value model.SystemStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	setModule(&s.system, value, err, s.now())
}

func (s *Store) SetRealtime(value model.RealtimeStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	setModule(&s.realtime, value, err, s.now())
}

func (s *Store) SetPools(value []model.PoolStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		value = append([]model.PoolStatus(nil), value...)
	}
	setModule(&s.pools, value, err, s.now())
}

func (s *Store) SetDisks(value []model.DiskStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		value = append([]model.DiskStatus(nil), value...)
	}
	setModule(&s.disks, value, err, s.now())
}

func (s *Store) SetApps(value []model.AppStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		value = append([]model.AppStatus(nil), value...)
	}
	setModule(&s.apps, value, err, s.now())
}

func (s *Store) SetAlerts(value []model.AlertStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		value = append([]model.AlertStatus(nil), value...)
	}
	setModule(&s.alerts, value, err, s.now())
}

func (s *Store) SetWeather(value model.WeatherStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		value = cloneWeatherStatus(value)
	}
	setModule(&s.weather, value, err, s.now())
}

func (s *Store) SetHome(value model.FanStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	setModule(&s.home, cloneFanStatus(value), err, s.now())
}

func (s *Store) SetDownloads(value model.DownloadStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		value = cloneDownloadStatus(value)
	}
	setModule(&s.downloads, value, err, s.now())
}

func (s *Store) SetPlex(value model.MediaStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		value = cloneMediaStatus(value)
	}
	setModule(&s.plex, value, err, s.now())
}

func (s *Store) SetJellyfin(value model.MediaStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		value = cloneMediaStatus(value)
	}
	setModule(&s.jellyfin, value, err, s.now())
}

func (s *Store) SetMonitors(value model.MonitorStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		value = cloneMonitorStatus(value)
	}
	setModule(&s.monitors, value, err, s.now())
}

func (s *Store) SetDiskHealth(value []model.DiskHealthStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		value = append([]model.DiskHealthStatus(nil), value...)
	}
	setModule(&s.diskHealth, value, err, s.now())
}

func (s *Store) SetMemory(value model.MemoryStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	setModule(&s.memory, value, err, s.now())
}

func (s *Store) SetReplication(value model.ReplicationStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	setModule(&s.replication, value, err, s.now())
}

func (s *Store) Snapshot() model.Snapshot {
	s.mu.RLock()
	now := s.now()
	snapshot := model.Snapshot{
		SchemaVersion: model.SchemaVersion,
		Version:       s.version,
		ServerTime:    now,
		SnapshotAt:    now,
		Connected:     s.connected,
		System:        s.system,
		Realtime:      s.realtime,
		Pools:         s.pools,
		Disks:         s.disks,
		Apps:          s.apps,
		Alerts:        s.alerts,
		Weather:       s.weather,
		Home:          s.home,
		Downloads:     s.downloads,
		Plex:          s.plex,
		Jellyfin:      s.jellyfin,
		Monitors:      s.monitors,
		DiskHealth:    s.diskHealth,
		Memory:        s.memory,
		Replication:   s.replication,
	}
	snapshot.Pools.Data = append([]model.PoolStatus(nil), s.pools.Data...)
	snapshot.Disks.Data = append([]model.DiskStatus(nil), s.disks.Data...)
	snapshot.Apps.Data = append([]model.AppStatus(nil), s.apps.Data...)
	snapshot.Alerts.Data = append([]model.AlertStatus(nil), s.alerts.Data...)
	snapshot.Home.Data = cloneFanStatus(s.home.Data)
	snapshot.Downloads.Data = cloneDownloadStatus(s.downloads.Data)
	snapshot.Plex.Data = cloneMediaStatus(s.plex.Data)
	snapshot.Jellyfin.Data = cloneMediaStatus(s.jellyfin.Data)
	snapshot.Monitors.Data = cloneMonitorStatus(s.monitors.Data)
	snapshot.Weather.Data = cloneWeatherStatus(s.weather.Data)
	snapshot.DiskHealth.Data = append([]model.DiskHealthStatus(nil), s.diskHealth.Data...)
	intervals := s.staleAfter
	s.mu.RUnlock()

	snapshot.System.Stale = stale(now, snapshot.System.UpdatedAt, intervals.System)
	snapshot.Realtime.Stale = stale(now, snapshot.Realtime.UpdatedAt, intervals.Realtime)
	snapshot.Pools.Stale = stale(now, snapshot.Pools.UpdatedAt, intervals.Pools)
	snapshot.Disks.Stale = stale(now, snapshot.Disks.UpdatedAt, intervals.Disks)
	snapshot.Apps.Stale = stale(now, snapshot.Apps.UpdatedAt, intervals.Apps)
	snapshot.Alerts.Stale = stale(now, snapshot.Alerts.UpdatedAt, intervals.Alerts)
	snapshot.Weather.Stale = stale(now, snapshot.Weather.UpdatedAt, intervals.Weather)
	snapshot.Home.Stale = stale(now, snapshot.Home.UpdatedAt, intervals.Home)
	snapshot.Downloads.Stale = stale(now, snapshot.Downloads.UpdatedAt, intervals.Downloads)
	snapshot.Plex.Stale = stale(now, snapshot.Plex.UpdatedAt, intervals.Plex)
	snapshot.Jellyfin.Stale = stale(now, snapshot.Jellyfin.UpdatedAt, intervals.Jellyfin)
	snapshot.Monitors.Stale = stale(now, snapshot.Monitors.UpdatedAt, intervals.Monitors)
	snapshot.DiskHealth.Stale = stale(now, snapshot.DiskHealth.UpdatedAt, intervals.DiskHealth)
	snapshot.Memory.Stale = stale(now, snapshot.Memory.UpdatedAt, intervals.Memory)
	snapshot.Replication.Stale = stale(now, snapshot.Replication.UpdatedAt, intervals.Replication)
	return snapshot
}

func cloneWeatherStatus(value model.WeatherStatus) model.WeatherStatus {
	value.Warnings = append([]model.WeatherWarning(nil), value.Warnings...)
	return value
}

func cloneDownloadStatus(value model.DownloadStatus) model.DownloadStatus {
	value.Items = append([]model.DownloadItem(nil), value.Items...)
	return value
}

func cloneMediaStatus(value model.MediaStatus) model.MediaStatus {
	value.Sessions = append([]model.MediaSession(nil), value.Sessions...)
	return value
}

func cloneMonitorStatus(value model.MonitorStatus) model.MonitorStatus {
	value.DownNames = append([]string(nil), value.DownNames...)
	return value
}

func cloneFanStatus(value model.FanStatus) model.FanStatus {
	if value.OnSince != nil {
		copy := *value.OnSince
		value.OnSince = &copy
	}
	if value.Percentage != nil {
		copy := *value.Percentage
		value.Percentage = &copy
	}
	if value.Oscillating != nil {
		copy := *value.Oscillating
		value.Oscillating = &copy
	}
	return value
}

func setModule[T any](module *model.Module[T], value T, err error, now time.Time) {
	if err != nil {
		module.Error = publicErrorCode(err)
		return
	}
	module.Data = value
	module.UpdatedAt = now
	module.Error = ""
	module.Stale = false
}

func stale(now, updated time.Time, interval time.Duration) bool {
	if updated.IsZero() {
		return true
	}
	if interval <= 0 || now.Before(updated) {
		return false
	}
	return now.Sub(updated) > 3*interval
}

func publicErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "deadline"), strings.Contains(message, "timeout"):
		return "timeout"
	case strings.Contains(message, "unauthorized"), strings.Contains(message, "authentication"), strings.Contains(message, "permission"), strings.Contains(message, "forbidden"):
		return "unauthorized"
	case strings.Contains(message, "method not found"), strings.Contains(message, "unsupported"), strings.Contains(message, "not implemented"):
		return "unsupported"
	case strings.Contains(message, "not_found"), strings.Contains(message, "not found"):
		return "not_found"
	default:
		return "unavailable"
	}
}
