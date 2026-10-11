package weather

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/dashboard"
	"example.com/nas-wallboard/internal/model"
)

// Control only the upstream HTTP boundary; run the real cache and dashboard.
func validationClient(t *testing.T, now *time.Time, current, alerts *string) *Client {
	t.Helper()
	c, err := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(r.URL.Path, "weatheralert"):
			return response(200, *alerts), nil
		case strings.Contains(r.URL.Path, "current"):
			return response(200, *current), nil
		case strings.Contains(r.URL.Path, "daily"):
			return response(200, validDailyPayload(*now)), nil
		default:
			return response(200, validRainPayload(*now)), nil
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return *now }
	return c
}

func validationActivity(t *testing.T, w model.WeatherStatus, now time.Time) dashboard.Activity {
	t.Helper()
	b, err := dashboard.New(config.DashboardConfig{Activities: []config.ActivityConfig{{Type: config.ActivityTypeWeather}}})
	if err != nil {
		t.Fatal(err)
	}
	return b.Build(model.Snapshot{ServerTime: now, Weather: model.Module[model.WeatherStatus]{Data: w}}, now).Activities[0]
}

func TestInvalidCurrentTemperatureDoesNotReplaceSuccessfulObservation(t *testing.T) {
	for _, broken := range []string{
		`{"condition":{"text":"晴","code":"100"}}`,
		`{"condition":{"text":"晴","code":"100"},"temperature":{}}`,
		`{"condition":{"text":"晴","code":"100"},"temperature":{"value":null}}`,
		`{"condition":{"text":"晴","code":"100"},"temperature":null}`,
		`{"condition":{"text":"晴","code":"100"},"temperature":{"value":80,"unit":"°F"}}`,
		`{"condition":{"text":" "},"temperature":{"value":30}}`,
	} {
		t.Run(broken, func(t *testing.T) {
			now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
			current, alerts := `{"condition":{"text":"晴","code":"100"},"temperature":{"value":30,"unit":"°C"}}`, `{"alerts":[]}`
			c := validationClient(t, &now, &current, &alerts)
			first, err := c.Refresh(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			current = broken
			now = now.Add(10 * time.Minute)
			got, err := c.Refresh(context.Background())
			if err == nil || got.Components.Current.Error == "" || !got.Components.Current.UpdatedAt.Equal(first.Components.Current.UpdatedAt) || got.Temperature != 30 {
				t.Fatalf("invalid observation promoted: temperature=%v metadata=%+v error=%v", got.Temperature, got.Components.Current, err)
			}
			if a := validationActivity(t, got, now); a.Value != "不可用" {
				t.Fatalf("missing temperature displayed as real weather: %+v", a)
			}
			if got.Components.Rain.Error != "" || got.Components.Alerts.Error != "" || len(got.Forecasts) != 2 {
				t.Fatal("invalid current response hid healthy siblings")
			}
		})
	}
}

func TestValidZeroAndNegativeCurrentTemperaturesRemainAvailable(t *testing.T) {
	for _, tc := range []struct{ payload, want string }{
		{`{"condition":{"text":"晴","code":"100"},"temperature":{"value":0,"unit":"°C"}}`, "0° · 晴"},
		{`{"condition":{"text":"晴","code":"100"},"temperature":{"value":-5}}`, "-5° · 晴"},
	} {
		now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
		current, alerts := tc.payload, `{"alerts":[]}`
		got, err := validationClient(t, &now, &current, &alerts).Refresh(context.Background())
		if err != nil || validationActivity(t, got, now).Value != tc.want {
			t.Fatalf("valid temperature rejected: %+v %v", got, err)
		}
	}
}

func TestMalformedAlertResponsePreservesLastWarningAndReportsFailure(t *testing.T) {
	for _, broken := range []string{
		`{}`, `{"alerts":null}`, `{"metadata":{"zeroResult":false}}`, `{"alerts":[{}]}`,
		`{"alerts":[{"headline":"暴雨预警","expireTime":"bad-time"}]}`,
		`{"alerts":[{"headline":"暴雨预警","messageType":{"code":"unexpected"}}]}`,
		`{"alerts":[{"headline":"暴雨预警"},{}]}`,
		`{"metadata":{"zeroResult":true},"alerts":[{"headline":"暴雨预警"}]}`,
	} {
		t.Run(broken, func(t *testing.T) {
			now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
			current := `{"condition":{"text":"晴","code":"100"},"temperature":{"value":30}}`
			alerts := `{"alerts":[{"id":"active","headline":"台风红色预警","color":{"code":"red"},"expireTime":"2026-10-11T13:00:00Z"}]}`
			c := validationClient(t, &now, &current, &alerts)
			first, err := c.Refresh(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			alerts = broken
			now = now.Add(5 * time.Minute)
			got, err := c.Refresh(context.Background())
			if err == nil || got.Components.Alerts.Error == "" || !got.Components.Alerts.UpdatedAt.Equal(first.Components.Alerts.UpdatedAt) || len(got.Warnings) != 1 || got.Warnings[0].Title != "台风红色预警" {
				t.Fatalf("malformed response cleared warning: warnings=%+v metadata=%+v error=%v", got.Warnings, got.Components.Alerts, err)
			}
			a := validationActivity(t, got, now)
			if !strings.Contains(a.Detail, "上次预警") || !strings.Contains(a.Detail, "预警更新失败") || a.Value != "30° · 晴" {
				t.Fatalf("failed alert source concealed: %+v", a)
			}
		})
	}
}

func TestExplicitNoAlertResponseCanClearOldWarnings(t *testing.T) {
	for _, empty := range []string{`{"alerts":[]}`, `{"metadata":{"zeroResult":true}}`, `{"metadata":{"zeroResult":true},"alerts":null}`} {
		now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
		current := `{"condition":{"text":"晴","code":"100"},"temperature":{"value":30}}`
		alerts := `{"alerts":[{"id":"active","headline":"台风红色预警","color":{"code":"red"}}]}`
		c := validationClient(t, &now, &current, &alerts)
		if _, err := c.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		alerts = empty
		now = now.Add(5 * time.Minute)
		got, err := c.Refresh(context.Background())
		if err != nil || len(got.Warnings) != 0 || got.Components.Alerts.Error != "" {
			t.Fatalf("legitimate no-alert response rejected: %s %+v %v", empty, got.Warnings, err)
		}
	}
}
