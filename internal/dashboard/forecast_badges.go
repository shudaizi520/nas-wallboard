package dashboard

// Filter the raw probability before formatting, uniformly for every phenomenon.
// This is only display-noise filtering: never rewrite the source, weather class,
// precipitation amount, reminders or official warnings based on this threshold.
func forecastProbabilityBadge(probability float64, missing bool) string {
	if missing || invalidNumber(probability) || probability <= .20 || probability > 1 {
		return ""
	}
	return forecastProbability(probability, false)
}
