package dashboard

import (
	"math"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

// Catches using one short precipitation hour as today's principal condition,
// suppressing real risks, or replacing a daily forecast with incomplete cache.
func todaySummaryWeather(now time.Time) model.WeatherStatus {
	weather := reminderWeather(now)
	weather.Condition, weather.ConditionCode, weather.Temperature = "多云", "101", 33
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weather.TodayForecast = &model.WeatherForecast{StartAt: start, EndAt: start.AddDate(0, 0, 1), Condition: "小雨", ConditionCode: "305", NightCondition: "晴", NightConditionCode: "100", TemperatureMin: 23.6, TemperatureMax: 33.74, NightPrecipitationKnown: true}
	weather.Forecasts = []model.WeatherForecast{{StartAt: start.AddDate(0, 0, 1), EndAt: start.AddDate(0, 0, 2), Condition: "小雨", ConditionCode: "305", TemperatureMin: 25, TemperatureMax: 33, PrecipitationProbability: .5}}
	weather.Hourly = nil
	for i := 1; i <= 24; i++ {
		at := now.Truncate(time.Hour).Add(time.Duration(i) * time.Hour)
		weather.Hourly = append(weather.Hourly, model.WeatherHour{At: at, Condition: "多云", ConditionCode: "101", Temperature: 30})
	}
	return weather
}

func TestTodayMainWeatherKeepsBriefRainInHoursAndReminders(t *testing.T) {
	now := time.Date(2026, 10, 11, 15, 45, 0, 0, time.FixedZone("weather", 8*3600))
	weather := todaySummaryWeather(now)
	// Real conflict: cloudy hourly condition, 0% source probability, .23 mm
	// predicted precipitation. It is a forecast, not a record of observed rain.
	weather.Hourly[1].PrecipitationAmount, weather.Hourly[1].PrecipitationType = .23, "rain"
	activity, messages := buildReminderActivity(t, weather, now)
	if activity.Value != "33° · 多云" || activity.Icon != "weather-cloudy" {
		t.Fatalf("current observation changed: %+v", activity)
	}
	if activity.ForecastDays == nil || len(*activity.ForecastDays) != 2 {
		t.Fatal("daily forecast lost")
	}
	today, next := (*activity.ForecastDays)[0], (*activity.ForecastDays)[1]
	if today.Icon != "weather-cloudy" || today.Condition != "多云" || today.Rain != "" || today.Low != "24°" || today.High != "34°" {
		t.Fatalf("brief rain still controls whole-day icon or changes source range/probability: %+v", today)
	}
	if !strings.Contains(today.Detail, "白天小雨") || !strings.Contains(today.Detail, "剩余时段主要多云") || !strings.Contains(today.Detail, "小时预报") {
		t.Fatalf("summary provenance or original forecast lost: %+v", today)
	}
	if next.Icon != "weather-rain" || next.Rain != "50%" || next.Condition != "小雨" {
		t.Fatalf("future daily forecast changed: %+v", next)
	}
	foundHour, foundReminder := false, false
	for _, hour := range *activity.ForecastHours {
		if hour.Label == "17时" {
			foundHour = hour.Icon == "weather-rain" && hour.Rain == "" && strings.Contains(hour.Condition, "可能有雨")
		}
	}
	for _, message := range messages {
		foundReminder = foundReminder || message.Text == "17时可能有雨"
	}
	if !foundHour || !foundReminder {
		t.Fatalf("brief rain risk hidden: %+v, %+v", activity.ForecastHours, messages)
	}
	if weather.TodayForecast.ConditionCode != "305" || weather.Hourly[1].PrecipitationAmount != .23 || weather.Hourly[1].PrecipitationProbability != 0 {
		t.Fatal("cached source mutated")
	}
}

func TestTodayMainWeatherRiskAndCoverageBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 11, 15, 45, 0, 0, time.FixedZone("weather", 8*3600))
	for _, tc := range []struct {
		name   string
		change func(*model.WeatherStatus)
		icon   string
	}{
		{"all cloudy", func(w *model.WeatherStatus) {}, "weather-cloudy"},
		{"two short rainy hours", func(w *model.WeatherStatus) {
			for i := 1; i <= 2; i++ {
				w.Hourly[i].Condition, w.Hourly[i].ConditionCode = "小雨", "305"
			}
		}, "weather-cloudy"},
		{"three consecutive rainy hours", func(w *model.WeatherStatus) {
			w.TodayForecast.Condition, w.TodayForecast.ConditionCode = "多云", "101"
			for i := 1; i <= 3; i++ {
				w.Hourly[i].Condition, w.Hourly[i].ConditionCode = "小雨", "305"
			}
		}, "weather-rain"},
		{"rain in half the remaining hours", func(w *model.WeatherStatus) {
			w.TodayForecast.Condition, w.TodayForecast.ConditionCode = "晴", "100"
			for _, i := range []int{0, 2, 4, 6} {
				w.Hourly[i].PrecipitationProbability = .65
			}
		}, "weather-rain"},
		{"isolated thunderstorm", func(w *model.WeatherStatus) { w.Hourly[1].Condition, w.Hourly[1].ConditionCode = "雷阵雨", "302" }, "weather-storm"},
		{"isolated heavy rain", func(w *model.WeatherStatus) {
			w.TodayForecast.Condition, w.TodayForecast.ConditionCode = "晴", "100"
			w.Hourly[1].Condition, w.Hourly[1].ConditionCode = "大雨", "307"
		}, "weather-rain"},
		{"substantial amount", func(w *model.WeatherStatus) {
			w.TodayForecast.Condition, w.TodayForecast.ConditionCode = "晴", "100"
			w.Hourly[1].PrecipitationAmount = 3
		}, "weather-rain"},
		{"isolated snow", func(w *model.WeatherStatus) { w.Hourly[1].Condition, w.Hourly[1].ConditionCode = "小雪", "400" }, "weather-snow"},
		{"daily thunderstorm remains conservative", func(w *model.WeatherStatus) {
			w.TodayForecast.Condition, w.TodayForecast.ConditionCode = "雷阵雨", "302"
		}, "weather-storm"},
		{"daily heavy rain remains conservative", func(w *model.WeatherStatus) {
			w.TodayForecast.Condition, w.TodayForecast.ConditionCode = "大雨", "307"
		}, "weather-rain"},
		{"stale hourly", func(w *model.WeatherStatus) { w.Components.Hourly.Stale = true }, "weather-rain"},
		{"hourly error", func(w *model.WeatherStatus) { w.Components.Hourly.Error = "unavailable" }, "weather-rain"},
		{"expired hourly even without stale flag", func(w *model.WeatherStatus) { w.Components.Hourly.ExpiresAt = now }, "weather-rain"},
		{"no hourly component", func(w *model.WeatherStatus) { w.Components.Hourly = nil }, "weather-rain"},
		{"no component metadata", func(w *model.WeatherStatus) { w.Components = nil }, "weather-rain"},
		{"hourly ends before midnight", func(w *model.WeatherStatus) { w.Hourly = w.Hourly[:6] }, "weather-rain"},
		{"hourly starts too late", func(w *model.WeatherStatus) { w.Hourly = w.Hourly[2:] }, "weather-rain"},
		{"gap in hourly coverage", func(w *model.WeatherStatus) { w.Hourly = append(w.Hourly[:3], w.Hourly[4:]...) }, "weather-rain"},
		{"invalid temperature", func(w *model.WeatherStatus) { w.Hourly[2].Temperature = math.NaN() }, "weather-rain"},
		{"unknown condition", func(w *model.WeatherStatus) { w.Hourly[2].ConditionCode = "bad" }, "weather-rain"},
		{"duplicate samples do not vote twice", func(w *model.WeatherStatus) { w.Hourly = append(w.Hourly, w.Hourly[2]) }, "weather-rain"},
		{"tomorrow storm cannot control today", func(w *model.WeatherStatus) { w.Hourly[8].Condition, w.Hourly[8].ConditionCode = "雷阵雨", "302" }, "weather-cloudy"},
		{"dry majority uses existing sun icon", func(w *model.WeatherStatus) {
			for i := 2; i < 8; i++ {
				w.Hourly[i].Condition, w.Hourly[i].ConditionCode = "晴", "100"
			}
		}, "weather-sunny"},
		{"tied dry types prefer nearest", func(w *model.WeatherStatus) {
			for i := 4; i < 8; i++ {
				w.Hourly[i].Condition, w.Hourly[i].ConditionCode = "晴", "100"
			}
		}, "weather-cloudy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := todaySummaryWeather(now)
			tc.change(&w)
			activity, _ := buildReminderActivity(t, w, now.UTC())
			if activity.ForecastDays == nil || (*activity.ForecastDays)[0].Icon != tc.icon {
				t.Fatalf("main condition=%+v; want %s", activity.ForecastDays, tc.icon)
			}
		})
	}
}

