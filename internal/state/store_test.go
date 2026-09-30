package state

import (
	"errors"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func testStore(now *time.Time) *Store {
	intervals := StaleAfter{
		Realtime:   5 * time.Second,
		Apps:       30 * time.Second,
		Alerts:     30 * time.Second,
		System:     time.Minute,
		Pools:      time.Minute,
		Disks:      time.Minute,
		Weather:    15 * time.Minute,
		Home:       30 * time.Second,
		HomePower:  15 * time.Second,
		Downloads:  15 * time.Second,
		Plex:       15 * time.Second,
		Jellyfin:   15 * time.Second,
		Monitors:   30 * time.Second,
		DiskHealth: 30 * time.Minute,
	}
	return New("test-version", intervals, func() time.Time { return *now })
}

func TestStoreCachesHomeAssistantPowerIndependently(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := testStore(&now)
	store.SetHomePower(model.PowerStatus{Available: true, Watts: 38}, nil)
	updated := now

	now = now.Add(10 * time.Second)
	store.SetHomePower(model.PowerStatus{Watts: 999}, errors.New("upstream failed"))
	snapshot := store.Snapshot()
	if snapshot.HomePower.Data.Watts != 38 || !snapshot.HomePower.Data.Available {
		t.Fatalf("power last-good data = %#v", snapshot.HomePower)
	}
	if snapshot.HomePower.Error != "unavailable" || !snapshot.HomePower.UpdatedAt.Equal(updated) || snapshot.HomePower.Stale {
		t.Fatalf("power module = %#v", snapshot.HomePower)
	}

	now = updated.Add(45*time.Second + time.Nanosecond)
	if !store.Snapshot().HomePower.Stale {
		t.Fatal("power module must become stale after three configured intervals")
	}
}

func TestStoreCachesHomeAssistantFanIndependently(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := testStore(&now)
	onSince := now.Add(-2 * time.Hour)
	percentage := 42.0
	oscillating := true
	store.SetHome(model.FanStatus{
		Enabled: true, Available: true, Name: "客厅风扇", State: "on",
		OnSince: &onSince, Percentage: &percentage, PresetMode: "nature", Oscillating: &oscillating,
		RemindAfterSeconds: 7200,
	}, nil)
	updated := now

	now = now.Add(10 * time.Second)
	store.SetHome(model.FanStatus{State: "off"}, errors.New("upstream body with bearer secret"))
	snapshot := store.Snapshot()
	if model.SchemaVersion != 6 {
		t.Fatalf("SchemaVersion = %d, want 6", model.SchemaVersion)
	}
	if snapshot.Home.Data.State != "on" || snapshot.Home.Data.OnSince == nil || !snapshot.Home.Data.OnSince.Equal(onSince) || snapshot.Home.Data.Percentage == nil || *snapshot.Home.Data.Percentage != 42 {
		t.Fatalf("home last-good data = %#v", snapshot.Home)
	}
	if snapshot.Home.Error != "unavailable" || !snapshot.Home.UpdatedAt.Equal(updated) || snapshot.Home.Stale {
		t.Fatalf("home module = %#v", snapshot.Home)
	}

	now = updated.Add(90*time.Second + time.Nanosecond)
	if !store.Snapshot().Home.Stale {
		t.Fatal("home module must become stale after three configured intervals")
	}
}

func TestStoreSuccessfulUpdatesReplaceData(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := testStore(&now)
	store.SetSystem(model.SystemStatus{Hostname: "first"}, nil)
	now = now.Add(time.Second)
	store.SetSystem(model.SystemStatus{Hostname: "second"}, nil)
	store.Connected(true)

	snapshot := store.Snapshot()
	if snapshot.Version != "test-version" || snapshot.SchemaVersion != model.SchemaVersion || !snapshot.Connected {
		t.Fatalf("snapshot metadata = %#v", snapshot)
	}
	if snapshot.System.Data.Hostname != "second" || !snapshot.System.UpdatedAt.Equal(now) || snapshot.System.Error != "" || snapshot.System.Stale {
		t.Fatalf("system module = %#v", snapshot.System)
	}
}

func TestStoreFailureKeepsLastGoodDataAndUsesPublicErrorCode(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := testStore(&now)
	store.SetRealtime(model.RealtimeStatus{CPUPercent: 12}, nil)
	updated := now
	now = now.Add(10 * time.Second)
	store.SetRealtime(model.RealtimeStatus{CPUPercent: 99}, errors.New("connection failed with secret raw payload"))

	snapshot := store.Snapshot()
	if snapshot.Realtime.Data.CPUPercent != 12 || !snapshot.Realtime.UpdatedAt.Equal(updated) {
		t.Fatalf("failed update replaced last good data: %#v", snapshot.Realtime)
	}
	if snapshot.Realtime.Error != "unavailable" || snapshot.Realtime.Stale {
		t.Fatalf("failed module state = %#v", snapshot.Realtime)
	}
	if snapshot.Realtime.Error == "connection failed with secret raw payload" {
		t.Fatal("raw error escaped into public snapshot")
	}

	now = updated.Add(15*time.Second + time.Nanosecond)
	if !store.Snapshot().Realtime.Stale {
		t.Fatal("module must become stale after three configured intervals")
	}
}

func TestStoreMapsStableErrorCodes(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	tests := []struct {
		err  error
		want string
	}{
		{errors.New("permission denied"), "unauthorized"},
		{errors.New("method not found"), "unsupported"},
		{errors.New("not_found"), "not_found"},
		{errors.New("deadline exceeded"), "timeout"},
		{errors.New("broken wire"), "unavailable"},
	}
	for _, tt := range tests {
		store := testStore(&now)
		store.SetAlerts(nil, tt.err)
		if got := store.Snapshot().Alerts.Error; got != tt.want {
			t.Errorf("error %q mapped to %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestStoreSnapshotCannotMutateStoreState(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := testStore(&now)
	store.SetPools([]model.PoolStatus{{Name: "tank"}}, nil)
	store.SetDisks([]model.DiskStatus{{Name: "sda"}}, nil)
	store.SetApps([]model.AppStatus{{Name: "plex"}}, nil)
	store.SetAlerts([]model.AlertStatus{{Title: "safe"}}, nil)
	store.SetDownloads(model.DownloadStatus{Items: []model.DownloadItem{{Name: "download"}}}, nil)
	store.SetPlex(model.MediaStatus{Sessions: []model.MediaSession{{Title: "plex"}}}, nil)
	store.SetJellyfin(model.MediaStatus{Sessions: []model.MediaSession{{Title: "jellyfin"}}}, nil)
	store.SetMonitors(model.MonitorStatus{DownNames: []string{"site"}}, nil)
	store.SetWeather(model.WeatherStatus{Warnings: []model.WeatherWarning{{Title: "暴雨"}}, Forecasts: []model.WeatherForecast{{Condition: "多云"}}}, nil)
	store.SetDiskHealth([]model.DiskHealthStatus{{Name: "sda", State: "healthy"}}, nil)

	first := store.Snapshot()
	first.Pools.Data[0].Name = "mutated"
	first.Disks.Data[0].Name = "mutated"
	first.Apps.Data[0].Name = "mutated"
	first.Alerts.Data[0].Title = "mutated"
	first.Downloads.Data.Items[0].Name = "mutated"
	first.Plex.Data.Sessions[0].Title = "mutated"
	first.Jellyfin.Data.Sessions[0].Title = "mutated"
	first.Monitors.Data.DownNames[0] = "mutated"
	first.Weather.Data.Warnings[0].Title = "mutated"
	first.Weather.Data.Forecasts[0].Condition = "mutated"
	first.DiskHealth.Data[0].State = "failed"
	second := store.Snapshot()
	if second.Pools.Data[0].Name != "tank" || second.Disks.Data[0].Name != "sda" || second.Apps.Data[0].Name != "plex" || second.Alerts.Data[0].Title != "safe" || second.Downloads.Data.Items[0].Name != "download" || second.Plex.Data.Sessions[0].Title != "plex" || second.Jellyfin.Data.Sessions[0].Title != "jellyfin" || second.Monitors.Data.DownNames[0] != "site" || second.Weather.Data.Warnings[0].Title != "暴雨" || second.Weather.Data.Forecasts[0].Condition != "多云" || second.DiskHealth.Data[0].State != "healthy" {
		t.Fatalf("snapshot mutated store: %#v", second)
	}
}

func TestStoreWeatherPointerMeasurementsAreIsolated(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := testStore(&now)
	weather := model.WeatherStatus{
		FeelsLike:               stateFloat64Pointer(34),
		HumidityPercent:         stateFloat64Pointer(64),
		WindScale:               stateIntPointer(6),
		WindGustMetersPerSecond: stateFloat64Pointer(18.1),
		UVIndex:                 stateFloat64Pointer(8),
	}
	store.SetWeather(weather, nil)

	*weather.FeelsLike = 99
	*weather.HumidityPercent = 99
	*weather.WindScale = 17
	*weather.WindGustMetersPerSecond = 99
	*weather.UVIndex = 15
	first := store.Snapshot()
	if first.Weather.Data.FeelsLike == nil || *first.Weather.Data.FeelsLike != 34 ||
		first.Weather.Data.HumidityPercent == nil || *first.Weather.Data.HumidityPercent != 64 ||
		first.Weather.Data.WindScale == nil || *first.Weather.Data.WindScale != 6 ||
		first.Weather.Data.WindGustMetersPerSecond == nil || *first.Weather.Data.WindGustMetersPerSecond != 18.1 ||
		first.Weather.Data.UVIndex == nil || *first.Weather.Data.UVIndex != 8 {
		t.Fatalf("SetWeather retained caller pointers: %#v", first.Weather.Data)
	}

	*first.Weather.Data.FeelsLike = 1
	*first.Weather.Data.HumidityPercent = 1
	*first.Weather.Data.WindScale = 1
	*first.Weather.Data.WindGustMetersPerSecond = 1
	*first.Weather.Data.UVIndex = 1
	second := store.Snapshot()
	if *second.Weather.Data.FeelsLike != 34 || *second.Weather.Data.HumidityPercent != 64 ||
		*second.Weather.Data.WindScale != 6 || *second.Weather.Data.WindGustMetersPerSecond != 18.1 ||
		*second.Weather.Data.UVIndex != 8 {
		t.Fatalf("snapshot retained weather pointers: %#v", second.Weather.Data)
	}
}

func stateFloat64Pointer(value float64) *float64 { return &value }
func stateIntPointer(value int) *int             { return &value }

func TestStoreExternalSourcesFailIndependently(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := testStore(&now)
	store.SetDownloads(model.DownloadStatus{ActiveCount: 1}, nil)
	store.SetPlex(model.MediaStatus{Sessions: []model.MediaSession{{Title: "Movie"}}}, nil)
	store.SetJellyfin(model.MediaStatus{Sessions: []model.MediaSession{{Title: "Show"}}}, nil)
	store.SetMonitors(model.MonitorStatus{Total: 8}, nil)
	now = now.Add(10 * time.Second)
	store.SetPlex(model.MediaStatus{}, errors.New("token leaked in upstream body"))

	snapshot := store.Snapshot()
	if snapshot.Plex.Error != "unavailable" || len(snapshot.Plex.Data.Sessions) != 1 {
		t.Fatalf("plex module = %#v", snapshot.Plex)
	}
	if snapshot.Downloads.Error != "" || snapshot.Jellyfin.Error != "" || snapshot.Monitors.Error != "" {
		t.Fatalf("unrelated source errors = %#v %#v %#v", snapshot.Downloads, snapshot.Jellyfin, snapshot.Monitors)
	}
}

func TestStoreReadyRequiresConnectionAndCoreSnapshots(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := testStore(&now)
	store.Connected(true)
	store.SetSystem(model.SystemStatus{}, nil)
	store.SetRealtime(model.RealtimeStatus{}, nil)
	if store.Ready() {
		t.Fatal("store ready before pools snapshot")
	}
	store.SetPools([]model.PoolStatus{}, nil)
	if !store.Ready() {
		t.Fatal("store not ready after connection and core snapshots")
	}
	store.Connected(false)
	if store.Ready() {
		t.Fatal("disconnected store reported ready")
	}
}

func TestStoreKeepsMemoryAndReplicationLastGoodValuesIndependently(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := testStore(&now)
	store.SetMemory(model.MemoryStatus{TotalBytes: 100, AvailableBytes: 20, AvailablePercent: 20}, nil)
	store.SetReplication(model.ReplicationStatus{Total: 3, Enabled: 2, Failed: 1}, nil)
	updated := now
	now = now.Add(10 * time.Second)
	store.SetReplication(model.ReplicationStatus{Failed: 99}, errors.New("remote credential leaked"))
	snapshot := store.Snapshot()
	if snapshot.Memory.Data.AvailablePercent != 20 || snapshot.Memory.Error != "" {
		t.Fatalf("memory = %#v", snapshot.Memory)
	}
	if snapshot.Replication.Data.Failed != 1 || snapshot.Replication.Error != "unavailable" || !snapshot.Replication.UpdatedAt.Equal(updated) {
		t.Fatalf("replication = %#v", snapshot.Replication)
	}
}
