package state

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func TestWeatherSnapshotDropsExpiredWarningEvenDuringAlertRefreshFailure(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	store := New("test", StaleAfter{Weather: 30 * time.Minute}, func() time.Time { return now })
	var value model.WeatherStatus
	if err := json.Unmarshal([]byte(`{"enabled":true,"warnings":[{"id":"expired","title":"台风红色预警","color":"red","expires_at":"2026-10-11T12:01:00Z"},{"id":"active","title":"雷电黄色预警","color":"yellow","expires_at":"2026-10-11T13:00:00Z"}],"components":{"alerts":{"updated_at":"2026-10-11T12:00:00Z","expires_at":"2026-10-11T12:15:00Z","error":"unavailable"}}}`), &value); err != nil {
		t.Fatal(err)
	}
	store.SetWeather(value, errors.New("alert request failed"))
	now = now.Add(time.Minute)
	got := store.Snapshot().Weather
	if len(got.Data.Warnings) != 1 || got.Data.Warnings[0].ID != "active" || got.Data.Components.Alerts.Error != "unavailable" {
		t.Fatalf("own expiry ignored while preserving last known warnings: %+v", got)
	}
	got.Data.Warnings[0].Title = "mutated"
	if store.Snapshot().Weather.Data.Warnings[0].Title != "雷电黄色预警" || len(value.Warnings) != 2 {
		t.Fatal("expiry projection mutated source or another snapshot")
	}
}
