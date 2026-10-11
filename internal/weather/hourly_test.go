package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
	"go.yaml.in/yaml/v3"
)

func hourlyPayload(now time.Time) string {
	items := ""
	for i := 0; i < 24; i++ {
		if i > 0 {
			items += ","
		}
		items += fmt.Sprintf(`{"forecastTime":%q,"condition":{"text":"晴","code":"100"},"temperature":{"value":30,"unit":"°C"},"precipitation":{"probability":0.07,"amount":{"value":0,"unit":"mm"}},"wind":{"scale":2},"windGust":{"value":4,"unit":"m/s"}}`, now.Add(time.Duration(i)*time.Hour).Format(time.RFC3339))
	}
	return `{"hours":[` + items + `]}`
}

func TestHourlyRefreshAddsOneCachedRequestWithoutChangingSiblingCadence(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("", 8*3600))
	cfg := weatherConfig()
	_ = yaml.Unmarshal([]byte("hourly_enabled: true"), &cfg)
	counts := map[string]int{}
	var mu sync.Mutex
	client, _ := New(cfg, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		counts[r.URL.Path]++
		mu.Unlock()
		switch {
		case strings.Contains(r.URL.Path, "hourly"):
			if r.URL.Query().Get("hours") != "24" || r.URL.Query().Get("localTime") != "true" || r.URL.Query().Get("lang") != "zh" {
				t.Errorf("hourly coverage/local time: %s", r.URL.RawQuery)
			}
			return response(200, hourlyPayload(now)), nil
		case strings.Contains(r.URL.Path, "daily"):
			return response(200, validDailyPayload(now)), nil
		case strings.Contains(r.URL.Path, "weatheralert"):
			return response(200, `{"alerts":[]}`), nil
		case strings.Contains(r.URL.Path, "current"):
			return response(200, `{"condition":{"text":"晴","code":"100"},"temperature":{"value":32}}`), nil
		default:
			return response(200, validRainPayload(now)), nil
		}
	})})
	client.now = func() time.Time { return now }
	got, err := client.Refresh(context.Background())
	encoded, _ := json.Marshal(got)
	var data struct {
		Hourly []struct {
			Temperature float64
			Probability float64 `json:"precipitation_probability"`
		} `json:"hourly"`
	}
	_ = json.Unmarshal(encoded, &data)
	if err != nil || len(data.Hourly) != 24 || counts["/weather/v1/hourly/22.54/114.06"] != 1 {
		t.Fatalf("hourly source absent: samples=%d counts=%v err=%v", len(data.Hourly), counts, err)
	}
	if data.Hourly[0].Temperature != 30 || data.Hourly[0].Probability != 0.07 {
		t.Fatalf("hourly values changed: %s", encoded)
	}
	stamp := client.RefreshedAt()
	now = now.Add(time.Minute)
	_, _ = client.Refresh(context.Background())
	if !client.RefreshedAt().Equal(stamp) {
		t.Fatal("cached tick renewed source timestamp")
	}
	now = now.Add(29 * time.Minute)
	_, _ = client.Refresh(context.Background())
	if counts["/weather/v1/hourly/22.54/114.06"] != 2 || counts["/weather/v1/daily/22.54/114.06"] != 1 || counts["/weather/v1/current/22.54/114.06"] != 2 {
		t.Fatalf("cadence changed: %v", counts)
	}
}

func TestProjectHourlyDropsPastSamplesWithoutMutatingCache(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	var value model.WeatherStatus
	_ = json.Unmarshal([]byte(`{"hourly":[{"at":"2026-10-11T11:00:00Z","condition":"小雨"},{"at":"2026-10-11T12:00:00Z","condition":"晴"}],"components":{"hourly":{"updated_at":"2026-10-11T11:00:00Z"}}}`), &value)
	got := ProjectAt(value, now)
	encoded, _ := json.Marshal(got)
	var decoded struct {
		Hourly []struct{ Condition string } `json:"hourly"`
	}
	_ = json.Unmarshal(encoded, &decoded)
	if len(decoded.Hourly) != 1 || decoded.Hourly[0].Condition != "晴" {
		t.Fatalf("past hours retained: %s", encoded)
	}
	old, _ := json.Marshal(value)
	if !strings.Contains(string(old), "小雨") {
		t.Fatal("projection mutated cached samples")
	}
}
