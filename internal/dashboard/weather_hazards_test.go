package dashboard

import (
	"example.com/nas-wallboard/internal/model"
	"strings"
	"testing"
)

func TestWindIsRetainedAlongsideRainAndOfficialWarning(t *testing.T) {
	force := 7
	for _, rain := range []string{"局部可能有雨", "约半小时后可能有雨", "正在降雨"} {
		v := model.Module[model.WeatherStatus]{Data: model.WeatherStatus{Enabled: true, Temperature: 28, Condition: "多云", RainSummary: rain, WindScale: &force}}
		a, _ := weatherActivity(v)
		if !strings.Contains(a.Detail, "强风7级") || !strings.Contains(a.Detail, rain) {
			t.Errorf("wind/rain hidden: %+v", a)
		}
		v.Data.Warnings = []model.WeatherWarning{{Title: "暴雨橙色预警", Color: "orange"}}
		a, _ = weatherActivity(v)
		if !strings.HasPrefix(a.Detail, "暴雨橙色预警") || !strings.Contains(a.Detail, "强风7级") {
			t.Errorf("warning/wind hidden: %+v", a)
		}
	}
}
