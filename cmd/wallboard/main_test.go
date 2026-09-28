package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/collector"
	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/model"
	"example.com/nas-wallboard/internal/state"
)

type fakeFanReader struct {
	status model.FanStatus
	err    error
}

type fakeDownloadReader struct {
	status model.DownloadStatus
	err    error
}

type fakeMediaReader struct {
	status model.MediaStatus
	err    error
}

type scriptedMediaReader struct {
	statuses []model.MediaStatus
	calls    int
}

type fakeMonitorReader struct {
	status model.MonitorStatus
	err    error
}

type fakeDiskHealthReader struct {
	status []model.DiskHealthStatus
	err    error
}

func (f fakeDiskHealthReader) Summary(context.Context) ([]model.DiskHealthStatus, error) {
	return f.status, f.err
}

func (f fakeMonitorReader) Current(context.Context) (model.MonitorStatus, error) {
	return f.status, f.err
}

func (f fakeMediaReader) Current(context.Context) (model.MediaStatus, error) {
	return f.status, f.err
}

func (f *scriptedMediaReader) Current(context.Context) (model.MediaStatus, error) {
	status := f.statuses[f.calls]
	f.calls++
	return status, nil
}

func (f fakeDownloadReader) Current(context.Context) (model.DownloadStatus, error) {
	return f.status, f.err
}

func (f fakeFanReader) CurrentFan(context.Context) (model.FanStatus, error) {
	return f.status, f.err
}

func TestLoadOptionalHomeAssistantToken(t *testing.T) {
	cfg := config.Config{}
	missing := filepath.Join(t.TempDir(), "missing-token")
	token, err := loadHomeAssistantToken(cfg, missing)
	if err != nil || token != "" {
		t.Fatalf("disabled token/error = %q / %v", token, err)
	}

	cfg.HomeAssistant.Enabled = true
	if _, err := loadHomeAssistantToken(cfg, missing); err == nil {
		t.Fatal("enabled Home Assistant accepted a missing token")
	}
}

func TestAppendHomeAssistantJobIsOptionalAndIndependent(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := state.New("test", state.StaleAfter{Home: 30 * time.Second}, func() time.Time { return now })
	base := []collector.Job{{Name: "existing"}}
	if got := appendHomeAssistantJob(base, config.Config{}, store, nil); len(got) != 1 {
		t.Fatalf("disabled jobs = %#v", got)
	}

	cfg := config.Config{
		HomeAssistant: config.HomeAssistantConfig{Enabled: true, CallTimeout: config.Duration{Duration: 5 * time.Second}},
		Refresh:       config.RefreshConfig{HomeAssistant: config.Duration{Duration: 30 * time.Second}},
	}
	reader := fakeFanReader{status: model.FanStatus{Enabled: true, Available: true, Name: "客厅风扇", State: "on"}}
	jobs := appendHomeAssistantJob(base, cfg, store, reader)
	if len(jobs) != 2 || jobs[1].Name != "home_assistant" || jobs[1].Every != 30*time.Second || jobs[1].Timeout != 5*time.Second {
		t.Fatalf("enabled jobs = %#v", jobs)
	}
	if initial := store.Snapshot().Home.Data; !initial.Enabled || initial.Available || initial.State != "unavailable" {
		t.Fatalf("initial home state = %#v", initial)
	}
	if err := jobs[1].Run(context.Background()); err != nil {
		t.Fatalf("home assistant job error = %v", err)
	}
	if got := store.Snapshot().Home.Data; got.State != "on" || got.Name != "客厅风扇" {
		t.Fatalf("home snapshot = %#v", got)
	}
	if store.Ready() {
		t.Fatal("home assistant data must not make the core store ready")
	}

	reader.err = errors.New("not_found")
	jobs = appendHomeAssistantJob(base, cfg, store, reader)
	_ = jobs[1].Run(context.Background())
	if got := store.Snapshot().Home.Error; got != "not_found" {
		t.Fatalf("home error = %q, want not_found", got)
	}
}

func TestAppendQBittorrentJobIsOptionalAndIndependent(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := state.New("test", state.StaleAfter{Downloads: 15 * time.Second}, func() time.Time { return now })
	base := []collector.Job{{Name: "existing"}}
	if got := appendQBittorrentJob(base, config.Config{}, store, nil); len(got) != 1 {
		t.Fatalf("disabled jobs = %#v", got)
	}
	cfg := config.Config{
		QBittorrent: config.QBittorrentConfig{Enabled: true, CallTimeout: config.Duration{Duration: 5 * time.Second}},
		Refresh:     config.RefreshConfig{QBittorrent: config.Duration{Duration: 15 * time.Second}},
	}
	reader := fakeDownloadReader{status: model.DownloadStatus{ActiveCount: 1}}
	jobs := appendQBittorrentJob(base, cfg, store, reader)
	if len(jobs) != 2 || jobs[1].Name != "qbittorrent" || jobs[1].Every != 15*time.Second || jobs[1].Timeout != 5*time.Second {
		t.Fatalf("jobs = %#v", jobs)
	}
	if err := jobs[1].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Downloads.Data.ActiveCount; got != 1 {
		t.Fatalf("active count = %d", got)
	}
	reader.err = errors.New("permission denied")
	jobs = appendQBittorrentJob(base, cfg, store, reader)
	_ = jobs[1].Run(context.Background())
	if got := store.Snapshot().Downloads.Error; got != "unauthorized" {
		t.Fatalf("downloads error = %q", got)
	}
}

