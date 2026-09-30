package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/model"
)

const maxResponseBytes = 2 << 20

const (
	currentRefreshInterval  = 10 * time.Minute
	rainRefreshInterval     = 10 * time.Minute
	alertRefreshInterval    = 5 * time.Minute
	forecastRefreshInterval = time.Hour
	minRainSamples          = 3
	minRainTotalMillimeters = 0.1
)

type minutelyPrecipitation struct {
	FXTime string `json:"fxTime"`
	Precip string `json:"precip"`
	Type   string `json:"type"`
}

type refreshCache struct {
	status model.WeatherStatus
}

type Client struct {
	cfg      config.WeatherConfig
	endpoint *url.URL
	http     *http.Client
	now      func() time.Time
	cache    refreshCache
}

func New(cfg config.WeatherConfig, httpClient *http.Client) (*Client, error) {
	client := &Client{cfg: cfg, http: integration.HardenHTTPClient(httpClient), now: time.Now}
	if !cfg.Enabled {
		return client, nil
	}
	endpoint, err := url.Parse("https://" + strings.TrimSpace(cfg.APIHost))
	if err != nil || endpoint.Host == "" || endpoint.Scheme != "https" {
		return nil, errors.New("invalid QWeather API Host")
	}
	client.endpoint = endpoint
	return client, nil
}

func (c *Client) Probe(ctx context.Context) integration.ProbeResult {
	if _, err := c.Current(ctx); err != nil {
		return integration.ProbeFailure(err)
	}
	return integration.ProbeResult{Stage: integration.ProbeStageFeature, OK: true, Message: "和风天气连接成功"}
}

func (c *Client) Current(ctx context.Context) (model.WeatherStatus, error) {
	if !c.cfg.Enabled {
		return model.WeatherStatus{Enabled: false, Units: c.cfg.Units}, nil
	}
	result := c.emptyStatus()
	if err := c.readCurrent(ctx, &result); err != nil {
		return model.WeatherStatus{}, err
	}
	if err := c.readForecast(ctx, &result); err != nil {
		return model.WeatherStatus{}, err
	}
	if err := c.readRain(ctx, &result); err != nil {
		return model.WeatherStatus{}, err
	}
	if err := c.readAlerts(ctx, &result); err != nil {
		return model.WeatherStatus{}, err
	}
	return result, nil
}

func (c *Client) Refresh(ctx context.Context) (model.WeatherStatus, error) {
	if !c.cfg.Enabled {
		return model.WeatherStatus{Enabled: false, Units: c.cfg.Units}, nil
	}
	now := c.now()
	next := c.cache
	if !next.status.Enabled {
		next.status = c.emptyStatus()
	}
	components := model.WeatherComponents{}
	if next.status.Components != nil {
		components = *next.status.Components
	}
	next.status.Components = &components
	var failures []error
	for _, source := range []struct {
		metadata *model.WeatherComponent
		interval time.Duration
		read     func(context.Context, *model.WeatherStatus) error
	}{
		{&components.Current, currentRefreshInterval, c.readCurrent},
		{&components.Rain, rainRefreshInterval, c.readRain},
		{&components.Alerts, alertRefreshInterval, c.readAlerts},
		{&components.Forecast, forecastRefreshInterval, c.readForecast},
	} {
		metadata := source.metadata
		if refreshDue(now, metadata.AttemptedAt, source.interval) {
			metadata.AttemptedAt = now
			// Readers stage their output and only commit after a successful response.
			if err := source.read(ctx, &next.status); err != nil {
				metadata.Error = "unavailable"
				failures = append(failures, err)
			} else {
				metadata.UpdatedAt = now
				metadata.ExpiresAt = now.Add(3 * source.interval)
				metadata.Error = ""
			}
		} else if metadata.Error != "" {
			failures = append(failures, errors.New("weather component unavailable"))
		}
		metadata.Stale = metadata.UpdatedAt.IsZero() || now.After(metadata.ExpiresAt)
	}
	c.cache = next
	// A caller must not be able to mutate the cached component timestamps.
	output := next.status
	copy := components
	output.Components = &copy
	return output, errors.Join(failures...)
}

