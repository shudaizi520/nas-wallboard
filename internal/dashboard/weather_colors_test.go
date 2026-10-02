package dashboard

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func TestWeatherWarningColorsAreIndependentOfCurrentValue(t *testing.T) {
	module := model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Temperature: 27, Condition: "晴间多云", Warnings: []model.WeatherWarning{
		{Title: "雷电黄色预警", Color: "yellow", Severity: "moderate"},
		{Title: "暴雨橙色预警", Color: "orange", Severity: "severe"},
	}}}
	activity, _ := weatherActivity(module)
	encoded, _ := json.Marshal(activity)
	var payload struct {
		ValueTone string `json:"value_tone"`
		Parts     []struct {
			Text  string `json:"text"`
			Color string `json:"color"`
		} `json:"detail_parts"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ValueTone != "neutral" {
		t.Fatalf("warning colors leaked into current value: %s", encoded)
	}
	var texts, colors []string
	for _, part := range payload.Parts {
		texts = append(texts, part.Text)
		if strings.Contains(part.Text, "预警") {
			colors = append(colors, part.Color)
		}
	}
	if strings.Join(texts, "") != "暴雨橙色预警\n雷电黄色预警" || !reflect.DeepEqual(colors, []string{"orange", "yellow"}) {
		t.Fatalf("warning detail lost independent colors: %s", encoded)
	}
}

func TestValidCurrentWeatherWithoutOfficialWarningsKeepsNeutralValue(t *testing.T) {
	for _, wind := range []*int{nil, intPointer(7)} {
		got, _ := weatherActivity(model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Temperature: 27, Condition: "多云", RainSummary: "未来2小时无明显降雨", WindScale: wind}})
		encoded, _ := json.Marshal(got)
		var payload struct {
			ValueTone string `json:"value_tone"`
		}
		json.Unmarshal(encoded, &payload)
		if payload.ValueTone != "neutral" {
			t.Fatalf("valid current value tinted by non-temperature hint: %s", encoded)
		}
	}
}

func TestWeatherWarningPartsPreserveFailureNoticesAndOfficialColors(t *testing.T) {
	stamp := time.Now()
	for _, currentFails := range []bool{false, true} {
		current := model.WeatherComponent{UpdatedAt: stamp}
		if currentFails {
			current.Error = "unavailable"
		}
		module := model.Module[model.WeatherStatus]{Partial: true, Error: "unavailable", Data: model.WeatherStatus{
			Enabled: true, Temperature: 27, Condition: "多云", WindScale: intPointer(7), RainSummary: "未来2小时无明显降雨",
			Warnings:   []model.WeatherWarning{{Title: "暴雨橙色预警", Color: "orange"}},
			Components: &model.WeatherComponents{Current: current, Rain: model.WeatherComponent{UpdatedAt: stamp}, Alerts: model.WeatherComponent{UpdatedAt: stamp, Error: "unavailable"}},
		}}
		got, _ := weatherActivity(module)
		if got.DetailParts == nil {
			t.Fatal("last known warning lost color metadata")
		}
		var full strings.Builder
		for _, part := range *got.DetailParts {
			full.WriteString(part.Text)
		}
		if full.String() != got.Detail || !strings.Contains(full.String(), "上次预警") || !strings.Contains(full.String(), "更新失败") {
			t.Fatalf("notice lost: %#v", got)
		}
		if currentFails && (got.Value != "不可用" || got.ValueTone != "") {
			t.Fatalf("outage hidden: %#v", got)
		}
		if !currentFails && got.ValueTone != "neutral" {
			t.Fatalf("partial notices tinted valid current value: %#v", got)
		}
	}
	for _, tc := range []struct{ color, severity, want string }{
		{" RED ", "", "red"}, {"orange", "extreme", "orange"}, {"yellow", "severe", "yellow"}, {"blue", "", "blue"}, {"purple", "", "purple"}, {"black", "", "black"},
		{"", "extreme", "red"}, {"unknown", "severe", "orange"}, {"", "moderate", "yellow"}, {"", "minor", "blue"}, {"url(secret)", "", ""},
	} {
		got, _ := weatherActivity(model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Temperature: 27, Condition: "多云", Warnings: []model.WeatherWarning{{Title: "暴雨预警", Color: tc.color, Severity: tc.severity}}}})
		if got.DetailParts == nil || (*got.DetailParts)[0].Color != tc.want {
			t.Fatalf("source color %q/%q: %#v", tc.color, tc.severity, got.DetailParts)
		}
	}
	got, _ := weatherActivity(model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Temperature: 27, Condition: "多云", Warnings: []model.WeatherWarning{{Title: "解除暴雨橙色预警", Color: "orange"}}}})
	if got.DetailParts != nil || got.ValueTone != "neutral" {
		t.Fatalf("released warning still colored: %#v", got)
	}
}
