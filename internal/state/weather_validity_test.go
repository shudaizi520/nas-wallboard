package state

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func TestWeatherSnapshotRecalculatesRainWithoutRenewingFreshness(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	store := New("test", StaleAfter{Weather: 30 * time.Minute}, func() time.Time { return now })
	var value model.WeatherStatus
	if err := json.Unmarshal([]byte(`{"enabled":true,"rain_summary":"约半小时后可能有雨","rain_points":[{"at":"2026-09-30T12:25:00Z","amount":0.2},{"at":"2026-09-30T12:30:00Z","amount":0.2},{"at":"2026-09-30T12:35:00Z","amount":0.2}],"components":{"rain":{"updated_at":"2026-09-30T12:00:00Z","expires_at":"2026-09-30T12:30:00Z"}}}`), &value); err != nil {
		t.Fatal(err)
	}
	store.SetWeather(value, nil)
	now = now.Add(26 * time.Minute)
	got := store.Snapshot().Weather
	if got.Data.RainSummary != "当前可能有雨" || !got.Data.Components.Rain.UpdatedAt.Equal(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("frozen relative rain time: %#v", got)
	}
	now = now.Add(10 * time.Minute)
	if !store.Snapshot().Weather.Data.Components.Rain.Stale {
		t.Fatal("expired cache still usable")
	}
}

func TestCachedDailyRowsRollOverAtTheirLocalMidnight(t *testing.T) {
	now := time.Date(2026, 9, 30, 15, 50, 0, 0, time.UTC) //23:50 China
	store := New("test", StaleAfter{Weather: 30 * time.Minute}, func() time.Time { return now })
	var value model.WeatherStatus
	if err := json.Unmarshal([]byte(`{"enabled":true,"forecasts":[{"condition":"小雨","start_at":"2026-10-01T00:00:00+08:00","end_at":"2026-10-02T00:00:00+08:00"},{"condition":"阴","start_at":"2026-10-02T00:00:00+08:00","end_at":"2026-10-03T00:00:00+08:00"}],"components":{"forecast":{"updated_at":"2026-09-30T15:50:00Z","expires_at":"2026-09-30T18:50:00Z"}}}`), &value); err != nil {
		t.Fatal(err)
	}
	store.SetWeather(value, nil)
	now = now.Add(20 * time.Minute)
	got := store.Snapshot().Weather.Data.Forecasts
	if len(got) != 1 || !strings.Contains(got[0].Condition, "阴") {
		t.Fatalf("yesterday's tomorrow still future after midnight: %#v", got)
	}
}

func TestWeatherRainSamplesAreIsolatedAndExpireAtExactBoundary(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	store := New("test", StaleAfter{Weather: 30 * time.Minute}, func() time.Time { return now })
	value := model.WeatherStatus{Enabled: true, RainPoints: []model.RainPoint{{At: now, Amount: 0.2}}, Components: &model.WeatherComponents{Rain: model.WeatherComponent{UpdatedAt: now, ExpiresAt: now.Add(5 * time.Minute)}}}
	store.SetWeather(value, nil)
	value.RainPoints[0].Amount = 0
	snapshot := store.Snapshot()
	if snapshot.Weather.Data.RainPoints[0].Amount != 0.2 {
		t.Fatal("caller mutated store rain samples")
	}
	snapshot.Weather.Data.RainPoints[0].Amount = 0
	if store.Snapshot().Weather.Data.RainPoints[0].Amount != 0.2 {
		t.Fatal("snapshot mutated store rain samples")
	}
	now = now.Add(5 * time.Minute)
	if !store.Snapshot().Weather.Data.Components.Rain.Stale {
		t.Fatal("rain still fresh at exact expiry boundary")
	}
}
