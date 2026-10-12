package dashboard

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func TestQuietWeatherLeavesReminderSpaceEmpty(t *testing.T) {
	feel, humidity := 35.0, 60.0
	for _, rain := range []string{"未来2小时无明显降雨", "暂未见明显降雨", ""} {
		got, _ := weatherActivity(model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Temperature: 32, Condition: "晴", FeelsLike: &feel, HumidityPercent: &humidity, RainSummary: rain}})
		if got.Detail != "" {
			t.Errorf("normal weather should leave reminder space empty, got %q", got.Detail)
		}
	}
}

func TestOfficialWarningKeepsNearRainReminder(t *testing.T) {
	got, _ := weatherActivity(model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Temperature: 28, Condition: "阴", RainSummary: "约半小时后可能有雨", Warnings: []model.WeatherWarning{{Title: "暴雨橙色预警", Color: "orange"}}}})
	if !strings.HasPrefix(got.Detail, "暴雨橙色预警") || !strings.Contains(got.Detail, "约半小时后可能有雨") {
		t.Fatalf("important reminder lost: %#v", got)
	}
}

func TestWeatherProvidesFiveDayStripAndNoFakeDryProbability(t *testing.T) {
	var data model.WeatherStatus
	_ = json.Unmarshal([]byte(`{"enabled":true,"temperature":32,"condition":"晴","today_forecast":{"start_at":"2026-10-10T00:00:00+08:00","end_at":"2026-10-11T00:00:00+08:00","condition":"晴","condition_code":"100","temperature_min":24,"temperature_max":32,"precipitation_probability":0},"forecasts":[{"start_at":"2026-10-11T00:00:00+08:00","end_at":"2026-10-12T00:00:00+08:00","condition":"多云","condition_code":"101","temperature_min":25,"temperature_max":33,"precipitation_probability":0.006},{"start_at":"2026-10-12T00:00:00+08:00","end_at":"2026-10-13T00:00:00+08:00","condition":"阵雨","condition_code":"300","temperature_min":23,"temperature_max":29,"precipitation_probability":0.65},{"start_at":"2026-10-13T00:00:00+08:00","end_at":"2026-10-14T00:00:00+08:00","condition":"小雨","condition_code":"305","temperature_min":22,"temperature_max":28,"precipitation_probability":0.8},{"start_at":"2026-10-14T00:00:00+08:00","end_at":"2026-10-15T00:00:00+08:00","condition":"多云","condition_code":"101","temperature_min":23,"temperature_max":30,"precipitation_probability":0.2}]}`), &data)
	rows := weatherActivities(model.Module[model.WeatherStatus]{Data: data})
	encoded, _ := json.Marshal(rows[0])
	var got struct {
		Days []struct{ Label, Icon, High, Low, Rain string } `json:"forecast_days"`
	}
	_ = json.Unmarshal(encoded, &got)
	if len(got.Days) != 5 {
		t.Fatalf("five-day strip absent: %s", encoded)
	}
	if got.Days[0].Label != "今天" || got.Days[0].Rain != "" || got.Days[0].High != "32°" || got.Days[1].Label != "周日" || got.Days[1].Rain != "" || got.Days[2].Rain != "65%" {
		t.Fatalf("misleading forecast strip: %s", encoded)
	}
}

func TestFutureHazardIsCompactButDetailsKeepWindAndUV(t *testing.T) {
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.FixedZone("", 8*3600))
	got := weatherForecastDays(model.WeatherStatus{Forecasts: []model.WeatherForecast{{StartAt: start, EndAt: start.AddDate(0, 0, 1), Condition: "多云", TemperatureMin: 24, TemperatureMax: 30, WindScale: 6, UVIndex: 8}}})
	if len(got) != 1 || got[0].Hazard != "强风/强紫外线" || !strings.Contains(got[0].Detail, "强风6级") || !strings.Contains(got[0].Detail, "紫外线8") {
		t.Fatalf("future warning too verbose or detail lost: %#v", got)
	}
}

func TestHazardWithoutRainSummaryHasNoLeadingSeparator(t *testing.T) {
	wind := 7
	got, _ := weatherActivity(model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Condition: "晴", WindScale: &wind}})
	if got.Detail != "强风7级" {
		t.Fatalf("empty rain leaves broken hazard text: %q", got.Detail)
	}
}

func TestLegacyForecastDoesNotInventProbabilityForMissingSourceData(t *testing.T) {
	for _, test := range []struct {
		probability float64
		missing     bool
		detail      string
	}{
		{0, true, "24°–32°"}, {0, false, "24°–32°"}, {0.006, false, "24°–32°"}, {2, false, "24°–32°"}, {0.65, false, "24°–32° · 雨65%"},
	} {
		got := weatherActivities(model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Condition: "晴", Forecasts: []model.WeatherForecast{{Condition: "晴", TemperatureMin: 24, TemperatureMax: 32, PrecipitationProbability: test.probability, PrecipitationMissing: test.missing}}}})
		if len(got) != 2 || got[1].Detail != test.detail {
			t.Errorf("legacy probability display=%#v; want %q", got, test.detail)
		}
	}
}

func TestOrdinaryNightRainStaysInNightDetailNotUnderDayIcon(t *testing.T) {
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.FixedZone("", 8*3600))
	got := weatherForecastDays(model.WeatherStatus{Forecasts: []model.WeatherForecast{{StartAt: start, EndAt: start.AddDate(0, 0, 1), Condition: "晴", ConditionCode: "100", TemperatureMin: 24, TemperatureMax: 32, NightCondition: "阵雨", NightConditionCode: "300", NightPrecipitationKnown: true, NightPrecipitationProbability: 0.65}}})
	if len(got) != 1 || got[0].Icon != "weather-sunny" || got[0].Rain != "" || !strings.Contains(got[0].Detail, "白天晴") || !strings.Contains(got[0].Detail, "夜间阵雨") || !strings.Contains(got[0].Detail, "夜间降水65%") {
		t.Fatalf("night rain hidden by dry daytime: %#v", got)
	}
}

func TestExpiredForecastDoesNotProduceStructuredDays(t *testing.T) {
	start := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	good := model.WeatherComponent{UpdatedAt: start}
	got := weatherActivities(model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Temperature: 32, Condition: "晴", TodayForecast: &model.WeatherForecast{StartAt: start, EndAt: start.AddDate(0, 0, 1), Condition: "晴", TemperatureMin: 24, TemperatureMax: 32}, Components: &model.WeatherComponents{Current: good, Rain: good, Alerts: good, Forecast: model.WeatherComponent{UpdatedAt: start, Stale: true}}}})
	if len(got) != 1 || got[0].ForecastDays != nil || !strings.Contains(got[0].Detail, "日预报暂不可用") {
		t.Fatalf("expired daily cache leaked into strip: %#v", got)
	}
}
