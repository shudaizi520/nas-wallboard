package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/nas-wallboard/internal/integration"
	"io"
	"net/http"
	"sort"
	"strings"
)

func (c *Client) Entities(ctx context.Context) ([]integration.EntityChoice, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.config.URL, "/")+"/api/states", nil)
	if err != nil {
		return nil, errors.New("unavailable")
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, errors.New("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("unauthorized_or_unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return nil, errors.New("response_too_large")
	}
	var states []struct {
		ID         string         `json:"entity_id"`
		Attributes map[string]any `json:"attributes"`
	}
	if json.Unmarshal(data, &states) != nil {
		return nil, errors.New("invalid_response")
	}
	result := []integration.EntityChoice{}
	for _, item := range states {
		kind := ""
		unit, _ := item.Attributes["unit_of_measurement"].(string)
		if strings.HasPrefix(item.ID, "fan.") {
			kind = "fan"
		} else if strings.HasPrefix(item.ID, "sensor.") && (strings.EqualFold(strings.TrimSpace(unit), "w") || strings.EqualFold(strings.TrimSpace(unit), "kw")) {
			kind = "power"
		}
		if kind == "" || len(item.ID) > 256 {
			continue
		}
		name, _ := item.Attributes["friendly_name"].(string)
		name = truncate(name, maxLabelRunes)
		if name == "" {
			name = item.ID
		}
		result = append(result, integration.EntityChoice{ID: item.ID, Name: name, Kind: kind, Unit: unit})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
