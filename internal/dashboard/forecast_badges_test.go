package dashboard

import (
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func TestForecastProbabilityBadgesHideDryLowRiskButRetainWetKnownProbability(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		code, condition string
		p               float64
		missing         bool
		want            string
	}{
		{"100", "晴", .03, false, ""}, {"101", "多云", .10, false, ""}, {"100", "晴", .19, false, ""},
		{"100", "晴", .20, false, "20%"}, {"305", "小雨", .07, false, "7%"}, {"302", "雷阵雨", .03, false, "3%"},
		{"100", "晴", .1949, false, ""}, {"100", "晴", .195, false, "20%"},
		{"305", "小雨", .1949, false, "19%"}, {"305", "小雨", .195, false, "20%"},
		{"100", "晴", .001, false, ""}, {"305", "小雨", .001, false, "<1%"},
		{"400", "小雪", .10, false, "10%"}, {"305", "小雨", 0, true, ""}, {"305", "小雨", 0, false, ""},
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
		if tc.p > .0 && !tc.missing && !strings.Contains((*a.ForecastDays)[0].Detail, forecastProbability(tc.p, false)) {
			t.Fatal("low probability source detail erased")
		}
		if w.Hourly[3].PrecipitationProbability != tc.p || w.Forecasts[0].PrecipitationProbability != tc.p {
			t.Fatal("source probability changed")
		}
	}
}

func TestTodayBadgeUsesFinalMainIconNotDiscardedDailyRainSymbol(t *testing.T) {
	now := time.Date(2026, 10, 12, 15, 45, 0, 0, time.FixedZone("weather", 8*3600))
	w := todaySummaryWeather(now)
	w.TodayForecast.PrecipitationProbability = .07
	a, _ := buildReminderActivity(t, w, now)
	if d := (*a.ForecastDays)[0]; d.Icon != "weather-cloudy" || d.Rain != "" || !strings.Contains(d.Detail, "7%") {
		t.Fatalf("dry summary still displays low rain badge: %+v", d)
	}
	for i := range w.Hourly {
		w.Hourly[i].Condition, w.Hourly[i].ConditionCode = "小雨", "305"
	}
	a, _ = buildReminderActivity(t, w, now)
	if d := (*a.ForecastDays)[0]; d.Icon != "weather-rain" || d.Rain != "7%" {
		t.Fatalf("wet final summary lost known probability: %+v", d)
	}
}
