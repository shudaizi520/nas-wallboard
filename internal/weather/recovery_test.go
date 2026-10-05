package weather

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// This exercises the actual cache and HTTP decoding. Only the remote service
// boundary is controlled: the daily source is unavailable during startup.
func TestFailedDailyForecastRecoversWithoutRefetchingHealthySources(t *testing.T) {
	now := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	counts := map[string]int{}
	var mu sync.Mutex
	available := false
	client, err := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		counts[r.URL.Path]++
		mu.Unlock()
		switch {
		case strings.Contains(r.URL.Path, "daily"):
			if !available {
				return response(503, `{}`), nil
			}
			return response(200, validDailyPayload(now)), nil
		case strings.Contains(r.URL.Path, "weatheralert"):
			return response(200, `{"alerts":[]}`), nil
		case strings.Contains(r.URL.Path, "current"):
			return response(200, `{"condition":{"text":"多云","code":"101"},"temperature":{"value":29}}`), nil
		default:
			return response(200, validRainPayload(now)), nil
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return now }
	if _, err := client.Refresh(context.Background()); err == nil {
		t.Fatal("startup failure was not reported")
	}
	available = true
	now = now.Add(time.Minute)
	got, err := client.Refresh(context.Background())
	if err != nil || len(got.Forecasts) != 2 || got.Components.Forecast.Stale {
		t.Fatalf("daily source did not recover after one minute: forecasts=%d stale=%v err=%v", len(got.Forecasts), got.Components.Forecast.Stale, err)
	}
	for path, count := range counts {
		want := 1
		if strings.Contains(path, "daily") {
			want = 2
		}
		if count != want {
			t.Fatalf("request count for %s = %d, want %d", path, count, want)
		}
	}
	stamp := got.Components.Forecast.UpdatedAt
	now = now.Add(time.Minute)
	if _, err := client.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !client.cache.status.Components.Forecast.UpdatedAt.Equal(stamp) || counts["/weather/v1/daily/22.54/114.06"] != 2 {
		t.Fatal("recovered daily source did not resume its normal hourly cache")
	}
	// A later outage gets a new bounded budget, not the previous outage's
	// backoff. Removing the success reset would suppress this one-minute retry.
	available = false
	now = stamp.Add(time.Hour)
	if _, err := client.Refresh(context.Background()); err == nil {
		t.Fatal("later failure was not reported")
	}
	available = true
	now = now.Add(time.Minute)
	if _, err := client.Refresh(context.Background()); err != nil {
		t.Fatalf("a recovered source did not reset its retry budget: %v", err)
	}
}

func TestPersistentWeatherFailureUsesOnlyThreeEarlyRetries(t *testing.T) {
	start := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	now := start
	counts := map[string]int{}
	var mu sync.Mutex
	client, _ := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		counts[r.URL.Path]++
		mu.Unlock()
		return response(503, `{}`), nil
	})})
	client.now = func() time.Time { return now }
	for _, step := range []struct {
		seconds int
		want    int
	}{{0, 1}, {59, 1}, {60, 2}, {179, 2}, {180, 3}, {419, 3}, {420, 4}, {480, 4}, {600, 4}} {
		now = start.Add(time.Duration(step.seconds) * time.Second)
		if _, err := client.Refresh(context.Background()); err == nil {
			t.Fatal("failed sources reported success")
		}
		for path, count := range counts {
			if count != step.want {
				t.Fatalf("at %ds request count for %s = %d, want %d", step.seconds, path, count, step.want)
			}
		}
		if !client.RefreshedAt().IsZero() {
			t.Fatal("failure advanced the successful acquisition timestamp")
		}
	}
}