// RefreshedAt is the last successful upstream component refresh, not the last
// call to Refresh. Cached scheduler ticks must not change collection freshness.
func (c *Client) RefreshedAt() time.Time {
	if c.cache.status.Components == nil {
		return time.Time{}
	}
	components := c.cache.status.Components
	latest := components.Current.UpdatedAt
	for _, stamp := range []time.Time{components.Rain.UpdatedAt, components.Alerts.UpdatedAt, components.Forecast.UpdatedAt} {
		if stamp.After(latest) {
			latest = stamp
		}
	}
	return latest
}

func (c *Client) emptyStatus() model.WeatherStatus {
	return model.WeatherStatus{
		Enabled: true, Name: strings.TrimSpace(c.cfg.Name), Warnings: []model.WeatherWarning{},
		Forecasts: []model.WeatherForecast{}, Source: "和风天气", Units: c.cfg.Units,
	}
}

func refreshDue(now, refreshedAt time.Time, interval time.Duration) bool {
	return refreshedAt.IsZero() || !now.Before(refreshedAt.Add(interval))
}

func (c *Client) readCurrent(ctx context.Context, result *model.WeatherStatus) error {
	coordinates := coordinate(c.cfg.Latitude) + "/" + coordinate(c.cfg.Longitude)
	var current struct {
		Condition struct {
			Text string `json:"text"`
			Code string `json:"code"`
		} `json:"condition"`
		Temperature struct {
			Value float64 `json:"value"`
		} `json:"temperature"`
		FeelsLike *struct {
			Value float64 `json:"value"`
		} `json:"feelsLike"`
		Humidity *float64 `json:"humidity"`
		Wind     struct {
			Scale *int `json:"scale"`
		} `json:"wind"`
		WindGust *struct {
			Value float64 `json:"value"`
		} `json:"windGust"`
		UVIndex *float64 `json:"uvIndex"`
	}
	if err := c.getJSON(ctx, "/weather/v1/current/"+coordinates, url.Values{"lang": {"zh"}}, &current); err != nil {
		return fmt.Errorf("read current weather: %w", err)
	}
	result.FeelsLike = nil
	result.HumidityPercent = nil
	result.WindScale = nil
	result.WindGustMetersPerSecond = nil
	result.UVIndex = nil
	result.Temperature = current.Temperature.Value
	if current.FeelsLike != nil {
		value := current.FeelsLike.Value
		result.FeelsLike = &value
	}
	if current.Humidity != nil && *current.Humidity >= 0 && *current.Humidity <= 1 {
		value := *current.Humidity * 100
		result.HumidityPercent = &value
	}
	if current.Wind.Scale != nil && *current.Wind.Scale >= 0 && *current.Wind.Scale <= 17 {
		value := *current.Wind.Scale
		result.WindScale = &value
	}
	if current.WindGust != nil && current.WindGust.Value >= 0 {
		value := current.WindGust.Value
		result.WindGustMetersPerSecond = &value
	}
	if current.UVIndex != nil && *current.UVIndex >= 0 && *current.UVIndex <= 15 {
		value := *current.UVIndex
		result.UVIndex = &value
	}
	result.Condition = strings.TrimSpace(current.Condition.Text)
	result.ConditionCode = strings.TrimSpace(current.Condition.Code)
	return nil
}

func (c *Client) readForecast(ctx context.Context, result *model.WeatherStatus) error {
	coordinates := coordinate(c.cfg.Latitude) + "/" + coordinate(c.cfg.Longitude)
	var daily struct {
		Days []struct {
			Daytime struct {
				Condition struct {
					Text string `json:"text"`
					Code string `json:"code"`
				} `json:"condition"`
				Precipitation struct {
					Probability float64 `json:"probability"`
				} `json:"precipitation"`
			} `json:"daytime"`
			TemperatureMin struct {
				Value float64 `json:"value"`
			} `json:"temperatureMin"`
			TemperatureMax struct {
				Value float64 `json:"value"`
			} `json:"temperatureMax"`
		} `json:"days"`
	}
	if err := c.getJSON(ctx, "/weather/v1/daily/"+coordinates, url.Values{"days": {"3"}, "lang": {"zh"}, "localTime": {"true"}}, &daily); err != nil {
		return fmt.Errorf("read daily forecast: %w", err)
	}
	forecasts := make([]model.WeatherForecast, 0, 2)
	for index := 1; index < len(daily.Days) && len(forecasts) < 2; index++ {
		day := daily.Days[index]
		forecasts = append(forecasts, model.WeatherForecast{
			Condition: strings.TrimSpace(day.Daytime.Condition.Text), ConditionCode: strings.TrimSpace(day.Daytime.Condition.Code),
			TemperatureMin: day.TemperatureMin.Value, TemperatureMax: day.TemperatureMax.Value,
			PrecipitationProbability: day.Daytime.Precipitation.Probability,
		})
	}
	result.Forecasts = forecasts
	return nil
}

