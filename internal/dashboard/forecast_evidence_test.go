package dashboard

import (
	"strings"
	"testing"
	"time"
)

func TestTodaySummaryKeepsRainAmountDetailsExplicitlyTimeScoped(t *testing.T) {
	now := time.Date(2026, 10, 12, 15, 45, 0, 0, time.FixedZone("", 8*3600))
	w := todaySummaryWeather(now)
	nightAmount := .45
	w.TodayForecast.NightPrecipitationAmount = &nightAmount
	a, _ := buildReminderActivity(t, w, now)
	d := (*a.ForecastDays)[0]
	if d.Icon != "weather-cloudy" || !strings.Contains(d.Detail, "白天预报降水量0.23mm") || !strings.Contains(d.Detail, "夜间预报降水量0.45mm") || strings.Contains(d.Detail, "图标对应时段") {
		t.Fatalf("hourly summary misattributes daily rain amount: %+v", d)
	}
}

func TestGuidanceDoesNotRewriteSourceWeatherIcon(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ kind, phrase string }{{"ice", "冰粒或冻雨"}, {"mixed", "混合降水"}} {
		w := calmTurningWeather(now)
		w.Hourly[3].ConditionCode, w.Hourly[3].Condition = "101", "多云"
		w.Hourly[3].PrecipitationType, w.Hourly[3].PrecipitationProbability = tc.kind, .65
		a, _ := buildReminderActivity(t, w, now)
		if a.Detail != "16时可能有"+tc.phrase {
			t.Fatalf("generic %s guidance lost: %+v", tc.kind, a)
		}
		found := false
		for _, h := range *a.ForecastHours {
			if h.Label == "16时" {
				found = true
				if h.Icon != "weather-cloudy" || !strings.Contains(h.Condition, "多云 · 可能有"+tc.phrase) {
					t.Fatalf("guidance invented a source phenomenon: %+v", h)
				}
			}
		}
		if !found {
			t.Fatal("guided hour not selected")
		}
	}
}
