package weather

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
)

func TestProbeClassifiesAuthenticationFailure(t *testing.T) {
	client, err := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(http.StatusUnauthorized, `{}`), nil })})
	if err != nil {
		t.Fatal(err)
	}
	if result := client.Probe(context.Background()); result.OK || result.Stage != integration.ProbeStageAuthentication {
		t.Fatalf("Probe = %#v", result)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func weatherConfig() config.WeatherConfig {
	return config.WeatherConfig{Enabled: true, Name: "深圳", Latitude: 22.54, Longitude: 114.06, APIHost: "abc123.qweatherapi.com", APIKey: "weather-secret", Units: "metric"}
}

func TestDisabledWeatherPerformsNoRequest(t *testing.T) {
	var requests atomic.Int32
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return response(http.StatusOK, `{}`), nil
	})}
	client, err := New(config.WeatherConfig{Enabled: false}, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Current(context.Background())
	if err != nil || got.Enabled || requests.Load() != 0 {
		t.Fatalf("Current() = %#v, %v; requests=%d", got, err, requests.Load())
	}
}

func TestQWeatherCombinesCurrentRainAndWarnings(t *testing.T) {
	var requests atomic.Int32
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		if request.URL.Host != "abc123.qweatherapi.com" || request.Header.Get("X-QW-Api-Key") != "weather-secret" {
			t.Errorf("unexpected request host/header: %s / %q", request.URL.Host, request.Header.Get("X-QW-Api-Key"))
		}
		switch request.URL.Path {
		case "/weather/v1/current/22.54/114.06":
			if request.URL.Query().Get("lang") != "zh" {
				t.Errorf("weather query = %s", request.URL.RawQuery)
			}
			return response(http.StatusOK, `{"condition":{"text":"多云","code":"101"},"temperature":{"value":29.4,"unit":"°C"}}`), nil
		case "/v7/minutely/5m":
			if request.URL.Query().Get("location") != "114.06,22.54" {
				t.Errorf("minutely query = %s", request.URL.RawQuery)
			}
			return response(http.StatusOK, `{"code":"200","summary":"40分钟后有雨","minutely":[]}`), nil
		case "/weatheralert/v1/current/22.54/114.06":
			return response(http.StatusOK, `{"metadata":{"zeroResult":false},"alerts":[{"id":"warning-1","eventType":{"name":"暴雨"},"severity":"severe","color":{"code":"orange"},"headline":"深圳市暴雨橙色预警"}]}`), nil
		default:
			t.Fatalf("unexpected weather path %q", request.URL.Path)
			return nil, nil
		}
	})}
	client, err := New(weatherConfig(), httpClient)
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if requests.Load() != 3 || !got.Enabled || got.Name != "深圳" || got.Temperature != 29.4 || got.Condition != "多云" || got.ConditionCode != "101" || got.RainSummary != "40分钟后有雨" || got.Source != "和风天气" {
		t.Fatalf("weather = %#v; requests=%d", got, requests.Load())
	}
	if len(got.Warnings) != 1 || got.Warnings[0].Title != "深圳市暴雨橙色预警" || got.Warnings[0].Severity != "severe" || got.Warnings[0].Color != "orange" {
		t.Fatalf("warnings = %#v", got.Warnings)
	}
}

func TestWeatherRejectsNonSuccessAndInvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
	}{{name: "http", code: http.StatusUnauthorized, body: `{}`}, {name: "json", code: http.StatusOK, body: `{`}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(tt.code, tt.body), nil })})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Current(context.Background()); err == nil || strings.Contains(err.Error(), "weather-secret") {
				t.Fatalf("Current() error = %v", err)
			}
		})
	}
}

func TestWeatherDoesNotFollowCredentialRedirect(t *testing.T) {
	var requests atomic.Int32
	client, err := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		redirect := response(http.StatusTemporaryRedirect, "")
		redirect.Header.Set("Location", "https://attacker.invalid/")
		redirect.Request = request
		return redirect, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.Current(context.Background())
	if requests.Load() != 1 {
		t.Fatalf("credential request followed redirect; requests=%d", requests.Load())
	}
}

func TestWeatherHonorsContextTimeout(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	client, err := New(weatherConfig(), httpClient)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = client.Current(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Current() error = %v, want deadline exceeded", err)
	}
}
