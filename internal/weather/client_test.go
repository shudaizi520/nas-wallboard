package weather

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/model"
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

func TestQWeatherCombinesCurrentRainWarningsAndTwoFutureDays(t *testing.T) {
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
			return response(http.StatusOK, `{
				"condition":{"text":"多云","code":"101"},
				"temperature":{"value":29.4,"unit":"°C"},
				"feelsLike":{"value":34.6,"unit":"°C"},
				"humidity":0.64,
				"wind":{"direction":{"degree":180,"compass":"s"},"speed":{"value":11.3,"unit":"m/s"},"scale":6},
				"windGust":{"value":18.1,"unit":"m/s"},
				"visibility":{"value":29800,"unit":"m"},
				"uvIndex":8
			}`), nil
		case "/weather/v1/daily/22.54/114.06":
			if request.URL.Query().Get("days") != "3" || request.URL.Query().Get("localTime") != "true" || request.URL.Query().Get("lang") != "zh" {
				t.Errorf("daily query = %s", request.URL.RawQuery)
			}
			return response(http.StatusOK, `{"days":[
				{"forecastStartTime":"2026-09-29T00:00+08:00","forecastEndTime":"2026-09-30T00:00+08:00","daytime":{"condition":{"text":"少云","code":"102"},"precipitation":{"probability":0.1}},"temperatureMin":{"value":25},"temperatureMax":{"value":34}},
				{"forecastStartTime":"2026-09-30T00:00+08:00","forecastEndTime":"2026-10-01T00:00+08:00","daytime":{"condition":{"text":"多云","code":"101"},"precipitation":{"probability":0.35}},"temperatureMin":{"value":26},"temperatureMax":{"value":33}},
				{"forecastStartTime":"2026-10-01T00:00+08:00","forecastEndTime":"2026-10-02T00:00+08:00","daytime":{"condition":{"text":"阵雨","code":"300"},"precipitation":{"probability":0.8}},"temperatureMin":{"value":25},"temperatureMax":{"value":31}}
			]}`), nil
		case "/v7/minutely/5m":
			if request.URL.Query().Get("location") != "114.06,22.54" {
				t.Errorf("minutely query = %s", request.URL.RawQuery)
			}
			return response(http.StatusOK, `{
				"code":"200",
				"summary":"40分钟后开始下中雨，70分钟后就停了",
				"updateTime":"2026-09-29T10:00+08:00",
				"minutely":[
					{"fxTime":"2026-09-29T10:00+08:00","precip":"0.0","type":"rain"},
					{"fxTime":"2026-09-29T10:05+08:00","precip":"0.0","type":"rain"},
					{"fxTime":"2026-09-29T10:10+08:00","precip":"0.0","type":"rain"},
					{"fxTime":"2026-09-29T10:15+08:00","precip":"0.0","type":"rain"},
					{"fxTime":"2026-09-29T10:20+08:00","precip":"0.0","type":"rain"},
					{"fxTime":"2026-09-29T10:25+08:00","precip":"0.0","type":"rain"},
					{"fxTime":"2026-09-29T10:30+08:00","precip":"0.0","type":"rain"},
					{"fxTime":"2026-09-29T10:35+08:00","precip":"0.0","type":"rain"},
					{"fxTime":"2026-09-29T10:40+08:00","precip":"0.04","type":"rain"},
					{"fxTime":"2026-09-29T10:45+08:00","precip":"0.04","type":"rain"},
					{"fxTime":"2026-09-29T10:50+08:00","precip":"0.04","type":"rain"}
				]
			}`), nil
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
	client.now = func() time.Time { return time.Date(2026, 9, 29, 2, 0, 0, 0, time.UTC) }
	got, err := client.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if requests.Load() != 4 || !got.Enabled || got.Name != "深圳" || got.Temperature != 29.4 || got.Condition != "多云" || got.ConditionCode != "101" || got.RainSummary != "约半小时后可能有雨" || got.Source != "和风天气" {
		t.Fatalf("weather = %#v; requests=%d", got, requests.Load())
	}
	if got.FeelsLike == nil || *got.FeelsLike != 34.6 || got.HumidityPercent == nil || *got.HumidityPercent != 64 {
		t.Fatalf("current comfort = feels-like %#v, humidity %#v", got.FeelsLike, got.HumidityPercent)
	}
	if got.WindScale == nil || *got.WindScale != 6 || got.WindGustMetersPerSecond == nil || *got.WindGustMetersPerSecond != 18.1 || got.UVIndex == nil || *got.UVIndex != 8 {
		t.Fatalf("current hazards = wind-scale %#v, wind-gust %#v, uv %#v", got.WindScale, got.WindGustMetersPerSecond, got.UVIndex)
	}
	if len(got.Warnings) != 1 || got.Warnings[0].Title != "深圳市暴雨橙色预警" || got.Warnings[0].Severity != "severe" || got.Warnings[0].Color != "orange" {
		t.Fatalf("warnings = %#v", got.Warnings)
	}
	wantForecasts := []model.WeatherForecast{
		{StartAt: time.Date(2026, 9, 30, 0, 0, 0, 0, time.FixedZone("", 8*3600)), EndAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.FixedZone("", 8*3600)), Condition: "多云", ConditionCode: "101", TemperatureMin: 26, TemperatureMax: 33, PrecipitationProbability: 0.35},
		{StartAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.FixedZone("", 8*3600)), EndAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.FixedZone("", 8*3600)), Condition: "阵雨", ConditionCode: "300", TemperatureMin: 25, TemperatureMax: 31, PrecipitationProbability: 0.8},
	}
	if !reflect.DeepEqual(got.Forecasts, wantForecasts) {
		t.Fatalf("forecasts = %#v, want %#v", got.Forecasts, wantForecasts)
	}
}

