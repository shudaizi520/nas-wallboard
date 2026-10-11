package dashboard

import (
	"math"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func TestWeatherNegativeTemperaturesRemainValid(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("", 8*3600))
	for _, units := range []string{"metric", "imperial"} {
		t.Run(units, func(t *testing.T) {
			weather := reminderWeather(now)
			weather.Units, weather.Temperature = units, -5
			for i := range weather.Hourly {
				weather.Hourly[i].Temperature = -5
			}
			weather.TodayForecast = &model.WeatherForecast{StartAt: now.Add(-12 * time.Hour), EndAt: now.Add(12 * time.Hour), Condition: "晴", ConditionCode: "100", TemperatureMin: -8, TemperatureMax: 2}
			activity, _ := buildReminderActivity(t, weather, now)
			wantTemperature, wantLow := "-5°", "-8°"
			if units == "imperial" {
				wantTemperature, wantLow = "23°F", "18°F"
			}
			if activity.Value != wantTemperature+" · 晴" {
				t.Fatalf("valid negative current disappeared: %q", activity.Value)
			}
			if activity.ForecastDays == nil || len(*activity.ForecastDays) != 1 || (*activity.ForecastDays)[0].Low != wantLow {
				t.Fatalf("valid negative daily range disappeared: %#v", activity.ForecastDays)
			}
			if activity.ForecastHours == nil || len(*activity.ForecastHours) != 6 {
				t.Fatal("valid negative hourly samples disappeared")
			}
			for _, hour := range *activity.ForecastHours {
				if hour.Temperature != wantTemperature {
					t.Fatalf("negative hourly temperature changed: %+v", hour)
				}
			}
		})
	}
}

func TestWeatherNonFiniteTemperaturesRemainUnavailable(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		weather := reminderWeather(now)
		weather.Temperature = bad
		activity, _ := weatherActivity(model.Module[model.WeatherStatus]{Data: weather})
		if activity.Value != "不可用" {
			t.Fatal("non-finite current rendered as valid")
		}
		weather.TodayForecast = &model.WeatherForecast{StartAt: now.Add(-12 * time.Hour), EndAt: now.Add(12 * time.Hour), Condition: "晴", TemperatureMin: bad, TemperatureMax: 2}
		if len(weatherForecastDays(weather)) != 0 {
			t.Fatal("non-finite daily range rendered as valid")
		}
		for i := range weather.Hourly {
			weather.Hourly[i].Temperature = bad
		}
		activity = Activity{}
		addHourlyWeather(&activity, model.Module[model.WeatherStatus]{Data: weather}, now)
		if activity.ForecastHours != nil {
			t.Fatal("non-finite hourly samples rendered as valid")
		}
	}
}
