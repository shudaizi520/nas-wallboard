package dashboard

import (
	"strconv"
	"strings"
)

// This is a display-noise threshold, not a meteorological risk threshold.
// Keep wet-weather probabilities, original source data and detailed text.
func quietForecastProbability(label, icon string) string {
	if icon == "weather-rain" || icon == "weather-storm" || icon == "weather-snow" || label == "" {
		return label
	}
	if label == "<1%" {
		return ""
	}
	if probability, err := strconv.ParseFloat(strings.TrimSuffix(label, "%"), 64); err == nil && probability >= 0 && probability < 20 {
		return ""
	}
	return label
}