func TestReadCurrentClearsOptionalMeasurementsWhenSourceOmitsThem(t *testing.T) {
	client, err := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"condition":{"text":"阴","code":"104"},"temperature":{"value":28,"unit":"°C"}}`), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	status := model.WeatherStatus{
		FeelsLike: float64TestPointer(34), HumidityPercent: float64TestPointer(80), WindScale: intTestPointer(7),
		WindGustMetersPerSecond: float64TestPointer(18), UVIndex: float64TestPointer(9),
	}
	if err := client.readCurrent(context.Background(), &status); err != nil {
		t.Fatalf("readCurrent() error = %v", err)
	}
	if status.FeelsLike != nil || status.HumidityPercent != nil || status.WindScale != nil || status.WindGustMetersPerSecond != nil || status.UVIndex != nil {
		t.Fatalf("optional current measurements were retained: %#v", status)
	}
}

func TestReadCurrentRejectsHumidityOutsideFractionRange(t *testing.T) {
	for _, humidity := range []string{"-0.01", "1.01"} {
		t.Run(humidity, func(t *testing.T) {
			client, err := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return response(http.StatusOK, `{"condition":{"text":"阴","code":"104"},"temperature":{"value":28},"humidity":`+humidity+`}`), nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			status := model.WeatherStatus{HumidityPercent: float64TestPointer(80)}
			if err := client.readCurrent(context.Background(), &status); err != nil {
				t.Fatalf("readCurrent() error = %v", err)
			}
			if status.HumidityPercent != nil {
				t.Fatalf("HumidityPercent = %v, want nil for source humidity %s", *status.HumidityPercent, humidity)
			}
		})
	}
}

func float64TestPointer(value float64) *float64 { return &value }
func intTestPointer(value int) *int             { return &value }

func TestSummarizeRainKeepsTraceRainAsLocalPossibility(t *testing.T) {
	points := []minutelyPrecipitation{
		{FXTime: "2026-09-29T10:00+08:00", Precip: "0.01", Type: "rain"},
		{FXTime: "2026-09-29T10:05+08:00", Precip: "0.01", Type: "rain"},
		{FXTime: "2026-09-29T10:10+08:00", Precip: "0.01", Type: "rain"},
		{FXTime: "2026-09-29T10:15+08:00", Precip: "0.0", Type: "rain"},
	}
	now := time.Date(2026, 9, 29, 2, 0, 0, 0, time.UTC)
	parsed, err := parseRainPoints(points, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := rainSummaryAt(parsed, now); got != "局部可能有雨" {
		t.Fatalf("summarizeRain() = %q, want trace precipitation retained without exact timing", got)
	}
}

func TestSummarizeRainKeepsIsolatedShowerAsLocalPossibility(t *testing.T) {
	points := []minutelyPrecipitation{
		{FXTime: "2026-09-29T10:00+08:00", Precip: "0.0", Type: "rain"},
		{FXTime: "2026-09-29T10:05+08:00", Precip: "0.2", Type: "rain"},
		{FXTime: "2026-09-29T10:10+08:00", Precip: "0.0", Type: "rain"},
	}
	now := time.Date(2026, 9, 29, 2, 0, 0, 0, time.UTC)
	parsed, err := parseRainPoints(points, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := rainSummaryAt(parsed, now); got != "局部可能有雨" {
		t.Fatalf("summarizeRain() = %q, want isolated prediction retained without exact timing", got)
	}
}

func TestWeatherRefreshUsesEachSourceUpdateCadence(t *testing.T) {
	now := time.Date(2026, time.September, 29, 10, 0, 0, 0, time.UTC)
	requests := map[string]int{}
	var requestMu sync.Mutex
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestMu.Lock()
		requests[request.URL.Path]++
		requestMu.Unlock()
		switch request.URL.Path {
		case "/weather/v1/current/22.54/114.06":
			return response(http.StatusOK, `{"condition":{"text":"多云","code":"101"},"temperature":{"value":29.4}}`), nil
		case "/weather/v1/daily/22.54/114.06":
			return response(http.StatusOK, validDailyPayload(now)), nil
		case "/v7/minutely/5m":
			return response(http.StatusOK, validRainPayload(now)), nil
		case "/weatheralert/v1/current/22.54/114.06":
			return response(http.StatusOK, `{"alerts":[]}`), nil
		default:
			t.Fatalf("unexpected weather path %q", request.URL.Path)
			return nil, nil
		}
	})}
	client, err := New(weatherConfig(), httpClient)
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return now }

	for tick := 0; tick <= 12; tick++ {
		got, refreshErr := client.Refresh(context.Background())
		if refreshErr != nil {
			t.Fatalf("Refresh() at tick %d error = %v", tick, refreshErr)
		}
		wantRain := "暂未见明显降雨"
		if tick%2 == 0 {
			wantRain = "未来2小时无明显降雨"
		}
		if !got.Enabled || got.Temperature != 29.4 || got.RainSummary != wantRain || len(got.Forecasts) != 2 {
			t.Fatalf("Refresh() at tick %d = %#v", tick, got)
		}
		stamp := client.RefreshedAt()
		if !stamp.Equal(now) {
			t.Fatal("successful source refresh timestamp missing")
		}
		// A scheduler tick just before the next interval has no upstream work.
		if _, err := client.Refresh(context.Background()); err != nil || !client.RefreshedAt().Equal(stamp) {
			t.Fatal("cached refresh replaced last request time")
		}
		now = now.Add(5 * time.Minute)
	}

	want := map[string]int{
		"/weather/v1/current/22.54/114.06":      7,
		"/weather/v1/daily/22.54/114.06":        2,
		"/v7/minutely/5m":                       7,
		"/weatheralert/v1/current/22.54/114.06": 13,
	}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("request counts = %#v, want %#v", requests, want)
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
