package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func TestDailyForecastReusesValidatedSunTimesWithoutExtraRequests(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("", 8*3600))
	for _, tc := range []struct {
		name, sunrise, sunset string
		valid                 bool
	}{
		{"local times", "2026-10-11T06:13:00+08:00", "2026-10-11T18:02:00+08:00", true},
		{"equivalent UTC times", "2026-10-10T22:13:00Z", "2026-10-11T10:02:00Z", true},
		{"missing", "", "", false},
		{"one missing", "2026-10-11T06:13:00+08:00", "", false},
		{"malformed", "bad", "2026-10-11T18:02:00+08:00", false},
		{"reversed", "2026-10-11T18:02:00+08:00", "2026-10-11T06:13:00+08:00", false},
		{"wrong date", "2026-10-12T06:13:00+08:00", "2026-10-12T18:02:00+08:00", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			payload := fmt.Sprintf(`{"days":[{"forecastStartTime":"2026-10-11T00:00:00+08:00","forecastEndTime":"2026-10-12T00:00:00+08:00","astro":{"sunrise":%q,"sunset":%q},"temperatureMin":{"value":-8,"unit":"°C"},"temperatureMax":{"value":2,"unit":"°C"},"daytime":{"condition":{"text":"晴","code":"100"}}}]}`, tc.sunrise, tc.sunset)
			client, _ := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.URL.Path != "/weather/v1/daily/22.54/114.06" {
					t.Errorf("unexpected astronomy request: %s", r.URL.Path)
				}
				return response(200, payload), nil
			})})
			client.now = func() time.Time { return now }
			var weather model.WeatherStatus
			if err := client.readForecast(context.Background(), &weather); err != nil || weather.TodayForecast == nil {
				t.Fatalf("optional sun data broke daily forecast: %v", err)
			}
			encoded, _ := json.Marshal(weather.TodayForecast)
			var solar struct{ Sunrise, Sunset time.Time }
			_ = json.Unmarshal(encoded, &solar)
			if tc.valid {
				rise, _ := time.Parse(time.RFC3339, tc.sunrise)
				set, _ := time.Parse(time.RFC3339, tc.sunset)
				if !solar.Sunrise.Equal(rise) || !solar.Sunset.Equal(set) {
					t.Fatalf("valid existing daily sun times lost: %s", encoded)
				}
			} else if !solar.Sunrise.IsZero() || !solar.Sunset.IsZero() {
				t.Fatalf("invalid sun times retained: %s", encoded)
			}
			if requests != 1 {
				t.Fatalf("solar rendering multiplied API reads: %d", requests)
			}
		})
	}
}
