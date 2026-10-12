package dashboard

import (
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func TestUpdatedWarningTitlesKeepCoreTypeSeverityAndPinnedColors(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ title, want string }{
		{"临桂区气象台更新大风蓝色预警信号", "大风蓝色预警信号"},
		{"深圳市气象台继续更新暴雨橙色预警信号", "暴雨橙色预警信号"},
		{"深圳市气象台发布高温黄色预警", "高温黄色预警"},
		{"深圳市气象台将台风蓝色预警更新为黄色预警", "深圳市气象台将台风蓝色预警更新为黄色预警"},
		{"雷电预警更新说明", "雷电预警更新说明"},
		{"深圳市气象台将台风蓝色预警更新：为黄色预警", "深圳市气象台将台风蓝色预警更新：为黄色预警"},
		{"深圳市气象台将暴雨橙色预警更新预警级别为黄色", "深圳市气象台将暴雨橙色预警更新预警级别为黄色"},
		{"深圳市气象台将台风蓝色预警更新黄色预警信号", "深圳市气象台将台风蓝色预警更新黄色预警信号"},
		{"某地气象台更新未知灾种黄色预警信号", "某地气象台更新未知灾种黄色预警信号"},
		{"临桂区气象台更新：大风蓝色预警信号", "：大风蓝色预警信号"},
	} {
		w := calmTurningWeather(now)
		w.Warnings = []model.WeatherWarning{{Title: tc.title, Color: "orange", Severity: "severe"}}
		a, messages := buildReminderActivity(t, w, now)
		if a.Detail != tc.want || a.DetailParts == nil || (*a.DetailParts)[0].Color != "orange" || len(messages) != 1 || !messages[0].Pinned || messages[0].Text != tc.want {
			t.Fatalf("warning simplification lost meaning/color/priority: %+v %#v want %q", a, messages, tc.want)
		}
		if w.Warnings[0].Title != tc.title {
			t.Fatal("raw warning headline mutated")
		}
	}
}
