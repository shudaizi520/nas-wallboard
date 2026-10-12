package dashboard

import (
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

// Regression: valid upstream phenomena must not be re-predicted from numeric
// fields. Covers the real Wednesday 305 / 0% / 0.29mm and legacy missing fields.
func TestUpstreamWeatherCodeControlsDailyAndHourlyIcons(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, code, condition, icon string
		p, amount                   float64
		missing                     bool
	}{
		{"rain without extra fields", "305", "小雨", "weather-rain", 0, 0, true},
		{"rain with zero amount and low chance", "305", "小雨", "weather-rain", .02, 0, false},
		{"actual Wednesday response", "305", "小雨", "weather-rain", 0, .29, false},
		{"cloudy with rain guidance", "101", "多云", "weather-cloudy", .65, .23, false},
		{"few clouds with high chance", "102", "少云", "weather-sunny", .65, 0, false},
		{"overcast is not inferred rain", "104", "阴", "weather-overcast", .65, .23, false},
		{"thunderstorm keeps specific symbol", "302", "雷阵雨", "weather-storm", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := calmTurningWeather(now)
			w.Hourly[3].ConditionCode, w.Hourly[3].Condition = tc.code, tc.condition
			w.Hourly[3].PrecipitationProbability, w.Hourly[3].PrecipitationMissing = tc.p, tc.missing
			w.Hourly[3].PrecipitationAmount = tc.amount
			w.Forecasts = []model.WeatherForecast{{StartAt: now.Add(24 * time.Hour), EndAt: now.Add(48 * time.Hour), ConditionCode: tc.code, Condition: tc.condition, TemperatureMin: 24, TemperatureMax: 33, PrecipitationProbability: tc.p, PrecipitationMissing: tc.missing, PrecipitationAmount: &tc.amount}}
			a, _ := buildReminderActivity(t, w, now)
			d := (*a.ForecastDays)[0]
			if d.Icon != tc.icon || d.Condition != tc.condition || strings.Contains(d.Detail, "待核实") || strings.Contains(d.Detail, "按云量") {
				t.Fatalf("upstream daily phenomenon rewritten: %+v", d)
			}
			found := false
			for _, h := range *a.ForecastHours {
				if h.Label == "16时" {
					found = true
					if h.Icon != tc.icon || !strings.HasPrefix(h.Condition, tc.condition) {
						t.Fatalf("upstream hourly symbol rewritten: %+v", h)
					}
				}
			}
			if !found {
				t.Fatal("hour slot lost")
			}
		})
	}
}
