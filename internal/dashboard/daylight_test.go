package dashboard

import (
	"encoding/json"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func solarForecast(t *testing.T, date string) model.WeatherForecast {
	t.Helper()
	var day model.WeatherForecast
	if err := json.Unmarshal([]byte(`{"start_at":"`+date+`T00:00:00+08:00","end_at":"`+date+`T23:59:59+08:00","sunrise":"`+date+`T06:13:00+08:00","sunset":"`+date+`T18:02:00+08:00","condition":"晴","condition_code":"100","temperature_min":24,"temperature_max":32}`), &day); err != nil {
		t.Fatal(err)
	}
	return day
}

func TestDaylightIconsUseLocationSunTimesAndBoundaryInstants(t *testing.T) {
	for _, tc := range []struct{ at, want string }{
		{"2026-10-11T06:12:59+08:00", "weather-moon"},
		{"2026-10-11T06:13:00+08:00", "weather-sunny"},
		{"2026-10-11T18:01:59+08:00", "weather-sunny"},
		{"2026-10-11T18:02:00+08:00", "weather-moon"},
		{"2026-10-11T23:30:00+08:00", "weather-moon"},
	} {
		t.Run(tc.at, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tc.at)
			weather := reminderWeather(now)
			day := solarForecast(t, "2026-10-11")
			weather.TodayForecast = &day
			// The NAS's UTC clock must not be mistaken for the location's date.
			activity, _ := buildReminderActivity(t, weather, now.UTC())
			if activity.Icon != tc.want || activity.ForecastHours == nil || (*activity.ForecastHours)[0].Icon != tc.want {
				t.Fatalf("wrong sun boundary/current icon: activity=%s hours=%#v want=%s", activity.Icon, activity.ForecastHours, tc.want)
			}
		})
	}
}

func TestDaylightIconsKeepCrossMidnightSlotsOnTheirOwnCalendar(t *testing.T) {
	now := time.Date(2026, 10, 11, 22, 0, 0, 0, time.FixedZone("", 8*3600))
	weather := reminderWeather(now)
	day := solarForecast(t, "2026-10-11")
	weather.TodayForecast, weather.Forecasts = &day, []model.WeatherForecast{solarForecast(t, "2026-10-12")}
	for i := range weather.Hourly {
		weather.Hourly[i].Condition, weather.Hourly[i].ConditionCode, weather.Hourly[i].PrecipitationProbability = "晴", "100", 0
	}
	activity, _ := buildReminderActivity(t, weather, now.UTC())
	want := []string{"weather-moon", "weather-moon", "weather-moon", "weather-moon", "weather-moon", "weather-sunny"}
	if activity.ForecastHours == nil || len(*activity.ForecastHours) != len(want) {
		t.Fatal("hourly data absent")
	}
	for i, hour := range *activity.ForecastHours {
		if hour.Icon != want[i] {
			t.Errorf("%s: got %s want %s", hour.Label, hour.Icon, want[i])
		}
	}
}

func TestDaylightIconsNeverGuessFromMissingOrStaleSunData(t *testing.T) {
	now := time.Date(2026, 10, 11, 22, 0, 0, 0, time.FixedZone("", 8*3600))
	for _, stale := range []bool{false, true} {
		weather := reminderWeather(now)
		if stale {
			day := solarForecast(t, "2026-10-11")
			weather.TodayForecast = &day
			weather.Components.Forecast.Stale = true
		}
		activity, _ := buildReminderActivity(t, weather, now)
		if activity.Icon != "weather-sunny" {
			t.Fatalf("guessed night from wall clock or stale astronomy: %s", activity.Icon)
		}
	}
}

func TestExplicitClearNightCodeDoesNotRequireGuessedSunTimes(t *testing.T) {
	now := time.Date(2026, 10, 11, 22, 0, 0, 0, time.UTC)
	weather := reminderWeather(now)
	weather.ConditionCode = "150"
	weather.Components.Forecast.Stale = true
	for i := range weather.Hourly {
		weather.Hourly[i].ConditionCode, weather.Hourly[i].PrecipitationProbability = "150", 0
	}
	activity, _ := buildReminderActivity(t, weather, now)
	if activity.Icon != "weather-moon" || activity.ForecastHours == nil {
		t.Fatalf("explicit clear night ignored: %+v", activity)
	}
	for _, hour := range *activity.ForecastHours {
		if hour.Icon != "weather-moon" {
			t.Fatalf("explicit clear-night hourly sample became sun: %+v", hour)
		}
	}
}
