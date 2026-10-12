package dashboard

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func TestForecastProbabilityBadgesUseRawThresholdForEveryWeatherClass(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		code, condition string
		p               float64
		missing         bool
		want            string
	}{
		{"100", "晴", .03, false, ""}, {"101", "多云", .10, false, ""}, {"100", "晴", .19, false, ""},
		{"100", "晴", .20, false, ""}, {"305", "小雨", .07, false, ""}, {"302", "雷阵雨", .03, false, ""},
		{"100", "晴", .1949, false, ""}, {"100", "晴", .195, false, ""},
		{"305", "小雨", .1949, false, ""}, {"305", "小雨", .195, false, ""},
		{"100", "晴", .001, false, ""}, {"305", "小雨", .001, false, ""},
		{"400", "小雪", .10, false, ""}, {"305", "小雨", 0, true, ""}, {"305", "小雨", 0, false, ""},
		{"305", "小雨", .20, false, ""}, {"305", "小雨", .1999, false, ""},
		{"305", "小雨", .2001, false, "20%"}, {"100", "晴", .21, false, "21%"},
		{"305", "小雨", .65, false, "65%"}, {"302", "雷阵雨", .65, false, "65%"},
		{"304", "雷阵雨伴有冰雹", .02, false, ""}, {"313", "冻雨", .02, false, ""},
		{"404", "雨夹雪", .02, false, ""}, {"403", "暴雪", .02, false, ""},
		{"305", "小雨", .65, true, ""}, {"305", "小雨", -1, false, ""},
		{"305", "小雨", 2, false, ""}, {"305", "小雨", math.NaN(), false, ""},
		{"305", "小雨", math.Inf(1), false, ""},
	} {
		w := calmTurningWeather(now)
		w.Hourly[3].Condition, w.Hourly[3].ConditionCode = tc.condition, tc.code
		w.Hourly[3].PrecipitationProbability, w.Hourly[3].PrecipitationMissing = tc.p, tc.missing
		start := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
		w.Forecasts = []model.WeatherForecast{{StartAt: start, EndAt: start.AddDate(0, 0, 1), Condition: tc.condition, ConditionCode: tc.code, TemperatureMin: 24, TemperatureMax: 32, PrecipitationProbability: tc.p, PrecipitationMissing: tc.missing}}
		a, _ := buildReminderActivity(t, w, now)
		if a.ForecastDays == nil || (*a.ForecastDays)[0].Rain != tc.want {
			t.Fatalf("code %s p=%v daily badge=%+v want=%q", tc.code, tc.p, a.ForecastDays, tc.want)
		}
		found := false
		for _, h := range *a.ForecastHours {
			if h.Label == "16时" {
				found = true
				if h.Rain != tc.want {
					t.Fatalf("code %s p=%v hourly badge=%q want=%q", tc.code, tc.p, h.Rain, tc.want)
				}
			}
		}
		if !found {
			t.Fatal("event/anchor absent")
		}
		if tc.p > .0 && tc.p <= 1 && !tc.missing && !strings.Contains((*a.ForecastDays)[0].Detail, forecastProbability(tc.p, false)) {
			t.Fatal("low probability source detail erased")
		}
		if !math.IsNaN(tc.p) && (w.Hourly[3].PrecipitationProbability != tc.p || w.Forecasts[0].PrecipitationProbability != tc.p) {
			t.Fatal("source probability changed")
		}
	}
}

func TestTodayHourlySummaryDoesNotReuseDailyProbability(t *testing.T) {
	now := time.Date(2026, 10, 12, 15, 45, 0, 0, time.FixedZone("weather", 8*3600))
	w := todaySummaryWeather(now)
	w.TodayForecast.PrecipitationProbability = .65
	a, _ := buildReminderActivity(t, w, now)
	if d := (*a.ForecastDays)[0]; d.Icon != "weather-cloudy" || d.Rain != "" || !strings.Contains(d.Detail, "65%") {
		t.Fatalf("remaining-hours summary reused full-period probability: %+v", d)
	}
	for i := range w.Hourly {
		w.Hourly[i].Condition, w.Hourly[i].ConditionCode = "小雨", "305"
		w.Hourly[i].PrecipitationAmount = .23
	}
	a, _ = buildReminderActivity(t, w, now)
	if d := (*a.ForecastDays)[0]; d.Icon != "weather-rain" || d.Rain != "" || !strings.Contains(d.Detail, "65%") {
		t.Fatalf("wet remaining-hours summary reused daily probability: %+v", d)
	}
}

