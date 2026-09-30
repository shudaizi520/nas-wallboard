package state

import (
	"errors"
	"example.com/nas-wallboard/internal/model"
	"testing"
	"time"
)

func TestWeatherStorePublishesPartialResultsAndRetainsSourceFreshness(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	stamp := now.Add(-time.Minute)
	store := New("test", StaleAfter{Weather: 30 * time.Minute}, func() time.Time { return now })
	value := model.WeatherStatus{Enabled: true, Temperature: 29, Condition: "多云", Components: &model.WeatherComponents{
		Current: model.WeatherComponent{UpdatedAt: stamp, ExpiresAt: stamp.Add(30 * time.Minute)},
		Rain:    model.WeatherComponent{Error: "unavailable"},
	}}
	store.SetWeather(value, errors.New("rain failed"))
	got := store.Snapshot().Weather
	if !got.Data.Enabled || got.Data.Temperature != 29 || !got.Partial || !got.UpdatedAt.Equal(stamp) {
		t.Fatalf("partial source result lost or timestamp renewed: %#v", got)
	}
	value.Components.Current.UpdatedAt = now
	got.Data.Components.Current.UpdatedAt = now
	if !store.Snapshot().Weather.Data.Components.Current.UpdatedAt.Equal(stamp) {
		t.Fatal("component metadata aliases caller or snapshot")
	}
	now = now.Add(time.Hour)
	if !store.Snapshot().Weather.Data.Components.Current.Stale {
		t.Fatal("expired current component still fresh")
	}
}

func TestWeatherStoreInitialFailureKeepsEnabledPlaceholderWithoutSuccessTime(t *testing.T) {
	store := New("test", StaleAfter{}, nil)
	store.SetWeather(model.WeatherStatus{Enabled: true, Components: &model.WeatherComponents{}}, errors.New("all failed"))
	got := store.Snapshot().Weather
	if !got.Data.Enabled || !got.UpdatedAt.IsZero() || got.Error == "" || got.Partial {
		t.Fatalf("initial failure = %#v", got)
	}
}
