package weather

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func (c *Client) readHourly(ctx context.Context, result *model.WeatherStatus) error {
	var payload struct {
		Hours []struct {
			At        string `json:"forecastTime"`
			Condition struct {
				Text string `json:"text"`
				Code string `json:"code"`
			} `json:"condition"`
			Temperature struct {
				Value *float64 `json:"value"`
				Unit  string   `json:"unit"`
			} `json:"temperature"`
			Precipitation struct {
				Probability *float64 `json:"probability"`
				Amount      struct {
					Value *float64 `json:"value"`
					Unit  string   `json:"unit"`
				} `json:"amount"`
				Type string `json:"type"`
			} `json:"precipitation"`
			Wind struct {
				Scale int `json:"scale"`
			} `json:"wind"`
			Gust struct {
				Value float64 `json:"value"`
				Unit  string  `json:"unit"`
			} `json:"windGust"`
		} `json:"hours"`
	}
	coordinates := coordinate(c.cfg.Latitude) + "/" + coordinate(c.cfg.Longitude)
	if err := c.getJSON(ctx, "/weather/v1/hourly/"+coordinates, url.Values{"hours": {"24"}, "localTime": {"true"}, "lang": {"zh"}}, &payload); err != nil {
		return fmt.Errorf("read hourly forecast: %w", err)
	}
	now := c.now()
	hours := make([]model.WeatherHour, 0, 24)
	var previous time.Time
	for _, raw := range payload.Hours {
		at, err := parseWeatherTime(raw.At)
		if err != nil || !previous.IsZero() && at.Sub(previous) != time.Hour {
			return errors.New("hourly forecast has invalid sample sequence")
		}
		previous = at
		if !at.Add(time.Hour).After(now) || at.After(now.Add(24*time.Hour)) {
			continue
		}
		if raw.Temperature.Value == nil || math.IsNaN(*raw.Temperature.Value) || math.IsInf(*raw.Temperature.Value, 0) || raw.Temperature.Unit != "°C" || strings.TrimSpace(raw.Condition.Text) == "" {
			return errors.New("hourly forecast has invalid temperature or condition")
		}
		hour := model.WeatherHour{At: at, Condition: strings.TrimSpace(raw.Condition.Text), ConditionCode: strings.TrimSpace(raw.Condition.Code), Temperature: *raw.Temperature.Value, PrecipitationMissing: true, PrecipitationType: raw.Precipitation.Type}
		if p := raw.Precipitation.Probability; p != nil && *p >= 0 && *p <= 1 {
			hour.PrecipitationProbability, hour.PrecipitationMissing = *p, false
		}
		if amount := forecastAmount(raw.Precipitation.Amount.Value, raw.Precipitation.Amount.Unit); amount != nil {
			hour.PrecipitationAmount = *amount
		}
		if raw.Wind.Scale >= 0 && raw.Wind.Scale <= 17 {
			hour.WindScale = raw.Wind.Scale
		}
		if raw.Gust.Unit == "m/s" && raw.Gust.Value >= 0 {
			hour.WindGustMetersPerSecond = raw.Gust.Value
		}
		hours = append(hours, hour)
	}
	if len(hours) == 0 || hours[0].At.After(now.Add(time.Hour)) {
		return errors.New("hourly forecast does not cover the near future")
	}
	result.Hourly = hours
	return nil
}

// Keep absent/invalid daily amounts absent; hourly guidance only uses positive
// valid amounts. Neither zero nor missing amount changes a condition code.
func forecastAmount(value *float64, unit string) *float64 {
	if unit != "mm" || value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return nil
	}
	return value
}