func TestDailyIconAndProbabilityComeFromTheSamePeriod(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, dayCode, nightCode, icon, badge string
		dayP, nightP                          float64
		dayMissing, nightMissing              bool
	}{
		{"light rain cannot borrow night probability", "305", "102", "weather-rain", "", 0, .02, false, false},
		{"day rain with higher night probability", "305", "305", "weather-rain", "35%", .35, .80, false, false},
		{"night probability cannot fill missing daytime", "305", "102", "weather-rain", "", 0, .65, true, false},
		{"ordinary night rain stays in night detail", "100", "305", "weather-sunny", "", 0, .65, false, false},
		{"night storm uses night probability", "100", "302", "weather-storm", "35%", .80, .35, false, false},
		{"night storm missing probability cannot borrow day", "100", "302", "weather-storm", "", .80, 0, false, true},
		{"night storm low probability keeps risk but no badge", "100", "302", "weather-storm", "", .80, .02, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := calmTurningWeather(now)
			start := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
			w.Forecasts = []model.WeatherForecast{{StartAt: start, EndAt: start.Add(24 * time.Hour), Condition: "白天原始描述", ConditionCode: tc.dayCode, TemperatureMin: 24, TemperatureMax: 32, PrecipitationProbability: tc.dayP, PrecipitationMissing: tc.dayMissing, NightCondition: "夜间原始描述", NightConditionCode: tc.nightCode, NightPrecipitationProbability: tc.nightP, NightPrecipitationKnown: !tc.nightMissing}}
			original := w.Forecasts[0]
			a, _ := buildReminderActivity(t, w, now)
			d := (*a.ForecastDays)[0]
			if d.Icon != tc.icon || d.Rain != tc.badge {
				t.Fatalf("mixed periods: %+v, want %s %q", d, tc.icon, tc.badge)
			}
			if !strings.Contains(d.Detail, "白天原始描述") || !strings.Contains(d.Detail, "夜间原始描述") || !reflect.DeepEqual(original, w.Forecasts[0]) {
				t.Fatal("source day/night data was overwritten")
			}
		})
	}
}

func TestTodayProtectedDailyRiskRetainsItsOwnPeriodProbability(t *testing.T) {
	now := time.Date(2026, 10, 12, 15, 45, 0, 0, time.FixedZone("weather", 8*3600))
	w := todaySummaryWeather(now)
	w.TodayForecast.Condition, w.TodayForecast.ConditionCode = "中雨", "306"
	w.TodayForecast.PrecipitationProbability = .35
	w.TodayForecast.NightCondition, w.TodayForecast.NightConditionCode = "多云", "101"
	w.TodayForecast.NightPrecipitationKnown, w.TodayForecast.NightPrecipitationProbability = true, .80
	a, _ := buildReminderActivity(t, w, now)
	if d := (*a.ForecastDays)[0]; d.Icon != "weather-rain" || d.Rain != "35%" {
		t.Fatalf("protected day risk borrowed unrelated night probability: %+v", d)
	}
}

func TestLowProbabilityAloneDoesNotCreateRainReminderOrIcon(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, p := range []float64{.02, .07, .1999, .20} {
		w := calmTurningWeather(now)
		for i := range w.Hourly {
			w.Hourly[i].PrecipitationProbability = p
		}
		a, _ := buildReminderActivity(t, w, now)
		if strings.Contains(a.Detail, "可能有雨") {
			t.Fatalf("probability %v fabricated rain reminder: %s", p, a.Detail)
		}
		for _, h := range *a.ForecastHours {
			if h.Icon != "weather-sunny" || h.Rain != "" {
				t.Fatalf("probability %v fabricated precipitation: %+v", p, h)
			}
		}
	}
}
