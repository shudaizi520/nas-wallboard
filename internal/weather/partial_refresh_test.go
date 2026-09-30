package weather

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRefreshRetainsSuccessfulComponentsWhenAnotherFails(t *testing.T) {
	for _, failed := range []string{"current", "minutely", "weatheralert", "daily"} {
		t.Run(failed, func(t *testing.T) {
			counts := map[string]int{}
			var countsMu sync.Mutex
			client, _ := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				countsMu.Lock()
				counts[r.URL.Path]++
				countsMu.Unlock()
				if strings.Contains(r.URL.Path, failed) && (failed != "current" || !strings.Contains(r.URL.Path, "weatheralert")) {
					return response(http.StatusServiceUnavailable, `{}`), nil
				}
				switch {
				case strings.Contains(r.URL.Path, "weatheralert"):
					return response(200, `{"alerts":[{"id":"1","headline":"暴雨预警","color":{"code":"orange"}}]}`), nil
				case strings.Contains(r.URL.Path, "current"):
					return response(200, `{"condition":{"text":"多云","code":"101"},"temperature":{"value":29}}`), nil
				case strings.Contains(r.URL.Path, "daily"):
					return response(200, `{"days":[{}, {"daytime":{"condition":{"text":"阴","code":"104"}},"temperatureMin":{"value":20},"temperatureMax":{"value":30}}]}`), nil
				default:
					return response(200, `{"code":"200","minutely":[{"precip":"0"}]}`), nil
				}
			})})
			now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
			client.now = func() time.Time { return now }
			got, err := client.Refresh(context.Background())
			if err == nil || !got.Enabled || len(counts) != 4 {
				t.Fatalf("partial refresh must keep configured weather and try all sources: enabled=%v requests=%d err=%v", got.Enabled, len(counts), err)
			}
			if failed != "current" && got.Temperature != 29 {
				t.Fatalf("successful current lost: %#v", got)
			}
			if failed != "daily" && len(got.Forecasts) != 1 {
				t.Fatalf("successful forecast lost: %#v", got)
			}
			if failed != "weatheralert" && len(got.Warnings) != 1 {
				t.Fatalf("successful alert lost: %#v", got)
			}
			if failed != "minutely" && got.RainSummary == "" {
				t.Fatalf("successful rain lost: %#v", got)
			}
			now = now.Add(time.Minute)
			_, _ = client.Refresh(context.Background())
			for path, count := range counts {
				if count != 1 {
					t.Fatalf("failed component exceeded existing request cadence: %s=%d", path, count)
				}
			}
		})
	}
}

func TestOneTimeoutDoesNotCancelHealthyWeatherSiblingResults(t *testing.T) {
	client, _ := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "current") && !strings.Contains(r.URL.Path, "weatheralert") {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		switch {
		case strings.Contains(r.URL.Path, "daily"):
			return response(200, `{"days":[{}, {"daytime":{"condition":{"text":"阴","code":"104"}},"temperatureMin":{"value":20},"temperatureMax":{"value":30}}]}`), nil
		case strings.Contains(r.URL.Path, "weatheralert"):
			return response(200, `{"alerts":[{"id":"1","headline":"暴雨预警"}]}`), nil
		default:
			return response(200, `{"code":"200","minutely":[{"precip":"0"}]}`), nil
		}
	})})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	got, err := client.Refresh(ctx)
	if err == nil || len(got.Forecasts) != 1 || len(got.Warnings) != 1 || got.RainSummary == "" {
		t.Fatalf("one timeout starved healthy siblings: %#v %v", got, err)
	}
}

func TestAlreadyCanceledRoundDoesNotAdvanceUnattemptedWeatherCadence(t *testing.T) {
	client, _ := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("request on cancelled context"); return nil, nil })})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := client.Refresh(ctx)
	if err == nil || got.Components == nil || !got.Components.Forecast.AttemptedAt.IsZero() {
		t.Fatalf("unattempted forecast retry suppressed: %#v %v", got, err)
	}
}

func TestInitialFailureStillReturnsConfiguredWeather(t *testing.T) {
	client, _ := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(503, `{}`), nil })})
	got, err := client.Refresh(context.Background())
	if err == nil || !got.Enabled || !client.RefreshedAt().IsZero() {
		t.Fatalf("initial error = %#v %v", got, err)
	}
}
