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

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/model"
)

const maxResponseBytes = 2 << 20

type Client struct {
	cfg      config.WeatherConfig
	endpoint *url.URL
	http     *http.Client
}

func New(cfg config.WeatherConfig, httpClient *http.Client) (*Client, error) {
	client := &Client{cfg: cfg, http: integration.HardenHTTPClient(httpClient)}
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
	coordinates := coordinate(c.cfg.Latitude) + "/" + coordinate(c.cfg.Longitude)
	var current struct {
		Condition struct {
			Text string `json:"text"`
			Code string `json:"code"`
		} `json:"condition"`
		Temperature struct {
			Value float64 `json:"value"`
		} `json:"temperature"`
	}
	if err := c.getJSON(ctx, "/weather/v1/current/"+coordinates, url.Values{"lang": {"zh"}}, &current); err != nil {
		return model.WeatherStatus{}, fmt.Errorf("read current weather: %w", err)
	}

	var rain struct {
		Code    string `json:"code"`
		Summary string `json:"summary"`
	}
	location := coordinate(c.cfg.Longitude) + "," + coordinate(c.cfg.Latitude)
	if err := c.getJSON(ctx, "/v7/minutely/5m", url.Values{"location": {location}, "lang": {"zh"}}, &rain); err != nil {
		return model.WeatherStatus{}, fmt.Errorf("read rain forecast: %w", err)
	}
	if rain.Code != "200" {
		return model.WeatherStatus{}, errors.New("read rain forecast: QWeather returned an error")
	}

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
		return model.WeatherStatus{}, fmt.Errorf("read weather alerts: %w", err)
	}

	result := model.WeatherStatus{
		Enabled: true, Name: strings.TrimSpace(c.cfg.Name), Temperature: current.Temperature.Value,
		Condition: strings.TrimSpace(current.Condition.Text), ConditionCode: strings.TrimSpace(current.Condition.Code),
		RainSummary: strings.TrimSpace(rain.Summary), Warnings: []model.WeatherWarning{}, Source: "和风天气", Units: c.cfg.Units,
	}
	for _, alert := range alerts.Alerts {
		title := strings.TrimSpace(alert.Headline)
		if title == "" {
			title = strings.TrimSpace(alert.EventType.Name)
		}
		result.Warnings = append(result.Warnings, model.WeatherWarning{
			ID: strings.TrimSpace(alert.ID), Title: title, Severity: strings.ToLower(strings.TrimSpace(alert.Severity)), Color: strings.ToLower(strings.TrimSpace(alert.Color.Code)),
		})
	}
	return result, nil
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