func TestTodayMainWeatherDoesNotKeepAnEndedRainHour(t *testing.T) {
	now := time.Date(2026, 10, 11, 18, 15, 0, 0, time.FixedZone("weather", 8*3600))
	w := todaySummaryWeather(now)
	w.Hourly = append(w.Hourly, model.WeatherHour{At: now.Add(-75 * time.Minute), Condition: "小雨", ConditionCode: "305", Temperature: 30})
	activity, _ := buildReminderActivity(t, w, now)
	if (*activity.ForecastDays)[0].Icon != "weather-cloudy" || strings.Contains(activity.Detail, "17时可能有雨") {
		t.Fatalf("past risk controls remaining-day summary: %+v", activity)
	}
}

func TestTodayMainWeatherKeepsFullDayRangeAndProbability(t *testing.T) {
	now := time.Date(2026, 10, 11, 15, 45, 0, 0, time.FixedZone("weather", 8*3600))
	w := todaySummaryWeather(now)
	w.Units = "imperial"
	w.TodayForecast.PrecipitationProbability = .65
	activity, _ := buildReminderActivity(t, w, now)
	today := (*activity.ForecastDays)[0]
	if today.Icon != "weather-cloudy" || today.Rain != "65%" || today.Low != "74°F" || today.High != "93°F" {
		t.Fatalf("summary invented probability or narrowed full-day range: %+v", today)
	}
}

