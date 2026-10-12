package dashboard

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

// Tests the icons consumers actually receive, not a mirror of the classifier.
func TestUnifiedWeatherClassesAcrossCurrentHoursAndDays(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		codes []string
		icon  string
	}{
		{[]string{"100", "102", " 102 "}, "weather-sunny"},
		{[]string{"101", "103", "151", "153"}, "weather-cloudy"},
		{[]string{"104"}, "weather-overcast"},
		{[]string{"300", "305", "306", "309", "314", "350", "399"}, "weather-rain"},
		{[]string{"301", "307", "308", "310", "311", "312", "315", "316", "317", "318", "351"}, "weather-heavy-rain"},
		{[]string{"302", "303"}, "weather-storm"},
		{[]string{"304"}, "weather-hail"},
		{[]string{"313"}, "weather-freezing-rain"},
		{[]string{"404", "405", "406", "456"}, "weather-sleet"},
		{[]string{"400", "401", "407", "408", "457", "499"}, "weather-snow"},
		{[]string{"402", "403", "409", "410"}, "weather-heavy-snow"},
		{[]string{"500", "501", "509", "510", "514", "515"}, "weather-fog"},
		{[]string{"502", "511", "512", "513"}, "weather-haze"},
		{[]string{"503", "504", "507", "508"}, "weather-dust"},
		{[]string{"900"}, "weather-hot"},
		{[]string{"901"}, "weather-cold"},
		{[]string{"999", "", "bad", "250", "319", "411", "516"}, "weather-unknown"},
	} {
		for _, code := range tc.codes {
			t.Run(code, func(t *testing.T) {
				w := calmTurningWeather(now)
				w.ConditionCode, w.Condition = code, "原始描述"
				w.Hourly[3].ConditionCode, w.Hourly[3].Condition = code, "原始描述"
				start := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
				w.TodayForecast = nil
				w.Forecasts = []model.WeatherForecast{{StartAt: start, EndAt: start.Add(24 * time.Hour), ConditionCode: code, Condition: "原始描述", TemperatureMin: 20, TemperatureMax: 30}}
				if tc.icon == "weather-rain" {
					amount := .23
					w.Hourly[3].PrecipitationAmount = amount
					w.Forecasts[0].PrecipitationAmount = &amount
				}
				a, _ := buildReminderActivity(t, w, now)
				if a.Icon != tc.icon || (*a.ForecastDays)[0].Icon != tc.icon {
					t.Fatalf("%q: current=%s day=%s want=%s", code, a.Icon, (*a.ForecastDays)[0].Icon, tc.icon)
				}
				found := false
				for _, h := range *a.ForecastHours {
					if h.Label == "16时" {
						found = true
						if h.Icon != tc.icon || h.Condition != "原始描述" {
							t.Fatalf("hour icon/description: %+v, want %s", h, tc.icon)
						}
					}
				}
				if !found || w.Hourly[3].ConditionCode != code {
					t.Fatal("selected sample lost or source mutated")
				}
			})
		}
	}
}

func TestDayIconDoesNotFollowSlightlyHigherNightProbability(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ day, night, icon, condition string }{
		{"102", "103", "weather-sunny", "少云"},
		{"305", "102", "weather-rain", "小雨"},
		{"100", "305", "weather-sunny", "晴"},
		{"100", "302", "weather-storm", "雷阵雨"},
		{"100", "307", "weather-heavy-rain", "大雨"},
		{"100", "313", "weather-freezing-rain", "冻雨"},
		{"100", "404", "weather-sleet", "雨夹雪"},
		{"100", "304", "weather-hail", "雷阵雨伴有冰雹"},
	} {
		w := calmTurningWeather(now)
		start := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
		names := map[string]string{"100": "晴", "102": "少云", "103": "晴间多云", "305": "小雨", "302": "雷阵雨", "307": "大雨", "313": "冻雨", "404": "雨夹雪", "304": "雷阵雨伴有冰雹"}
		w.TodayForecast = nil
		w.Forecasts = []model.WeatherForecast{{StartAt: start, EndAt: start.Add(24 * time.Hour), ConditionCode: tc.day, Condition: names[tc.day], NightConditionCode: tc.night, NightCondition: names[tc.night], NightPrecipitationKnown: true, NightPrecipitationProbability: .02, TemperatureMin: 20, TemperatureMax: 30}}
		if tc.day == "305" {
			amount := .23
			w.Forecasts[0].PrecipitationAmount = &amount
		}
		a, _ := buildReminderActivity(t, w, now)
		d := (*a.ForecastDays)[0]
		if d.Icon != tc.icon || d.Condition != tc.condition || !strings.Contains(d.Detail, "夜间降水2%") || !strings.Contains(d.Detail, "白天"+names[tc.day]) {
			t.Fatalf("day=%s night=%s changed by probability or hazard lost: %+v", tc.day, tc.night, d)
		}
		if tc.night == "302" || tc.night == "307" || tc.night == "313" || tc.night == "404" || tc.night == "304" {
			if !strings.Contains(d.Detail, "夜间风险优先展示") || d.Rain != "" {
				t.Fatalf("night hazard origin lost or low probability displayed: %+v", d)
			}
		}
	}
}

