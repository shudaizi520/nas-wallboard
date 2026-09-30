package weather

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func parseWeatherTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04Z07:00"} {
		if stamp, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return stamp, nil
		}
	}
	return time.Time{}, errors.New("invalid weather time")
}

func earlier(first, second time.Time) time.Time {
	if second.Before(first) {
		return second
	}
	return first
}

// The API's localTime offset is the forecast location's calendar, not the NAS's.
func futureDaily(start, end, now time.Time) bool {
	if !end.After(now) {
		return false
	}
	local := now.In(start.Location())
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, start.Location())
	day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	return day.After(today)
}

func parseRainPoints(raw []minutelyPrecipitation, now time.Time) ([]model.RainPoint, error) {
	if len(raw) == 0 {
		return nil, errors.New("rain forecast has no samples")
	}
	points := make([]model.RainPoint, 0, len(raw))
	for index, point := range raw {
		at, err := parseWeatherTime(point.FXTime)
		if err != nil {
			return nil, errors.New("rain forecast has invalid sample time")
		}
		amount, err := strconv.ParseFloat(strings.TrimSpace(point.Precip), 64)
		if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
			return nil, errors.New("rain forecast has invalid precipitation")
		}
		if index > 0 && at.Sub(points[index-1].At) != 5*time.Minute {
			return nil, errors.New("rain forecast sample sequence unavailable")
		}
		points = append(points, model.RainPoint{At: at, Amount: amount})
	}
	if points[0].At.After(now.Add(5*time.Minute)) || !points[len(points)-1].At.Add(5*time.Minute).After(now) {
		return nil, errors.New("rain forecast does not cover the present")
	}
	return points, nil
}

func rainSummaryAt(points []model.RainPoint, now time.Time) string {
	first := 0
	for first < len(points) && !points[first].At.Add(5*time.Minute).After(now) {
		first++
	}
	points = points[first:]
	if len(points) == 0 {
		return ""
	}
	for start := 0; start+minRainSamples <= len(points); start++ {
		total, stable := 0.0, true
		for _, point := range points[start : start+minRainSamples] {
			if point.Amount <= 0 {
				stable = false
				break
			}
			total += point.Amount
		}
		if !stable || total < minRainTotalMillimeters {
			continue
		}
		minutes := int(math.Ceil(points[start].At.Sub(now).Minutes()))
		switch {
		case minutes <= 0:
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
	for _, point := range points {
		if point.Amount > 0 {
			return "局部可能有雨"
		}
	}
	if points[len(points)-1].At.Add(5*time.Minute).Sub(now) < 115*time.Minute {
		return "暂未见明显降雨"
	}
	return "未来2小时无明显降雨"
}

// ProjectAt derives display values from retained valid times without upstream
// requests or renewing success timestamps. It does not mutate cached slices.
func ProjectAt(value model.WeatherStatus, now time.Time) model.WeatherStatus {
	if value.Components != nil {
		copy := *value.Components
		value.Components = &copy
	}
	if len(value.RainPoints) > 0 {
		value.RainSummary = rainSummaryAt(value.RainPoints, now)
		if value.RainSummary == "" && value.Components != nil {
			value.Components.Rain.Stale = true
		}
	}
	original := value.Forecasts
	if len(original) > 0 {
		value.Forecasts = make([]model.WeatherForecast, 0, len(original))
		for _, forecast := range original {
			if forecast.StartAt.IsZero() && forecast.EndAt.IsZero() || futureDaily(forecast.StartAt, forecast.EndAt, now) {
				value.Forecasts = append(value.Forecasts, forecast)
			}
		}
		if len(value.Forecasts) == 0 && value.Components != nil {
			value.Components.Forecast.Stale = true
		}
	}
	return value
}
