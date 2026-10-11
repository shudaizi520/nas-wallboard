package dashboard

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/model"
)

func reminderWeather(now time.Time) model.WeatherStatus {
	uv := 8.0
	good := model.WeatherComponent{UpdatedAt: now, ExpiresAt: now.Add(time.Hour)}
	hourly := good
	weather := model.WeatherStatus{Enabled: true, Units: "metric", Temperature: 32, Condition: "晴", ConditionCode: "100", UVIndex: &uv, RainSummary: "未来2小时无明显降雨", Components: &model.WeatherComponents{Current: good, Rain: good, Alerts: good, Forecast: good, Hourly: &hourly}}
	for i := 1; i <= 12; i++ {
		hour := model.WeatherHour{At: now.Add(time.Duration(i) * time.Hour), Temperature: 30, Condition: "晴", ConditionCode: "100"}
		if i == 6 || i == 7 {
			hour.Condition = "小雨"
			hour.ConditionCode = "305"
			hour.PrecipitationProbability = 0.65
		}
		weather.Hourly = append(weather.Hourly, hour)
	}
	return weather
}

func buildReminderActivity(t *testing.T, weather model.WeatherStatus, now time.Time) (Activity, []struct {
	Text   string
	Pinned bool
	Parts  []DetailPart `json:"detail_parts"`
}) {
	t.Helper()
	builder, err := New(config.DashboardConfig{Activities: []config.ActivityConfig{{Type: config.ActivityTypeWeather}}})
	if err != nil {
		t.Fatal(err)
	}
	activity := builder.Build(model.Snapshot{Weather: model.Module[model.WeatherStatus]{Data: weather}}, now).Activities[0]
	encoded, _ := json.Marshal(activity)
	var decoded struct {
		Messages []struct {
			Text   string
			Pinned bool
			Parts  []DetailPart `json:"detail_parts"`
		} `json:"weather_reminders"`
	}
	_ = json.Unmarshal(encoded, &decoded)
	return activity, decoded.Messages
}

func TestWeatherRemindersSeparateUVAndLaterRainWithoutLosingLegacyDetails(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("", 8*3600))
	activity, messages := buildReminderActivity(t, reminderWeather(now), now)
	if len(messages) != 2 || messages[0].Text != "紫外线很强 · 指数8" || messages[0].Pinned || messages[1].Text != "18–19时可能有雨" || messages[1].Pinned {
		t.Fatalf("ordinary reminders not separated: %#v", messages)
	}
	if activity.Detail != "紫外线很强 · 指数8\n18–19时可能有雨" {
		t.Fatalf("legacy details lost: %q", activity.Detail)
	}
}

func TestWeatherRemindersKeepOfficialColorsAndPinWarnings(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("", 8*3600))
	weather := reminderWeather(now)
	weather.Warnings = []model.WeatherWarning{{Title: "暴雨橙色预警", Color: "orange"}}
	_, messages := buildReminderActivity(t, weather, now)
	if len(messages) != 2 || !messages[0].Pinned || !strings.Contains(messages[0].Text, "暴雨橙色预警") {
		t.Fatalf("official warning can rotate away: %#v", messages)
	}
	colored := false
	for _, p := range messages[0].Parts {
		colored = colored || p.Color == "orange"
	}
	if !colored {
		t.Fatalf("official warning color lost: %#v", messages)
	}
}

func TestWeatherRemindersPinNearRainAndRemoveExpiredHourlyMessage(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("", 8*3600))
	weather := reminderWeather(now)
	weather.RainSummary = "约半小时后可能有雨"
	_, messages := buildReminderActivity(t, weather, now)
	if len(messages) == 0 || !messages[0].Pinned || !strings.Contains(messages[0].Text, "约半小时后可能有雨") {
		t.Fatalf("near rain disappeared into rotation: %#v", messages)
	}
	weather.RainSummary = "未来2小时无明显降雨"
	weather.Components.Hourly.Stale = true
	_, messages = buildReminderActivity(t, weather, now)
	if len(messages) != 2 || messages[1].Text != "小时预报暂不可用" || !messages[1].Pinned {
		t.Fatalf("stale message not pinned: %#v", messages)
	}
	for _, m := range messages {
		if strings.Contains(m.Text, "18–19") {
			t.Fatalf("expired rain still queued: %#v", messages)
		}
	}
}

func TestWeatherRemindersOnlyDeduplicateOverlappingRainWindows(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("", 8*3600))
	for _, tc := range []struct {
		name string
		wet  []int
		want string
	}{
		{"separate evening rain", []int{6, 7}, "18–19时可能有雨"},
		{"overlapping near rain", []int{1, 2}, ""},
		{"near and independent evening bands", []int{1, 6, 7}, "18–19时可能有雨"},
		{"rain continuing beyond minute coverage", []int{1, 2, 3, 4, 5, 6, 7}, "13–19时可能有雨"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			weather := reminderWeather(now)
			weather.UVIndex = nil
			weather.RainSummary = "约半小时后可能有雨"
			for i := range weather.Hourly {
				weather.Hourly[i].Condition, weather.Hourly[i].ConditionCode = "晴", "100"
				weather.Hourly[i].PrecipitationProbability = 0
			}
			for _, offset := range tc.wet {
				h := &weather.Hourly[offset-1]
				h.Condition, h.ConditionCode, h.PrecipitationProbability = "小雨", "305", 0.65
			}
			activity, messages := buildReminderActivity(t, weather, now)
			if len(messages) == 0 || !messages[0].Pinned || messages[0].Text != "约半小时后可能有雨" {
				t.Fatalf("near rain was hidden: %#v", messages)
			}
			if tc.want == "" {
				if len(messages) != 1 {
					t.Fatalf("overlapping rain repeated: %#v", messages)
				}
			} else if len(messages) != 2 || messages[1].Pinned || messages[1].Text != tc.want {
				t.Fatalf("independent later window lost: detail=%q messages=%#v", activity.Detail, messages)
			}
		})
	}
}
