package dashboard

import (
	"strings"
	"time"

	"example.com/nas-wallboard/internal/model"
)

// Reuse the daily forecast's location-specific astronomy. With no trustworthy
// events retain the generic condition icon instead of guessing from NAS time.
func weatherIconAt(code string, weather model.WeatherStatus, at time.Time) string {
	return weatherClassIconAt(classifyWeather(code), code, weather, at)
}

func weatherClassIconAt(class weatherClass, code string, weather model.WeatherStatus, at time.Time) string {
	base := string(class)
	if class == weatherClear && (strings.TrimSpace(code) == "150" || strings.TrimSpace(code) == "152") {
		return "weather-moon"
	}
	if class != weatherClear || weather.Components == nil || !weatherComponentAvailable(weather.Components.Forecast) {
		return base
	}
	days := weather.Forecasts
	if weather.TodayForecast != nil {
		days = append(append([]model.WeatherForecast(nil), days...), *weather.TodayForecast)
	}
	for _, day := range days {
		if !sameLocalDate(day.StartAt, at) || day.Sunrise.IsZero() || day.Sunset.IsZero() || !day.Sunrise.Before(day.Sunset) || !sameLocalDate(day.StartAt, day.Sunrise) || !sameLocalDate(day.StartAt, day.Sunset) {
			continue
		}
		if at.Before(day.Sunrise) || !at.Before(day.Sunset) {
			return "weather-moon"
		}
		return base
	}
	return base
}
