package scrutiny

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/model"
)

const maxResponseBytes = 2 << 20

type Client struct {
	endpoint *url.URL
	http     *http.Client
}

func New(cfg config.ScrutinyConfig, httpClient *http.Client) (*Client, error) {
	endpoint, err := url.Parse(cfg.URL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, errors.New("invalid Scrutiny URL")
	}
	return &Client{endpoint: endpoint, http: integration.HardenHTTPClient(httpClient)}, nil
}

func (c *Client) Probe(ctx context.Context) integration.ProbeResult {
	if _, err := c.Summary(ctx); err != nil {
		return integration.ProbeFailure(err)
	}
	return integration.ProbeResult{Stage: integration.ProbeStageFeature, OK: true, Message: "Scrutiny 连接成功"}
}

func (c *Client) Summary(ctx context.Context) ([]model.DiskHealthStatus, error) {
	endpoint := *c.endpoint
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + "/api/summary"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Scrutiny request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("read Scrutiny: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read Scrutiny: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Scrutiny response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("read Scrutiny response: response exceeds size limit")
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Summary map[string]struct {
				Device struct {
					Name     string `json:"device_name"`
					Model    string `json:"model_name"`
					Capacity uint64 `json:"capacity"`
					Status   *int   `json:"device_status"`
				} `json:"device"`
			} `json:"summary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("decode Scrutiny response: invalid JSON")
	}
	if !payload.Success {
		return nil, errors.New("Scrutiny response was not successful")
	}
	result := make([]model.DiskHealthStatus, 0, len(payload.Data.Summary))
	for _, entry := range payload.Data.Summary {
		state := "unknown"
		if entry.Device.Status != nil {
			if *entry.Device.Status == 0 {
				state = "healthy"
			} else {
				state = "failed"
			}
		}
		result = append(result, model.DiskHealthStatus{
			Name: strings.TrimSpace(entry.Device.Name), Model: strings.TrimSpace(entry.Device.Model), SizeBytes: entry.Device.Capacity, State: state,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
