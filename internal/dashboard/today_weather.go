package dashboard

import (
	"sort"
	"strings"
	"time"

	"example.com/nas-wallboard/internal/model"
)

// Only today's presentation can follow the remaining local-day forecast. Keep
// the source day/night details and full-day range untouched. A remaining-hours
// summary cannot inherit a probability for a different daily forecast period.
// No network requests, cache writes or synthetic observations are involved.
func addTodayMainWeather(activity *Activity, weather model.WeatherStatus, now time.Time) {
	day := weather.TodayForecast
	if activity.ForecastDays == nil || day == nil || now.Before(day.StartAt) || !now.Before(day.EndAt) {
		return
	}
	index := -1
	for i, row := range *activity.ForecastDays {
		if row.Label == "今天" && row.Date == day.StartAt.Format("2006-01-02") {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	// Do not downgrade a more serious daily phenomenon merely because hourly
	// samples disagree. Short light rain/showers may be summarized separately.
	protectedRank, protectedIcon, protectedCondition := 0, "", ""
	protectedRain := ""
	for _, period := range []forecastPeriod{dailyForecastPeriod(*day, false), dailyForecastPeriod(*day, true)} {
		class, condition := period.presentation()
		icon, rank := string(class), period.protectionRank()
		if rank > protectedRank && strings.TrimSpace(period.condition) != "" {
			protectedRank, protectedIcon, protectedCondition = rank, icon, condition
			protectedRain = forecastProbabilityBadge(period.probability, period.probabilityMissing)
		}
	}
	if protectedRank > 0 {
		row := &(*activity.ForecastDays)[index]
		row.Icon, row.Condition, row.Rain = protectedIcon, protectedCondition, protectedRain
	}
	if weather.Components == nil || weather.Components.Hourly == nil {
		return
	}
	source := *weather.Components.Hourly
	if !weatherComponentAvailable(source) || !source.ExpiresAt.IsZero() && !now.Before(source.ExpiresAt) {
		return
	}
	localNow := now.In(day.StartAt.Location())
	// Subtract local minutes instead of truncating absolute UTC hours. This
	// also retains the correct repeated hour across daylight-saving changes.
	currentHour := now.Add(-(time.Duration(localNow.Minute())*time.Minute + time.Duration(localNow.Second())*time.Second + time.Duration(localNow.Nanosecond())))
	hours := make([]model.WeatherHour, 0, 24)
	for _, hour := range weather.Hourly {
		if hour.At.Before(currentHour) || !hour.At.Before(day.EndAt) || hour.At.Before(day.StartAt) {
			continue
		}
		if hourlyWeatherClass(hour) == weatherUnknown || strings.TrimSpace(hour.Condition) == "" || invalidTemperature(hour.Temperature) {
			return
		}
		hours = append(hours, hour)
	}
	sort.SliceStable(hours, func(i, j int) bool { return hours[i].At.Before(hours[j].At) })
	// Require uninterrupted coverage through local midnight, including the
	// nearest hour. Sparse, duplicate or partial samples cannot justify clearing
	// a daily rain symbol. A duplicate conservatively falls back, never votes twice.
	if len(hours) == 0 || (!hours[0].At.Equal(currentHour) && !hours[0].At.Equal(currentHour.Add(time.Hour))) || hours[len(hours)-1].At.Add(time.Hour).Before(day.EndAt) {
		return
	}
	for i := 1; i < len(hours); i++ {
		if hours[i].At.Sub(hours[i-1].At) != time.Hour {
			return
		}
	}
	counts := map[string]int{}
	descriptions := map[string]map[string]int{}
	descriptionOrder := map[string][]string{}
	order := []string{}
	wet, streak, longest := 0, 0, 0
	wetHour := model.WeatherHour{}
	riskIcon, riskCondition := protectedIcon, protectedCondition
	riskRank := protectedRank
	riskFromHourly := false
	for _, hour := range hours {
		icon, condition := hourlyPresentation(hour, weather)
		rank := hourlyProtectionRank(hour)
		if rank > riskRank {
			riskIcon, riskCondition, riskRank = icon, condition, rank
			riskFromHourly = true
		}
		if isWeatherPrecipitationIcon(icon) {
			// The existing strip/reminders stop at 12 hours. A later rain spell
			// must remain represented in today's icon until it can be shown there.
			if hour.At.After(now.Add(12*time.Hour)) && riskIcon == "" {
				riskIcon, riskCondition = icon, condition
				riskFromHourly = true
			}
			if wet == 0 {
				wetHour = hour
			}
			wet++
			streak++
			if streak > longest {
				longest = streak
			}
			continue
		}
		streak = 0
		// Daily symbols describe conditions, not a specific day/night hour.
		icon = string(hourlyWeatherClass(hour))
		if descriptions[icon] == nil {
			descriptions[icon] = map[string]int{}
			order = append(order, icon)
		}
		counts[icon]++
		condition = strings.TrimSpace(condition)
		if descriptions[icon][condition] == 0 {
			descriptionOrder[icon] = append(descriptionOrder[icon], condition)
		}
		descriptions[icon][condition]++
	}
	icon, condition := riskIcon, riskCondition
	description := "剩余时段主要"
	suffix := "（小时预报）"
	if riskIcon != "" {
		description = "剩余时段需留意"
		if !riskFromHourly {
			description, suffix = "保留日预报", "风险"
		}
	}
	if icon == "" && (longest >= 3 || wet*2 >= len(hours)) {
		icon, condition = hourlyPresentation(wetHour, weather)
		description = "剩余时段需留意"
	}
	if icon == "" {
		// Equal counts prefer the nearest condition; map iteration cannot flip
		// the icon across otherwise identical dashboard refreshes.
		for _, candidate := range order {
			if icon == "" || counts[candidate] > counts[icon] {
				icon = candidate
			}
		}
		for _, candidate := range descriptionOrder[icon] {
			if condition == "" || descriptions[icon][candidate] > descriptions[icon][condition] {
				condition = candidate
			}
		}
	}
	if icon != "" {
		row := &(*activity.ForecastDays)[index]
		row.Icon, row.Condition = icon, condition
		row.Rain = ""
		if riskIcon != "" && !riskFromHourly {
			row.Rain = protectedRain
		}
		row.Detail += " · " + description + condition + suffix
	}
}

func briefRainCode(code string) bool {
	switch strings.TrimSpace(code) {
	case "300", "305", "309", "350":
		return true
	}
	return false
}
