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

func TestFiveDayForecastUsesOneRequestAndRetainsToday(t *testing.T) {
	now := time.Date(2026, 10, 10, 8, 0, 0, 0, time.FixedZone("", 8*3600))
	requests := 0
	client, err := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Query().Get("days") != "5" {
			t.Errorf("forecast coverage query = %q; want five days in one request", r.URL.Query().Get("days"))
		}
		days := ""
		for i := 0; i < 5; i++ {
			if i > 0 {
				days += ","
			}
			start := now.AddDate(0, 0, i)
			days += fmt.Sprintf(`{"forecastStartTime":"%s","forecastEndTime":"%s","daytime":{"condition":{"text":"晴","code":"100"},"precipitation":{"probability":0}},"temperatureMin":{"value":24},"temperatureMax":{"value":32}}`, start.Format("2006-01-02T00:00Z07:00"), start.AddDate(0, 0, 1).Format("2006-01-02T00:00Z07:00"))
		}
		return response(200, `{"days":[`+days+`]}`), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return now }
	var got model.WeatherStatus
	if err := client.readForecast(context.Background(), &got); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got)
	var decoded struct {
		Today *model.WeatherForecast `json:"today_forecast"`
	}
	_ = json.Unmarshal(encoded, &decoded)
	if requests != 1 || decoded.Today == nil || len(got.Forecasts) != 4 {
		t.Fatalf("five-day coverage missing: today=%v future=%d requests=%d", decoded.Today, len(got.Forecasts), requests)
	}
}

func TestTodayForecastExpiresAndFutureBecomesTodayAtLocalMidnight(t *testing.T) {
	now := time.Date(2026, 10, 10, 16, 1, 0, 0, time.UTC) // Sunday 00:01 in China.
	var value model.WeatherStatus
	_ = json.Unmarshal([]byte(`{"today_forecast":{"start_at":"2026-10-10T00:00:00+08:00","end_at":"2026-10-11T00:00:00+08:00","condition":"晴"},"forecasts":[{"start_at":"2026-10-11T00:00:00+08:00","end_at":"2026-10-12T00:00:00+08:00","condition":"小雨"},{"start_at":"2026-10-12T00:00:00+08:00","end_at":"2026-10-13T00:00:00+08:00","condition":"阴"}]}`), &value)
	got := ProjectAt(value, now)
	encoded, _ := json.Marshal(got)
	var decoded struct {
		Today *model.WeatherForecast `json:"today_forecast"`
	}
	_ = json.Unmarshal(encoded, &decoded)
	if decoded.Today == nil || decoded.Today.Condition != "小雨" || len(got.Forecasts) != 1 || got.Forecasts[0].Condition != "阴" {
		t.Fatalf("local midnight rollover = %s", encoded)
	}
}

func TestTodayOnlyForecastExpiresWithoutRenewingSourceFreshness(t *testing.T) {
	start := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	value := model.WeatherStatus{TodayForecast: &model.WeatherForecast{StartAt: start, EndAt: end, Condition: "晴"}, Components: &model.WeatherComponents{Forecast: model.WeatherComponent{UpdatedAt: start}}}
	got := ProjectAt(value, end)
	if got.TodayForecast != nil || !got.Components.Forecast.Stale || !got.Components.Forecast.UpdatedAt.Equal(start) {
		t.Fatalf("expired today-only forecast stayed fresh: %#v", got)
	}
}

func TestProjectedTodayCannotMutateCachedForecast(t *testing.T) {
	now := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	value := model.WeatherStatus{TodayForecast: &model.WeatherForecast{StartAt: now.Add(-10 * time.Hour), EndAt: now.Add(14 * time.Hour), TemperatureMax: 32, Condition: "晴"}}
	got := ProjectAt(value, now)
	got.TodayForecast.TemperatureMax = 999
	if value.TodayForecast.TemperatureMax != 32 {
		t.Fatal("projected today aliases cached forecast")
	}
}

func TestDailyForecastRetainsNightRainAndValidWindWithoutInventingMissingValues(t *testing.T) {
	now := time.Date(2026, 10, 10, 10, 0, 0, 0, time.FixedZone("", 8*3600))
	client, err := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `{"days":[{"forecastStartTime":"2026-10-10T00:00:00+08:00","forecastEndTime":"2026-10-11T00:00:00+08:00","temperatureMin":{"value":24},"temperatureMax":{"value":32},"uvIndexMax":8,"daytime":{"condition":{"text":"少云","code":"102"},"wind":{"scale":2},"windGustMax":{"value":100,"unit":"km/h"}},"nighttime":{"condition":{"text":"阵雨","code":"300"},"precipitation":{"probability":0.65},"wind":{"scale":6},"windGustMax":{"value":18,"unit":"m/s"}}},{"forecastStartTime":"2026-10-11T00:00:00+08:00","forecastEndTime":"2026-10-12T00:00:00+08:00","daytime":{"condition":{"text":"晴","code":"100"}},"temperatureMax":{"value":32}}]}`), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return now }
	var got model.WeatherStatus
	if err := client.readForecast(context.Background(), &got); err != nil {
		t.Fatal(err)
	}
	day := got.TodayForecast
	if day == nil || !day.PrecipitationMissing || !day.NightPrecipitationKnown || day.NightPrecipitationProbability != 0.65 || day.NightCondition != "阵雨" || day.WindScale != 6 || day.WindGustMetersPerSecond != 18 || day.UVIndex != 8 || len(got.Forecasts) != 0 {
		t.Fatalf("night rain/unit/missing temperature contract: %#v", got)
	}
}
