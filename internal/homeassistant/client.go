package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
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
	if strings.TrimSpace(c.config.PowerEntityID) != "" {
		if _, err := c.CurrentPower(ctx); err != nil {
			return integration.ProbeFailure(err)
		}
	}
	return integration.ProbeResult{Stage: integration.ProbeStageFeature, OK: true, Message: "Home Assistant 连接成功"}
}

func (c *Client) CurrentFan(ctx context.Context) (model.FanStatus, error) {
	entity, err := c.currentEntity(ctx, c.config.FanEntityID)
	if err != nil {
		return model.FanStatus{}, err
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

func (c *Client) CurrentPower(ctx context.Context) (model.PowerStatus, error) {
	entity, err := c.currentEntity(ctx, c.config.PowerEntityID)
	if err != nil {
		return model.PowerStatus{}, err
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(entity.State), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100_000 {
		return model.PowerStatus{}, errors.New("invalid_response")
	}
	unit, _ := entity.Attributes["unit_of_measurement"].(string)
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "w":
	case "kw":
		value *= 1000
	default:
		return model.PowerStatus{}, errors.New("invalid_response")
	}
	return model.PowerStatus{Available: true, Watts: value}, nil
}

type entityState struct {
	State       string         `json:"state"`
	LastChanged string         `json:"last_changed"`
	Attributes  map[string]any `json:"attributes"`
}

func (c *Client) currentEntity(ctx context.Context, entityID string) (entityState, error) {
	requestCtx := ctx
	cancel := func() {}
	if c.config.CallTimeout.Duration > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, c.config.CallTimeout.Duration)
	}
	defer cancel()

	endpoint := strings.TrimRight(c.config.URL, "/") + "/api/states/" + url.PathEscape(entityID)
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return entityState{}, errors.New("unavailable")
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return entityState{}, errors.New("timeout")
		}
		return entityState{}, errors.New("unavailable")
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return entityState{}, errors.New("unauthorized")
		case http.StatusNotFound:
			return entityState{}, errors.New("not_found")
		default:
			return entityState{}, errors.New("unavailable")
		}
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return entityState{}, errors.New("unavailable")
	}
	if len(body) > maxResponseBytes {
		return entityState{}, errors.New("response_too_large")
	}

	var entity entityState
	if err := json.Unmarshal(body, &entity); err != nil {
		return entityState{}, errors.New("invalid_response")
	}
	return entity, nil
}

func truncate(value string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}
