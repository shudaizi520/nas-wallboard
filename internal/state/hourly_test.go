package state

import (
	"example.com/nas-wallboard/internal/model"
	"testing"
	"time"
)

func TestHourlySnapshotsOwnSamplesAndMetadataAndExpireIndependently(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	store := New("test", StaleAfter{Weather: time.Hour}, func() time.Time { return now })
	good := model.WeatherComponent{UpdatedAt: now, ExpiresAt: now.Add(time.Hour)}
	hourly := good
	hourly.ExpiresAt = now.Add(time.Minute)
	value := model.WeatherStatus{Enabled: true, Hourly: []model.WeatherHour{{At: now.Add(2 * time.Hour), Temperature: 30}}, Components: &model.WeatherComponents{Current: good, Rain: good, Alerts: good, Forecast: good, Hourly: &hourly}}
	store.SetWeather(value, nil)
	value.Hourly[0].Temperature = 999
	value.Components.Hourly.UpdatedAt = now.Add(time.Hour)
	got := store.Snapshot().Weather
	if got.Data.Hourly[0].Temperature != 30 || !got.Data.Components.Hourly.UpdatedAt.Equal(now) {
		t.Fatal("hourly cache aliases collector")
	}
	got.Data.Hourly[0].Temperature = 1000
	got.Data.Components.Hourly.Stale = true
	if store.Snapshot().Weather.Data.Components.Hourly.Stale || store.Snapshot().Weather.Data.Hourly[0].Temperature != 30 {
		t.Fatal("hourly snapshot aliases cache")
	}
	now = now.Add(time.Minute)
	got = store.Snapshot().Weather
	if !got.Partial || !got.Data.Components.Hourly.Stale || got.Data.Components.Current.Stale {
		t.Fatalf("hourly expiry renewed by sibling: %#v", got)
	}
}
