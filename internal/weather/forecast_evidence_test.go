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

// Exercises real response decoding; the transport replaces only the upstream
// request. A parser which discards fields or invents zero makes this fail.
func TestDailyRetainsPeriodRainAmount(t *testing.T) {
	for _, tc := range []struct {
		name, amount, cloud string
		known               bool
	}{
		{"explicit zero", `{"value":0,"unit":"mm"}`, `0.5`, true},
		{"positive amount", `{"value":0.23,"unit":"mm"}`, `0.5`, true},
		{"missing amount", `{}`, `null`, false},
		{"null amount", `{"value":null,"unit":"mm"}`, `null`, false},
		{"invalid amount and cloud", `{"value":-1,"unit":"mm"}`, `2`, false},
		{"unsupported unit", `{"value":1,"unit":"inch"}`, `null`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			payload := fmt.Sprintf(`{"days":[{"forecastStartTime":"2026-10-12T00:00:00+08:00","forecastEndTime":"2026-10-13T00:00:00+08:00","temperatureMin":{"value":24},"temperatureMax":{"value":32},"daytime":{"condition":{"text":"小雨","code":"305"},"precipitation":{"probability":0.02,"type":"rain","amount":%s},"cloudCover":%s},"nighttime":{"condition":{"text":"多云","code":"101"},"precipitation":{"probability":0.65,"type":"rain","amount":{"value":0.45,"unit":"mm"}},"cloudCover":0.8}}]}`, tc.amount, tc.cloud)
			client, _ := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.URL.Path != "/weather/v1/daily/22.54/114.06" || r.URL.Query().Get("days") != "5" {
					t.Errorf("changed forecast request: %s", r.URL.Path)
				}
				return response(200, payload), nil
			})})
			client.now = func() time.Time { return time.Date(2026, 10, 12, 8, 0, 0, 0, time.FixedZone("", 8*3600)) }
			var weather model.WeatherStatus
			if err := client.readForecast(context.Background(), &weather); err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(weather.TodayForecast)
			var day map[string]any
			_ = json.Unmarshal(encoded, &day)
			if _, ok := day["precipitation_amount"]; ok != tc.known {
				t.Fatalf("rain amount presence=%v want=%v: %s", ok, tc.known, encoded)
			}
			if day["condition_code"] != "305" || day["condition"] != "小雨" || day["night_precipitation_amount"] != .45 || day["night_precipitation_probability"] != .65 || requests != 1 {
				t.Fatalf("night evidence or single-request contract lost: %s requests=%d", encoded, requests)
			}
		})
	}
}

func TestHourlyAmountValidationNeverRewritesCondition(t *testing.T) {
	for _, tc := range []struct {
		amount string
	}{
		{`{"value":0,"unit":"mm"}`}, {`{}`},
		{`{"value":null,"unit":"mm"}`}, {`{"value":-1,"unit":"mm"}`},
		{`{"value":0,"unit":"inch"}`},
	} {
		t.Run(tc.amount, func(t *testing.T) {
			payload := fmt.Sprintf(`{"hours":[{"forecastTime":"2026-10-12T13:00:00+08:00","condition":{"text":"小雨","code":"305"},"temperature":{"value":30,"unit":"°C"},"precipitation":{"probability":0.02,"type":"rain","amount":%s},"cloudCover":0.5}]}`, tc.amount)
			client, _ := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, payload), nil })})
			client.now = func() time.Time { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.FixedZone("", 8*3600)) }
			var weather model.WeatherStatus
			if err := client.readHourly(context.Background(), &weather); err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(weather.Hourly[0])
			var hour map[string]any
			_ = json.Unmarshal(encoded, &hour)
			if hour["condition_code"] != "305" || hour["condition"] != "小雨" || weather.Hourly[0].PrecipitationAmount != 0 {
				t.Fatalf("amount validation changed source phenomenon: %s", encoded)
			}
		})
	}
}

func TestProjectionDoesNotAliasForecastEvidence(t *testing.T) {
	var weather model.WeatherStatus
	_ = json.Unmarshal([]byte(`{"today_forecast":{"start_at":"2026-10-12T00:00:00Z","end_at":"2026-10-13T00:00:00Z","precipitation_amount":0.23,"cloud_cover":0.5,"night_precipitation_amount":0,"night_cloud_cover":0.1},"forecasts":[{"start_at":"2026-10-13T00:00:00Z","end_at":"2026-10-14T00:00:00Z","precipitation_amount":0,"cloud_cover":0.5}],"hourly":[{"at":"2026-10-12T13:00:00Z","cloud_cover":0.5}]}`), &weather)
	projected := ProjectAt(weather, time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC))
	*projected.TodayForecast.PrecipitationAmount = 1
	*projected.TodayForecast.NightPrecipitationAmount = 1
	*projected.Forecasts[0].PrecipitationAmount = 1
	if *weather.TodayForecast.PrecipitationAmount != .23 || *weather.TodayForecast.NightPrecipitationAmount != 0 || *weather.Forecasts[0].PrecipitationAmount != 0 {
		t.Fatal("display projection wrote forecast evidence back into the cache")
	}
}
