package dashboard

import (
	"example.com/nas-wallboard/internal/model"
	"strings"
	"testing"
	"time"
)

func TestPartialWeatherDisplaysSuccessfulCurrentAndForecast(t *testing.T) {
	stamp := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	module := model.Module[model.WeatherStatus]{Error: "collection_failed", Partial: true, Data: model.WeatherStatus{
		Enabled: true, Temperature: 29, Condition: "多云", ConditionCode: "101", RainSummary: "约半小时后可能有雨",
		Forecasts:  []model.WeatherForecast{{Condition: "阴", TemperatureMin: 20, TemperatureMax: 30}},
		Components: &model.WeatherComponents{Current: model.WeatherComponent{UpdatedAt: stamp}, Rain: model.WeatherComponent{Error: "unavailable"}, Alerts: model.WeatherComponent{Error: "unavailable"}, Forecast: model.WeatherComponent{UpdatedAt: stamp}},
	}}
	got := weatherActivities(module)
	if len(got) != 2 || !strings.Contains(got[0].Value, "29°") || strings.Contains(got[0].Detail, "半小时") || !strings.Contains(got[0].Detail, "暂不可用") {
		t.Fatalf("partial weather = %#v", got)
	}
}

func TestExpiredWeatherRiskDoesNotSurviveSuccessfulSibling(t *testing.T) {
	stamp := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	module := model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Temperature: 29, Condition: "多云", RainSummary: "约半小时后可能有雨", Warnings: []model.WeatherWarning{{Title: "暴雨预警", Color: "orange"}}, Components: &model.WeatherComponents{Current: model.WeatherComponent{UpdatedAt: stamp}, Rain: model.WeatherComponent{UpdatedAt: stamp, Stale: true}, Alerts: model.WeatherComponent{UpdatedAt: stamp, Stale: true}}}}
	got, _ := weatherActivity(module)
	if strings.Contains(got.Detail, "半小时") || strings.Contains(got.Detail, "暴雨预警") || !strings.Contains(got.Detail, "暂不可用") {
		t.Fatalf("expired weather risk = %#v", got)
	}
}

func TestTemporaryAlertFailureRetainsUnexpiredOfficialWarningAsLastKnown(t *testing.T) {
	stamp := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	module := model.Module[model.WeatherStatus]{Error: "unavailable", Partial: true, Data: model.WeatherStatus{Enabled: true, Temperature: 29, Condition: "多云", RainSummary: "未来2小时无明显降雨", Warnings: []model.WeatherWarning{{Title: "暴雨橙色预警", Color: "orange"}}, Components: &model.WeatherComponents{Current: model.WeatherComponent{UpdatedAt: stamp}, Rain: model.WeatherComponent{UpdatedAt: stamp}, Alerts: model.WeatherComponent{UpdatedAt: stamp, Error: "unavailable"}}}}
	got, _ := weatherActivity(module)
	if !strings.Contains(got.Detail, "上次预警") || !strings.Contains(got.Detail, "暴雨橙色预警") || !strings.Contains(got.Detail, "更新失败") {
		t.Fatalf("unexpired official warning suppressed: %#v", got)
	}
}

func TestOfficialWarningStillShowsWhenCurrentWeatherAloneFails(t *testing.T) {
	stamp := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	module := model.Module[model.WeatherStatus]{Error: "unavailable", Partial: true, Data: model.WeatherStatus{Enabled: true, Warnings: []model.WeatherWarning{{Title: "暴雨橙色预警", Color: "orange"}}, Components: &model.WeatherComponents{Current: model.WeatherComponent{Error: "unavailable"}, Alerts: model.WeatherComponent{UpdatedAt: stamp}}}}
	got, _ := weatherActivity(module)
	if got.Value != "不可用" || !strings.Contains(got.Detail, "暴雨橙色预警") || !strings.Contains(got.Detail, "天气暂不可用") {
		t.Fatalf("current failure hides successful official warning: %#v", got)
	}
}

func TestImperialWeatherConvertsAllDisplayedTemperaturesOnly(t *testing.T) {
	feel := 30.0
	humidity := 50.0
	wind := 7
	module := model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Units: "imperial", Temperature: 20, FeelsLike: &feel, HumidityPercent: &humidity, WindScale: &wind, Condition: "晴", RainSummary: "未来2小时无明显降雨", Forecasts: []model.WeatherForecast{{Condition: "晴", TemperatureMin: 10, TemperatureMax: 30}}}}
	got := weatherActivities(module)
	if len(got) != 2 || got[0].Value != "68°F · 晴" || !strings.Contains(got[1].Detail, "50°F–86°F") || !strings.Contains(got[0].Detail, "强风7级") {
		t.Fatalf("imperial weather = %#v", got)
	}
	module.Data.WindScale = nil
	got = weatherActivities(module)
	if !strings.Contains(got[0].Detail, "体感86°F") {
		t.Fatalf("imperial comfort = %#v", got)
	}
}