func TestTodayMainWeatherLocalMidnightAndYearBoundary(t *testing.T) {
	zone := time.FixedZone("weather", 8*3600)
	for _, now := range []time.Time{time.Date(2026, 12, 31, 22, 15, 0, 0, zone), time.Date(2027, 1, 1, 0, 15, 0, 0, zone)} {
		w := todaySummaryWeather(now)
		activity, _ := buildReminderActivity(t, w, now.UTC())
		if (*activity.ForecastDays)[0].Icon != "weather-cloudy" || (*activity.ForecastDays)[0].Date != now.Format("2006-01-02") {
			t.Fatalf("NAS/UTC date or year rollover changed local today: %+v", activity.ForecastDays)
		}
	}
	// An unrolled old TodayForecast must never be resummarized as a new day.
	now := time.Date(2027, 1, 1, 0, 15, 0, 0, zone)
	w := todaySummaryWeather(now.Add(-24 * time.Hour))
	activity, _ := buildReminderActivity(t, w, now)
	if (*activity.ForecastDays)[0].Icon != "weather-rain" {
		t.Fatal("expired local day received a new summary")
	}
}

func TestTodayMainWeatherDoesNotLetNightProbabilityHideDayStorm(t *testing.T) {
	now := time.Date(2026, 10, 11, 15, 45, 0, 0, time.FixedZone("weather", 8*3600))
	w := todaySummaryWeather(now)
	w.TodayForecast.Condition, w.TodayForecast.ConditionCode = "雷阵雨", "302"
	w.TodayForecast.NightCondition, w.TodayForecast.NightConditionCode = "多云", "101"
	w.TodayForecast.NightPrecipitationProbability = .65
	activity, _ := buildReminderActivity(t, w, now)
	if (*activity.ForecastDays)[0].Icon != "weather-storm" || (*activity.ForecastDays)[0].Condition != "雷阵雨" {
		t.Fatalf("larger night probability hid daily storm: %+v", activity.ForecastDays)
	}
}

