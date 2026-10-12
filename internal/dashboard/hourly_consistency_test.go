package dashboard

import (
	"math"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

// The first fixture is the real NAS combination that exposed the split rules:
// cloudy code101, probability0, predicted rain0.23mm. Tests use the actual
// builder and assert the consumer-facing icon, reminder and probability.
func TestHourlyPrecipitationPresentationMatchesReminder(t *testing.T) {
	now := time.Date(2026, 10, 11, 13, 0, 0, 0, time.FixedZone("", 8*3600))
	for _, tc := range []struct {
		name, code, kind, condition, icon, reminder, rain string
		amount, probability                               float64
		missing                                           bool
	}{
		{name: "real cloudy rain amount with zero probability", code: "101", kind: "rain", condition: "多云", amount: .23, icon: "weather-cloudy", reminder: "17时可能有雨"},
		{name: "cloudy known probability without amount", code: "101", kind: "rain", condition: "多云", probability: .65, icon: "weather-cloudy", reminder: "17时可能有雨", rain: "65%"},
		{name: "missing probability does not invent percent", code: "101", kind: "rain", condition: "多云", amount: .23, missing: true, icon: "weather-cloudy", reminder: "17时可能有雨"},
		{name: "low probability alone is not rain", code: "100", kind: "none", condition: "晴", probability: .07, icon: "weather-sunny"},
		{name: "ordinary rain follows source code", code: "305", kind: "rain", condition: "小雨", probability: .07, icon: "weather-rain", reminder: "17时可能有雨"},
		{name: "storm icon is not reduced to generic rain", code: "302", kind: "rain", condition: "雷阵雨", icon: "weather-storm", reminder: "17时可能有雨"},
		{name: "explicit storm takes priority over conflicting precipitation type", code: "302", kind: "snow", condition: "雷阵雨", amount: .23, icon: "weather-storm", reminder: "17时可能有雨"},
		{name: "explicit rain takes priority over conflicting precipitation type", code: "305", kind: "snow", condition: "小雨", probability: .65, icon: "weather-rain", reminder: "17时可能有雨", rain: "65%"},
		{name: "cloudy snow probability", code: "101", kind: "snow", condition: "多云", probability: .65, icon: "weather-cloudy", reminder: "17时可能有雪", rain: "65%"},
		{name: "cloudy snow amount without percent", code: "101", kind: "snow", condition: "多云", amount: .23, missing: true, icon: "weather-cloudy", reminder: "17时可能有雪"},
		{name: "cold is not snow", code: "901", kind: "none", condition: "冷", icon: "weather-cold"},
		{name: "negative amount is invalid", code: "101", kind: "rain", condition: "多云", amount: -1, icon: "weather-cloudy"},
		{name: "infinite amount is invalid", code: "101", kind: "rain", condition: "多云", amount: math.Inf(1), icon: "weather-cloudy"},
		{name: "nan amount is invalid", code: "101", kind: "rain", condition: "多云", amount: math.NaN(), icon: "weather-cloudy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			weather := reminderWeather(now)
			weather.UVIndex = nil
			for i := range weather.Hourly {
				weather.Hourly[i] = model.WeatherHour{At: now.Add(time.Duration(i+1) * time.Hour), Condition: "晴", ConditionCode: "100", Temperature: 30}
			}
			weather.Hourly[3] = model.WeatherHour{At: now.Add(4 * time.Hour), Condition: tc.condition, ConditionCode: tc.code, Temperature: 31, PrecipitationType: tc.kind, PrecipitationAmount: tc.amount, PrecipitationProbability: tc.probability, PrecipitationMissing: tc.missing}
			activity, _ := buildReminderActivity(t, weather, now)
			if activity.Detail != tc.reminder {
				t.Fatalf("reminder=%q, want %q", activity.Detail, tc.reminder)
			}
			if activity.ForecastHours == nil {
				t.Fatal("hour strip absent")
			}
			found := false
			if activity.Value != "32° · 晴" || activity.Icon != "weather-sunny" {
				t.Fatal("future rain replaced the current header observation")
			}
			for _, hour := range *activity.ForecastHours {
				at, err := time.Parse(time.RFC3339, hour.At)
				if hour.Label == "现在" || err != nil || !at.After(now) {
					t.Fatal("current or past observation still occupies a forecast slot")
				}
				if hour.Label != "17时" {
					continue
				}
				found = true
				if hour.Icon != tc.icon || hour.Rain != tc.rain {
					t.Fatalf("17h icon=%q probability=%q, want %q %q", hour.Icon, hour.Rain, tc.icon, tc.rain)
				}
				if !strings.Contains(hour.Condition, tc.condition) {
					t.Fatalf("original condition lost: %q", hour.Condition)
				}
				if tc.code == "101" && tc.reminder != "" && !strings.Contains(hour.Condition, "可能有") {
					t.Fatalf("precipitation icon contradicts accessible description: %q", hour.Condition)
				}
			}
			if !found {
				t.Fatal("17h anchor or precipitation event is absent from the strip")
			}
		})
	}
}
