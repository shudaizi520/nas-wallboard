package weather

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

// Payload helpers encode hand-checked dates, independently of production parsing.
func validRainPayload(now time.Time, amounts ...string) string {
	if len(amounts) == 0 {
		amounts = make([]string, 24)
		for i := range amounts {
			amounts[i] = "0"
		}
	}
	points := make([]map[string]string, 0, len(amounts))
	for i, amount := range amounts {
		points = append(points, map[string]string{"fxTime": now.Add(time.Duration(i) * 5 * time.Minute).Format(time.RFC3339), "precip": amount, "type": "rain"})
	}
	body, _ := json.Marshal(map[string]any{"code": "200", "updateTime": now.Format(time.RFC3339), "minutely": points})
	return string(body)
}

func validDailyPayload(now time.Time) string {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	days := make([]map[string]any, 0, 3)
	for i, condition := range []string{"少云", "多云", "阵雨"} {
		date := start.AddDate(0, 0, i)
		days = append(days, map[string]any{"forecastStartTime": date.Format(time.RFC3339), "forecastEndTime": date.AddDate(0, 0, 1).Format(time.RFC3339), "daytime": map[string]any{"condition": map[string]any{"text": condition, "code": []string{"102", "101", "300"}[i]}, "precipitation": map[string]any{"probability": []float64{0.1, 0.35, 0.8}[i]}}, "temperatureMin": map[string]any{"value": []int{25, 26, 25}[i]}, "temperatureMax": map[string]any{"value": []int{34, 33, 31}[i]}})
	}
	body, _ := json.Marshal(map[string]any{"days": days})
	return string(body)
}

func validityClient(t *testing.T, now *time.Time, rain, daily string, requests *atomic.Int32) *Client {
	t.Helper()
	client, err := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if requests != nil {
			requests.Add(1)
		}
		switch {
		case strings.Contains(r.URL.Path, "weatheralert"):
			return response(200, `{"alerts":[]}`), nil
		case strings.Contains(r.URL.Path, "daily"):
			return response(200, daily), nil
		case strings.Contains(r.URL.Path, "minutely"):
			return response(200, rain), nil
		default:
			return response(200, `{"condition":{"text":"晴","code":"100"},"temperature":{"value":29}}`), nil
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return *now }
	return client
}

func TestExpiredSourceRainIsRejectedWithoutHidingHealthyForecast(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	client := validityClient(t, &now, validRainPayload(now.Add(-24*time.Hour), "0.2", "0.2", "0.2"), validDailyPayload(now), nil)
	got, err := client.Refresh(context.Background())
	if err == nil || got.Components.Rain.Error == "" || !got.Components.Rain.UpdatedAt.IsZero() || got.RainSummary != "" || len(got.Forecasts) != 2 {
		t.Fatalf("expired rain promoted or siblings hidden: %#v %v", got, err)
	}
}

func TestPastDailyPeriodsAreNotAcceptedAsFreshFutureRows(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	client := validityClient(t, &now, validRainPayload(now), validDailyPayload(now.AddDate(0, 0, -3)), nil)
	got, err := client.Refresh(context.Background())
	if err == nil || got.Components.Forecast.Error == "" || len(got.Forecasts) != 0 || got.Temperature != 29 {
		t.Fatalf("past days promoted or current hidden: %#v %v", got, err)
	}
}

func TestRainRelativeTimeUsesSourceClockAndCachedTimeProgression(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	var requests atomic.Int32
	client := validityClient(t, &now, validRainPayload(now.Add(-10*time.Minute), "0", "0", "0", "0", "0", "0.2", "0.2", "0.2"), validDailyPayload(now), &requests)
	first, err := client.Refresh(context.Background())
	if err != nil || first.RainSummary != "短时可能有雨" {
		t.Fatalf("15-minute rain result=%q err=%v", first.RainSummary, err)
	}
	now = now.Add(4 * time.Minute)
	cached, err := client.Refresh(context.Background())
	if err != nil || cached.RainSummary != "短时可能有雨" || !cached.Components.Rain.UpdatedAt.Equal(first.Components.Rain.UpdatedAt) || requests.Load() != 4 {
		t.Fatalf("cache advanced timestamp/request count or lost rain: %#v requests=%d err=%v", cached, requests.Load(), err)
	}
}