func TestTodayMainWeatherDescriptionMatchesMostCommonCondition(t *testing.T) {
	now := time.Date(2026, 10, 11, 15, 45, 0, 0, time.FixedZone("weather", 8*3600))
	w := todaySummaryWeather(now)
	w.Hourly[0].Condition, w.Hourly[0].ConditionCode = "阴", "104"
	activity, _ := buildReminderActivity(t, w, now)
	today := (*activity.ForecastDays)[0]
	if today.Icon != "weather-cloudy" || today.Condition != "多云" || strings.Contains(today.Detail, "主要阴") {
		t.Fatalf("one overcast hour overrides seven cloudy hours: %+v", today)
	}
}

func TestTodayMainWeatherKeepsRainOutsideHourlyStripHorizon(t *testing.T) {
	now := time.Date(2026, 10, 11, 6, 15, 0, 0, time.FixedZone("weather", 8*3600))
	w := todaySummaryWeather(now)
	// A 20:00 shower is beyond the existing 12-hour strip/reminder horizon.
	w.Hourly[13].Condition, w.Hourly[13].ConditionCode = "小雨", "305"
	activity, _ := buildReminderActivity(t, w, now)
	if (*activity.ForecastDays)[0].Icon != "weather-rain" {
		t.Fatalf("rain risk hidden in both daily and hourly displays: %+v", activity)
	}
}

func TestTodayMainWeatherIncludesCurrentLocalHourInFractionalOffset(t *testing.T) {
	now := time.Date(2026, 10, 11, 15, 45, 0, 0, time.FixedZone("weather", 5*3600+1800))
	w := todaySummaryWeather(now)
	w.Hourly = nil
	start := time.Date(2026, 10, 11, 15, 0, 0, 0, now.Location())
	for i := 0; i < 24; i++ {
		w.Hourly = append(w.Hourly, model.WeatherHour{At: start.Add(time.Duration(i) * time.Hour), Condition: "多云", ConditionCode: "101", Temperature: 30})
	}
	w.Hourly[0].Condition, w.Hourly[0].ConditionCode = "雷阵雨", "302"
	activity, _ := buildReminderActivity(t, w, now.UTC())
	if (*activity.ForecastDays)[0].Icon != "weather-storm" {
		t.Fatalf("UTC truncation discarded ongoing local-hour storm: %+v", activity.ForecastDays)
	}
}

func TestTodayMainWeatherHourlyStormCanUpgradeDailyHeavyRain(t *testing.T) {
	now := time.Date(2026, 10, 11, 15, 45, 0, 0, time.FixedZone("weather", 8*3600))
	w := todaySummaryWeather(now)
	w.TodayForecast.Condition, w.TodayForecast.ConditionCode = "大雨", "307"
	w.Hourly[1].Condition, w.Hourly[1].ConditionCode = "雷阵雨", "302"
	activity, _ := buildReminderActivity(t, w, now)
	if (*activity.ForecastDays)[0].Icon != "weather-storm" {
		t.Fatalf("daily rain floor prevented stronger hourly storm: %+v", activity.ForecastDays)
	}
}

func TestTodayMainWeatherRiskDetailsDoNotInventHourlyConsensus(t *testing.T) {
	now := time.Date(2026, 10, 11, 15, 45, 0, 0, time.FixedZone("weather", 8*3600))
	w := todaySummaryWeather(now)
	w.TodayForecast.Condition, w.TodayForecast.ConditionCode = "大雨", "307"
	activity, _ := buildReminderActivity(t, w, now)
	detail := (*activity.ForecastDays)[0].Detail
	if !strings.Contains(detail, "保留日预报大雨风险") || strings.Contains(detail, "主要大雨（小时预报）") {
		t.Fatalf("daily risk floor presented as hourly consensus: %q", detail)
	}
	w = todaySummaryWeather(now)
	w.Hourly[1].Condition, w.Hourly[1].ConditionCode = "雷阵雨", "302"
	activity, _ = buildReminderActivity(t, w, now)
	detail = (*activity.ForecastDays)[0].Detail
	if !strings.Contains(detail, "需留意雷阵雨（小时预报）") || strings.Contains(detail, "主要雷阵雨") {
		t.Fatalf("one strong hour presented as principal weather: %q", detail)
	}
}