func (c *Client) readRain(ctx context.Context, result *model.WeatherStatus) error {
	var rain struct {
		Code     string                  `json:"code"`
		Minutely []minutelyPrecipitation `json:"minutely"`
	}
	location := coordinate(c.cfg.Longitude) + "," + coordinate(c.cfg.Latitude)
	if err := c.getJSON(ctx, "/v7/minutely/5m", url.Values{"location": {location}, "lang": {"zh"}}, &rain); err != nil {
		return fmt.Errorf("read rain forecast: %w", err)
	}
	if rain.Code != "200" {
		return errors.New("read rain forecast: QWeather returned an error")
	}
	result.RainSummary = summarizeRain(rain.Minutely)
	return nil
}

func summarizeRain(points []minutelyPrecipitation) string {
	start := stableRainStart(points)
	if start < 0 {
		if hasRainSignal(points) {
			return "局部可能有雨"
		}
		return "未来2小时无明显降雨"
	}
	minutes := start * 5
	switch {
	case minutes == 0:
		return "当前可能有雨"
	case minutes <= 20:
		return "短时可能有雨"
	case minutes <= 50:
		return "约半小时后可能有雨"
	case minutes <= 80:
		return "约1小时后可能有雨"
	default:
		return "未来2小时可能有雨"
	}
}

func hasRainSignal(points []minutelyPrecipitation) bool {
	for _, point := range points {
		amount, err := strconv.ParseFloat(strings.TrimSpace(point.Precip), 64)
		if err == nil && amount > 0 {
			return true
		}
	}
	return false
}

func stableRainStart(points []minutelyPrecipitation) int {
	for start := 0; start+minRainSamples <= len(points); start++ {
		total := 0.0
		stable := true
		for index := start; index < start+minRainSamples; index++ {
			amount, err := strconv.ParseFloat(strings.TrimSpace(points[index].Precip), 64)
			if err != nil || amount <= 0 {
				stable = false
				break
			}
			total += amount
		}
		if stable && total >= minRainTotalMillimeters {
			return start
		}
	}
	return -1
}

func (c *Client) readAlerts(ctx context.Context, result *model.WeatherStatus) error {
	coordinates := coordinate(c.cfg.Latitude) + "/" + coordinate(c.cfg.Longitude)
	var alerts struct {
		Alerts []struct {
			ID        string `json:"id"`
			Severity  string `json:"severity"`
			Headline  string `json:"headline"`
			EventType struct {
				Name string `json:"name"`
			} `json:"eventType"`
			Color struct {
				Code string `json:"code"`
			} `json:"color"`
		} `json:"alerts"`
	}
	if err := c.getJSON(ctx, "/weatheralert/v1/current/"+coordinates, url.Values{"lang": {"zh"}, "localTime": {"true"}}, &alerts); err != nil {
		return fmt.Errorf("read weather alerts: %w", err)
	}
	warnings := make([]model.WeatherWarning, 0, len(alerts.Alerts))
	for _, alert := range alerts.Alerts {
		title := strings.TrimSpace(alert.Headline)
		if title == "" {
			title = strings.TrimSpace(alert.EventType.Name)
		}
		warnings = append(warnings, model.WeatherWarning{
			ID: strings.TrimSpace(alert.ID), Title: title, Severity: strings.ToLower(strings.TrimSpace(alert.Severity)), Color: strings.ToLower(strings.TrimSpace(alert.Color.Code)),
		})
	}
	result.Warnings = warnings
	return nil
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, target any) error {
	endpoint := *c.endpoint
	endpoint.Path = path
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-QW-Api-Key", c.cfg.APIKey)
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("request QWeather: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("QWeather HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return errors.New("response exceeds size limit")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return errors.New("invalid JSON response")
	}
	return nil
}

func coordinate(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
