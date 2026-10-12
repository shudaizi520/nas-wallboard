package dashboard

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/model"
)

// These tests catch selection limited to the next five consecutive hours,
// stale-source leakage, and replacing higher-priority warning/minutely text.
func hourlyDashboard(t *testing.T, now time.Time, wet map[int]string, probability float64, current bool, extra string) map[string]any {
	t.Helper()
	items := ""
	for i := 1; i <= 24; i++ {
		if i > 1 {
			items += ","
		}
		condition, code := "晴", "100"
		wind := 0
		if wet[i] != "" {
			condition, code = wet[i], "305"
			if condition == "晴" {
				code = "100"
			}
		}
		pop := 0.0
		if wet[i] != "" {
			pop = probability
		}
		if wet[i] == "强风" {
			condition, code, pop, wind = "多云", "101", 0, 7
		}
		if wet[i] == "小雨强风" {
			condition, code, wind = "小雨", "305", 7
		}
		items += fmt.Sprintf(`{"at":%q,"condition":%q,"condition_code":%q,"temperature":30,"precipitation_probability":%v,"wind_scale":%d}`, now.Add(time.Duration(i)*time.Hour).Format(time.RFC3339), condition, code, pop, wind)
	}
	components := fmt.Sprintf(`"components":{"current":{"updated_at":%q},"rain":{"updated_at":%q},"alerts":{"updated_at":%q},"forecast":{"updated_at":%q},"hourly":{"updated_at":%q%s}},`, now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339), extra)
	if !current {
		components = strings.Replace(components, `"current":{"updated_at":`+fmt.Sprintf("%q", now.Format(time.RFC3339))+`}`, `"current":{"stale":true}`, 1)
	}
	var weather model.WeatherStatus
	if err := json.Unmarshal([]byte(`{"enabled":true,"units":"metric","temperature":32,"condition":"晴","condition_code":"100",`+components+`"hourly":[`+items+`]}`), &weather); err != nil {
		t.Fatal(err)
	}
	builder, err := New(config.DashboardConfig{Activities: []config.ActivityConfig{{Type: config.ActivityTypeWeather}}})
	if err != nil {
		t.Fatal(err)
	}
	view := builder.Build(model.Snapshot{ServerTime: now, Weather: model.Module[model.WeatherStatus]{Data: weather}}, now)
	encoded, _ := json.Marshal(view.Activities[0])
	var got map[string]any
	_ = json.Unmarshal(encoded, &got)
	return got
}

func TestHourlyQuietWeatherUsesAllSixSlotsForFutureWithoutRepeatingCurrent(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("", 8*3600))
	got := hourlyDashboard(t, now, nil, 0, true, "")
	hours, _ := got["forecast_hours"].([]any)
	if len(hours) != 6 {
		t.Fatalf("hourly strip absent: %#v", got)
	}
	want := []string{"14时", "16时", "18时", "20时", "22时", "次日0时"}
	for i, h := range hours {
		row := h.(map[string]any)
		at, err := time.Parse(time.RFC3339, row["at"].(string))
		if row["label"] != want[i] || err != nil || !at.After(now) || at.After(now.Add(12*time.Hour)) || row["temperature"] != "30°" {
			t.Fatalf("quiet labels: %#v", hours)
		}
	}
	if got["value"] != "32° · 晴" || got["icon"] != "weather-sunny" || got["detail"] != "" {
		t.Fatalf("observation or quiet header changed: %#v", got)
	}
}

func TestHourlySelectsEveningRainBeforeItEntersConsecutiveSixHourWindow(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("", 8*3600))
	got := hourlyDashboard(t, now, map[int]string{6: "小雨", 7: "小雨"}, 0.65, true, "")
	hours, _ := got["forecast_hours"].([]any)
	if len(hours) != 6 {
		t.Fatalf("hourly strip absent: %#v", got)
	}
	labels := map[string]map[string]any{}
	for _, h := range hours {
		row := h.(map[string]any)
		labels[row["label"].(string)] = row
	}
	if labels["现在"] != nil {
		t.Fatalf("current observation still occupies a forecast slot: %#v", hours)
	}
	for _, label := range []string{"17时", "18时", "19时"} {
		if labels[label] == nil {
			t.Fatalf("important evening time lost: %#v", hours)
		}
	}
	if labels["18时"]["rain"] != "65%" || !strings.Contains(got["detail"].(string), "18–19时可能有雨") {
		t.Fatalf("rain probability/reminder lost: %#v", got)
	}
}

func TestHourlyDoesNotTurnLowProbabilityIntoRainAnnouncement(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("", 8*3600))
	got := hourlyDashboard(t, now, map[int]string{6: "晴"}, 0.07, true, "")
	// A rainy phenomenon remains useful even at low probability; merely a
	// small probability on a dry phenomenon must not announce likely rain.
	if strings.Contains(got["detail"].(string), "可能有雨") {
		t.Fatalf("7%% exaggerated: %#v", got)
	}
}