func TestUnifiedDryClassesDriveTurningSelection(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, codes := range [][2]string{{"102", "103"}, {"101", "104"}} {
		w := calmTurningWeather(now)
		for i := range w.Hourly {
			w.Hourly[i].ConditionCode = codes[0]
			if i >= 7 {
				w.Hourly[i].ConditionCode = codes[1]
			}
		}
		labels := turningLabels(t, w, now)
		if !containsLabel(labels, "19时") || !containsLabel(labels, "20时") {
			t.Fatalf("different displayed classes %v did not retain turn: %v", codes, labels)
		}
	}
	w := calmTurningWeather(now)
	for i := range w.Hourly {
		w.Hourly[i].ConditionCode = "103"
		if i >= 7 {
			w.Hourly[i].ConditionCode = "101"
		}
	}
	if got := turningLabels(t, w, now); !reflect.DeepEqual(got, []string{"14时", "16时", "18时", "20时", "22时", "次日0时"}) {
		t.Fatalf("same cloudy class invented a turn: %v", got)
	}
}

func TestUnavailableCurrentWeatherDoesNotPretendItIsCloudy(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	w := calmTurningWeather(now)
	w.Components.Current.Error = "unavailable"
	a, _ := buildReminderActivity(t, w, now)
	if a.Icon != "weather-unknown" || a.Value != "不可用" {
		t.Fatalf("unavailable weather masquerades as a phenomenon: %+v", a)
	}
}

func TestNightPrecipitationKeepsItsTypeAndReminderWithoutLowProbabilityBadge(t *testing.T) {
	now := time.Date(2026, 10, 12, 17, 0, 0, 0, time.FixedZone("", 8*3600))
	for _, tc := range []struct{ code, icon, reminder string }{
		{"305", "weather-rain", "可能有雨"}, {"307", "weather-heavy-rain", "可能有雨"},
		{"302", "weather-storm", "可能有雨"}, {"304", "weather-hail", "可能有冰雹"},
		{"313", "weather-freezing-rain", "可能有冻雨"}, {"404", "weather-sleet", "可能有雨夹雪"},
		{"403", "weather-heavy-snow", "可能有雪"},
	} {
		w := calmTurningWeather(now)
		day := solarForecast(t, "2026-10-12")
		w.TodayForecast = &day
		w.Hourly[3].ConditionCode, w.Hourly[3].PrecipitationProbability = tc.code, .07
		if tc.code == "305" {
			w.Hourly[3].PrecipitationAmount = .23
		}
		a, _ := buildReminderActivity(t, w, now)
		found := false
		for _, h := range *a.ForecastHours {
			if h.Label == "21时" {
				found = true
				if h.Icon != tc.icon || h.Rain != "" {
					t.Fatalf("night phenomenon lost or low probability displayed: %+v", h)
				}
			}
		}
		if !found || !strings.Contains(a.Detail, "21时"+tc.reminder) {
			t.Fatalf("night event missing: %+v", a)
		}
	}
}

func TestHourlyLaterEscalationsAreNotCrowdedOutByFirstRain(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	w := calmTurningWeather(now)
	for i := 1; i < 11; i++ {
		w.Hourly[i].ConditionCode, w.Hourly[i].Condition = "305", "小雨"
	}
	w.Hourly[3].WindScale = 7
	w.Hourly[4].ConditionCode, w.Hourly[4].Condition = "302", "雷阵雨"
	w.Hourly[6].ConditionCode, w.Hourly[6].Condition = "307", "大雨"
	w.Hourly[8].ConditionCode, w.Hourly[8].Condition = "313", "冻雨"
	labels := turningLabels(t, w, now)
	for _, label := range []string{"14时", "16时", "17时", "19时", "21时"} {
		if !containsLabel(labels, label) {
			t.Fatalf("risk escalation crowded out: %s in %v", label, labels)
		}
	}
}

func TestTodaySummaryCombinesSunnyAndFewCloudsInsteadOfCountingThemAsCloudy(t *testing.T) {
	now := time.Date(2026, 10, 12, 15, 45, 0, 0, time.FixedZone("", 8*3600))
	w := todaySummaryWeather(now)
	for i, code := range []string{"100", "102", "100", "102", "101", "100", "102", "101"} {
		w.Hourly[i].ConditionCode = code
		w.Hourly[i].Condition = "晴"
		if code == "102" {
			w.Hourly[i].Condition = "少云"
		}
		if code == "101" {
			w.Hourly[i].Condition = "多云"
		}
	}
	a, _ := buildReminderActivity(t, w, now)
	if d := (*a.ForecastDays)[0]; d.Icon != "weather-sunny" || d.Condition != "晴" || !strings.Contains(d.Detail, "白天小雨") {
		t.Fatalf("clear majority misclassified or source erased: %+v", d)
	}
}

func TestHourlySnowGuidanceDoesNotInventTodaySnowIcon(t *testing.T) {
	now := time.Date(2026, 10, 12, 15, 45, 0, 0, time.FixedZone("", 8*3600))
	for _, probability := range []bool{false, true} {
		w := todaySummaryWeather(now)
		w.TodayForecast.Condition, w.TodayForecast.ConditionCode = "多云", "101"
		w.Hourly[1].PrecipitationType = "snow"
		if probability {
			w.Hourly[1].PrecipitationProbability = .65
		} else {
			w.Hourly[1].PrecipitationAmount = .23
		}
		a, _ := buildReminderActivity(t, w, now)
		if d := (*a.ForecastDays)[0]; d.Icon != "weather-cloudy" || d.Condition != "多云" || !strings.Contains(a.Detail, "17时可能有雪") {
			t.Fatalf("snow guidance rewrote source class, probability=%v: %+v", probability, d)
		}
	}
}
