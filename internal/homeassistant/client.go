package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/model"
)

const (
	maxResponseBytes = 64 * 1024
	maxLabelRunes    = 64
)

type Client struct {
	config config.HomeAssistantConfig
	token  string
	http   *http.Client
}

func New(cfg config.HomeAssistantConfig, token string, httpClient *http.Client) *Client {
	return &Client{config: cfg, token: token, http: integration.HardenHTTPClient(httpClient)}
}

func (c *Client) Probe(ctx context.Context) integration.ProbeResult {
	if _, err := c.CurrentFan(ctx); err != nil {
		return integration.ProbeFailure(err)
	}
	return integration.ProbeResult{Stage: integration.ProbeStageFeature, OK: true, Message: "Home Assistant 连接成功"}
}

func (c *Client) CurrentFan(ctx context.Context) (model.FanStatus, error) {
	requestCtx := ctx
	cancel := func() {}
	if c.config.CallTimeout.Duration > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, c.config.CallTimeout.Duration)
	}
	defer cancel()

	endpoint := strings.TrimRight(c.config.URL, "/") + "/api/states/" + url.PathEscape(c.config.FanEntityID)
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return model.FanStatus{}, errors.New("unavailable")
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return model.FanStatus{}, errors.New("timeout")
		}
		return model.FanStatus{}, errors.New("unavailable")
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return model.FanStatus{}, errors.New("unauthorized")
		case http.StatusNotFound:
			return model.FanStatus{}, errors.New("not_found")
		default:
			return model.FanStatus{}, errors.New("unavailable")
		}
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return model.FanStatus{}, errors.New("unavailable")
	}
	if len(body) > maxResponseBytes {
		return model.FanStatus{}, errors.New("response_too_large")
	}

	var entity struct {
		State       string         `json:"state"`
		LastChanged string         `json:"last_changed"`
		Attributes  map[string]any `json:"attributes"`
	}
	if err := json.Unmarshal(body, &entity); err != nil {
		return model.FanStatus{}, errors.New("invalid_response")
	}

	state := strings.ToLower(strings.TrimSpace(entity.State))
	if state != "on" && state != "off" && state != "unavailable" {
		return model.FanStatus{}, errors.New("invalid_response")
	}
	status := model.FanStatus{
		Enabled:            true,
		Available:          state != "unavailable",
		Name:               truncate(c.config.FanName, maxLabelRunes),
		State:              state,
		RemindAfterSeconds: int64(c.config.RemindAfter.Duration / time.Second),
	}
	if status.Name == "" {
		status.Name = truncate(c.config.FanEntityID, maxLabelRunes)
	}
	if state == "on" {
		if changed, err := time.Parse(time.RFC3339Nano, entity.LastChanged); err == nil {
			status.OnSince = &changed
		}
	}
	if value, ok := entity.Attributes["percentage"].(float64); ok && value >= 0 && value <= 100 {
		status.Percentage = &value
	}
	if value, ok := entity.Attributes["preset_mode"].(string); ok {
		status.PresetMode = truncate(strings.TrimSpace(value), maxLabelRunes)
	}
	if value, ok := entity.Attributes["oscillating"].(bool); ok {
		status.Oscillating = &value
	}
	return status, nil
}

func truncate(value string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}