func TestHourlyUnavailableDoesNotLeakForecastIntoCurrentReminder(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for _, extra := range []string{`,"stale":true`, `,"error":"unavailable"`} {
		got := hourlyDashboard(t, now, map[int]string{6: "小雨"}, 0.9, true, extra)
		if _, exists := got["forecast_hours"]; exists || strings.Contains(got["detail"].(string), "可能有雨") {
			t.Fatalf("old forecast leaked: %#v", got)
		}
		if !strings.Contains(got["detail"].(string), "小时预报暂不可用") {
			t.Fatalf("unavailability hidden: %#v", got)
		}
	}
}

func TestHourlyCrossMidnightLabelsDistinguishNextDay(t *testing.T) {
	now := time.Date(2026, 10, 11, 22, 0, 0, 0, time.FixedZone("", 8*3600))
	got := hourlyDashboard(t, now, nil, 0, true, "")
	hours, _ := got["forecast_hours"].([]any)
	if len(hours) != 6 || hours[0].(map[string]any)["label"] != "次日0时" {
		t.Fatalf("midnight ambiguity: %#v", got)
	}
}

func TestHourlySparseSamplesDoNotFillMissingForecastsWithCurrentObservation(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	weather := reminderWeather(now)
	weather.UVIndex = nil
	weather.Hourly = []model.WeatherHour{
		{At: now, Condition: "晴", ConditionCode: "100", Temperature: 32},
		{At: now.Add(time.Hour), Condition: "多云", ConditionCode: "101", Temperature: 31},
		{At: now.Add(3 * time.Hour), Condition: "晴", ConditionCode: "100", Temperature: 30},
	}
	activity, _ := buildReminderActivity(t, weather, now)
	if activity.Value != "32° · 晴" || activity.ForecastHours == nil || len(*activity.ForecastHours) != 2 {
		t.Fatalf("sparse forecast padded or current header lost: %+v", activity)
	}
	for i, hour := range *activity.ForecastHours {
		if hour.Label != []string{"13时", "15时"}[i] {
			t.Fatalf("non-future or fabricated forecast: %+v", hour)
		}
	}
}

func TestHourlyDoesNotInventCurrentObservationWhenCurrentSourceFailed(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	got := hourlyDashboard(t, now, nil, 0, false, "")
	hours, _ := got["forecast_hours"].([]any)
	if len(hours) == 0 {
		t.Fatalf("healthy hourly source lost alongside failed current: %#v", got)
	}
	for _, h := range hours {
		if h.(map[string]any)["label"] == "现在" {
			t.Fatalf("failed current masquerades as observation: %#v", got)
		}
	}
}

func TestHourlyEarlierWindDoesNotHideLaterRainContext(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("", 8*3600))
	got := hourlyDashboard(t, now, map[int]string{2: "强风", 6: "小雨", 7: "小雨"}, 0.65, true, "")
	hours, _ := got["forecast_hours"].([]any)
	labels := map[string]bool{}
	for _, h := range hours {
		labels[h.(map[string]any)["label"].(string)] = true
	}
	if !labels["18时"] || !labels["19时"] || !strings.Contains(got["detail"].(string), "18–19时可能有雨") || !strings.Contains(got["detail"].(string), "14时可能有强风") {
		t.Fatalf("earlier wind hid evening rain: %#v", got)
	}
}

func TestHourlySimultaneousRainDoesNotHideStrongWind(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("", 8*3600))
	got := hourlyDashboard(t, now, map[int]string{6: "小雨强风"}, 0.65, true, "")
	if !strings.Contains(got["detail"].(string), "18时可能有雨") || !strings.Contains(got["detail"].(string), "18时可能有强风") {
		t.Fatalf("same-hour rain hides strong wind: %#v", got)
	}
}

func TestHourlyColdCodeDoesNotInventSnow(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	weather := reminderWeather(now)
	weather.UVIndex = nil
	for i := range weather.Hourly {
		weather.Hourly[i] = model.WeatherHour{At: now.Add(time.Duration(i+1) * time.Hour), Condition: "冷", ConditionCode: "901", Temperature: 2}
	}
	activity, _ := buildReminderActivity(t, weather, now)
	if strings.Contains(activity.Detail, "可能有雪") || activity.ForecastHours == nil {
		t.Fatalf("cold mistaken for snowfall: %+v", activity)
	}
	for _, hour := range *activity.ForecastHours {
		if hour.Icon == "weather-snow" {
			t.Fatalf("cold represented as snowy weather: %+v", hour)
		}
	}
}

func TestHourlyPrecipitationKindComesFromWeatherData(t *testing.T) {
	for _, tc := range []struct {
		code, precipitationType string
		probability             float64
		want                    string
	}{
		{"901", "none", 0, ""}, {"400", "snow", 0, "雪"},
		{"305", "rain", 0.07, "雨"}, {"305", "rain", 0.65, "雨"}, {"100", "none", 0.07, ""},
		{"101", "rain", 0.20, ""}, {"101", "rain", 0.21, ""}, {"101", "rain", 0.30, "雨"},
		{"101", "snow", 0.65, "雪"}, {"101", "rain", 0.65, "雨"},
	} {
		hour := model.WeatherHour{ConditionCode: tc.code, PrecipitationType: tc.precipitationType, PrecipitationProbability: tc.probability}
		if got := hourlyEvent(hour); got != tc.want {
			t.Errorf("code=%s type=%s probability=%v: got %q want %q", tc.code, tc.precipitationType, tc.probability, got, tc.want)
		}
	}
}