func TestAppendMediaJobsAreOptionalAndIndependent(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := state.New("test", state.StaleAfter{Plex: 15 * time.Second, Jellyfin: 15 * time.Second}, func() time.Time { return now })
	base := []collector.Job{{Name: "existing"}}
	if got := appendPlexJob(base, config.Config{}, store, nil); len(got) != 1 {
		t.Fatalf("disabled Plex jobs = %#v", got)
	}
	cfg := config.Config{
		Plex:     config.PlexConfig{Enabled: true, CallTimeout: config.Duration{Duration: 5 * time.Second}},
		Jellyfin: config.JellyfinConfig{Enabled: true, CallTimeout: config.Duration{Duration: 6 * time.Second}},
		Refresh: config.RefreshConfig{
			Plex: config.Duration{Duration: 15 * time.Second}, Jellyfin: config.Duration{Duration: 20 * time.Second},
		},
	}
	plexReader := fakeMediaReader{status: model.MediaStatus{Sessions: []model.MediaSession{{Title: "Movie"}}}}
	jellyfinReader := fakeMediaReader{status: model.MediaStatus{Sessions: []model.MediaSession{{Title: "Show"}}}}
	jobs := appendPlexJob(base, cfg, store, plexReader)
	jobs = appendJellyfinJob(jobs, cfg, store, jellyfinReader)
	if len(jobs) != 3 || jobs[1].Name != "plex" || jobs[1].Every != 15*time.Second || jobs[1].Timeout != 5*time.Second || jobs[2].Name != "jellyfin" || jobs[2].Every != 20*time.Second || jobs[2].Timeout != 6*time.Second {
		t.Fatalf("media jobs = %#v", jobs)
	}
	if err := jobs[1].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := jobs[2].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot(); got.Plex.Data.Sessions[0].Title != "Movie" || got.Jellyfin.Data.Sessions[0].Title != "Show" {
		t.Fatalf("media snapshot = %#v", got)
	}
}

func TestPlexJobPollsOncePerMinuteWhenIdleAndEveryTickDuringPlayback(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := state.New("test", state.StaleAfter{Plex: 15 * time.Second}, func() time.Time { return now })
	cfg := config.Config{
		Plex:    config.PlexConfig{Enabled: true, CallTimeout: config.Duration{Duration: 5 * time.Second}},
		Refresh: config.RefreshConfig{Plex: config.Duration{Duration: 15 * time.Second}},
	}
	reader := &scriptedMediaReader{statuses: []model.MediaStatus{
		{},
		{Sessions: []model.MediaSession{{Title: "Third-party player"}}},
		{Sessions: []model.MediaSession{{Title: "Third-party player"}}},
		{},
		{},
	}}
	job := appendPlexJob(nil, cfg, store, reader)[0]

	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := job.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if reader.calls != 1 {
		t.Fatalf("idle Plex requests after 45 seconds = %d, want 1", reader.calls)
	}
	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 2 || len(store.Snapshot().Plex.Data.Sessions) != 1 {
		t.Fatalf("Plex did not discover playback on the one-minute check: calls=%d snapshot=%#v", reader.calls, store.Snapshot().Plex.Data)
	}
	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 3 {
		t.Fatalf("active Plex requests after one tick = %d, want 3", reader.calls)
	}
	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 4 || len(store.Snapshot().Plex.Data.Sessions) != 0 {
		t.Fatalf("Plex did not clear stopped playback: calls=%d snapshot=%#v", reader.calls, store.Snapshot().Plex.Data)
	}
}

func TestAppendUptimeKumaJobIsOptional(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := state.New("test", state.StaleAfter{Monitors: 30 * time.Second}, func() time.Time { return now })
	base := []collector.Job{{Name: "existing"}}
	if got := appendUptimeKumaJob(base, config.Config{}, store, nil); len(got) != 1 {
		t.Fatalf("disabled jobs = %#v", got)
	}
	cfg := config.Config{
		UptimeKuma: config.UptimeKumaConfig{Enabled: true, CallTimeout: config.Duration{Duration: 5 * time.Second}},
		Refresh:    config.RefreshConfig{UptimeKuma: config.Duration{Duration: 30 * time.Second}},
	}
	reader := fakeMonitorReader{status: model.MonitorStatus{Total: 8, DownNames: []string{"site"}}}
	jobs := appendUptimeKumaJob(base, cfg, store, reader)
	if len(jobs) != 2 || jobs[1].Name != "uptime_kuma" || jobs[1].Every != 30*time.Second || jobs[1].Timeout != 5*time.Second {
		t.Fatalf("jobs = %#v", jobs)
	}
	if err := jobs[1].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Monitors.Data; got.Total != 8 || len(got.DownNames) != 1 {
		t.Fatalf("monitors = %#v", got)
	}
}

func TestAppendScrutinyJobIsOptional(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := state.New("test", state.StaleAfter{DiskHealth: 30 * time.Minute}, func() time.Time { return now })
	base := []collector.Job{{Name: "existing"}}
	if got := appendScrutinyJob(base, config.Config{}, store, nil); len(got) != 1 {
		t.Fatalf("disabled jobs = %#v", got)
	}
	cfg := config.Config{
		Scrutiny: config.ScrutinyConfig{Enabled: true, CallTimeout: config.Duration{Duration: 5 * time.Second}},
		Refresh:  config.RefreshConfig{Scrutiny: config.Duration{Duration: 30 * time.Minute}},
	}
	reader := fakeDiskHealthReader{status: []model.DiskHealthStatus{{Name: "sda", State: "healthy"}}}
	jobs := appendScrutinyJob(base, cfg, store, reader)
	if len(jobs) != 2 || jobs[1].Name != "scrutiny" || jobs[1].Every != 30*time.Minute || jobs[1].Timeout != 5*time.Second {
		t.Fatalf("jobs = %#v", jobs)
	}
	if err := jobs[1].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().DiskHealth.Data; len(got) != 1 || got[0].Name != "sda" || got[0].State != "healthy" {
		t.Fatalf("disk health = %#v", got)
	}
}