func TestMalformedRainTimesAmountsAndMissingCoverageAreUnavailable(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	good := validRainPayload(now, "0", "0.2", "0.2", "0.2")
	for name, payload := range map[string]string{
		"missing":       `{"code":"200","minutely":[]}`,
		"bad_time":      strings.Replace(good, "2026-09-30T12:00:00Z", "not-a-time", 1),
		"nan":           strings.Replace(good, `"precip":"0"`, `"precip":"NaN"`, 1),
		"old_update":    strings.Replace(good, `"updateTime":"2026-09-30T12:00:00Z"`, `"updateTime":"2026-09-29T12:00:00Z"`, 1),
		"future_update": strings.Replace(good, `"updateTime":"2026-09-30T12:00:00Z"`, `"updateTime":"2026-10-01T12:00:00Z"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			client := validityClient(t, &now, payload, validDailyPayload(now), nil)
			got, err := client.Refresh(context.Background())
			if err == nil || got.Components.Rain.Error == "" {
				t.Fatalf("invalid source treated as no rain: %#v %v", got, err)
			}
		})
	}
}

func TestValidForecastOffsetAndMinutePrecisionAreAccepted(t *testing.T) {
	zone := time.FixedZone("east", 8*3600)
	now := time.Date(2026, 9, 30, 23, 50, 0, 0, zone)
	rain := strings.ReplaceAll(validRainPayload(now, "0", "0.2", "0.2", "0.2"), ":00+08:00", "+08:00")
	client := validityClient(t, &now, rain, validDailyPayload(now), nil)
	got, err := client.Refresh(context.Background())
	if err != nil || len(got.Forecasts) != 2 || got.Forecasts[0].Condition != "多云" || got.RainSummary != "短时可能有雨" {
		t.Fatalf("valid offset forecast suppressed: %#v %v", got, err)
	}
}

func TestMissingDailyPeriodIsUnavailable(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	broken := strings.ReplaceAll(validDailyPayload(now), "forecastStartTime", "unknownTime")
	got, err := validityClient(t, &now, validRainPayload(now), broken, nil).Refresh(context.Background())
	if err == nil || got.Components.Forecast.Error == "" {
		t.Fatalf("undated forecast accepted: %#v", got)
	}
}

func TestSourceAgeBoundsRainCacheEvenAfterRepeatedHTTP200(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	issued := now.Add(-20 * time.Minute)
	payload := strings.Replace(validRainPayload(now), `"updateTime":"`+now.Format(time.RFC3339)+`"`, `"updateTime":"`+issued.Format(time.RFC3339)+`"`, 1)
	client := validityClient(t, &now, payload, validDailyPayload(now), nil)
	first, err := client.Refresh(context.Background())
	if err != nil || !first.Components.Rain.ExpiresAt.Equal(issued.Add(30*time.Minute)) || !first.Components.Rain.SourceUpdatedAt.Equal(issued) {
		t.Fatalf("source age ignored: %#v %v", first.Components.Rain, err)
	}
	now = now.Add(10 * time.Minute)
	second, err := client.Refresh(context.Background())
	if err == nil || !second.Components.Rain.Stale || second.Components.Rain.Error == "" || !second.Components.Rain.UpdatedAt.Equal(first.Components.Rain.UpdatedAt) {
		t.Fatalf("repeated old HTTP 200 renewed freshness: %#v %v", second.Components.Rain, err)
	}
}

func TestRainProjectionRetainsNormalHintsAndDoesNotClaimMissingCoverage(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	points := make([]model.RainPoint, 24)
	for index := range points {
		points[index].At = now.Add(time.Duration(index) * 5 * time.Minute)
	}
	if got := rainSummaryAt(points, now); got != "未来2小时无明显降雨" {
		t.Fatalf("full dry forecast = %q", got)
	}
	if got := rainSummaryAt(points, now.Add(6*time.Minute)); got != "暂未见明显降雨" {
		t.Fatalf("partial dry coverage overclaimed = %q", got)
	}
	for index := 18; index < 21; index++ {
		points[index].Amount = 0.2
	}
	if got := rainSummaryAt(points, now); got != "未来2小时可能有雨" {
		t.Fatalf("valid 90-minute rain suppressed = %q", got)
	}
	for index := range points {
		points[index].Amount = 0
	}
	for index := 5; index < 8; index++ {
		points[index].Amount = 0.2
	}
	if got := rainSummaryAt(points, now); got != "约半小时后可能有雨" {
		t.Fatalf("25-minute rain = %q", got)
	}
	if got := rainSummaryAt(points, now.Add(6*time.Minute)); got != "短时可能有雨" {
		t.Fatalf("cached relative hint did not progress = %q", got)
	}
}

func TestRainSampleGapsAndInvalidDailyEndsAreRejected(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	brokenRain := strings.Replace(validRainPayload(now), "2026-09-30T12:05:00Z", "2026-09-30T12:06:00Z", 1)
	if got, err := validityClient(t, &now, brokenRain, validDailyPayload(now), nil).Refresh(context.Background()); err == nil || got.Components.Rain.Error == "" {
		t.Fatal("gapped rain samples accepted")
	}
	brokenDaily := strings.Replace(validDailyPayload(now), `"forecastEndTime":"2026-10-01T00:00:00Z"`, `"forecastEndTime":"2026-09-29T00:00:00Z"`, 1)
	if got, err := validityClient(t, &now, validRainPayload(now), brokenDaily, nil).Refresh(context.Background()); err == nil || got.Components.Forecast.Error == "" {
		t.Fatal("invalid daily validity period accepted")
	}
}
