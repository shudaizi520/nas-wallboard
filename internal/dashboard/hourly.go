package dashboard

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"example.com/nas-wallboard/internal/model"
)

// Select from cached samples only. Absolute labels make deliberately unequal
// intervals explicit; the screen never rotates or needs pointer interaction.
func addHourlyWeather(activity *Activity, module model.Module[model.WeatherStatus], now time.Time) {
	weather := module.Data
	if weather.Components == nil || weather.Components.Hourly == nil {
		return
	}
	appendDetail := func(detail string, pinned bool) {
		if detail == "" {
			return
		}
		if activity.Detail != "" {
			activity.Detail += "\n"
		}
		activity.Detail += detail
		appendWeatherReminder(activity, detail, pinned)
		if activity.DetailParts != nil {
			parts := append(DetailPartList(nil), (*activity.DetailParts)...)
			parts = append(parts, DetailPart{Text: "\n" + detail})
			activity.DetailParts = &parts
		}
	}
	if !weatherComponentAvailable(*weather.Components.Hourly) {
		appendDetail("小时预报暂不可用", true)
		return
	}
	hours := make([]model.WeatherHour, 0, len(weather.Hourly))
	for _, hour := range weather.Hourly {
		if hour.At.After(now) && !hour.At.After(now.Add(12*time.Hour)) && strings.TrimSpace(hour.Condition) != "" && !invalidTemperature(hour.Temperature) {
			hours = append(hours, hour)
		}
	}
	sort.SliceStable(hours, func(i, j int) bool { return hours[i].At.Before(hours[j].At) })
	if len(hours) == 0 {
		return
	}
	seen := map[int64]bool{}
	unique := hours[:0]
	for _, hour := range hours {
		if !seen[hour.At.Unix()] {
			unique = append(unique, hour)
			seen[hour.At.Unix()] = true
		}
	}
	hours = unique
	list := WeatherHourList{}
	selected := map[int]bool{}
	pick := func(index int) {
		if index >= 0 && index < len(hours) && len(selected) < 6 {
			selected[index] = true
		}
	}
	event := -1
	windEvent := -1
	for i, hour := range hours {
		if windEvent < 0 && hourlyWind(hour) {
			windEvent = i
		}
		kind := hourlyEvent(hour)
		if event < 0 && (kind == "雨" || kind == "雪") {
			event = i
		}
	}
	if event < 0 {
		event = windEvent
	}
	appendEvent := func(start, end int, kind string) {
		label := hourLabel(hours[start].At, now)
		if end > start && sameLocalDate(hours[start].At, hours[end].At) {
			label = strings.TrimSuffix(label, "时") + "–" + strconv.Itoa(hours[end].At.Hour()) + "时"
		}
		suffix := "可能有雨"
		if kind == "雪" {
			suffix = "可能有雪"
		}
		if kind == "强风" {
			suffix = "可能有强风"
		}
		appendDetail(label+suffix, false)
	}
	if event >= 0 {
		kind := hourlyEvent(hours[event])
		end := event
		for end+1 < len(hours) && hours[end+1].At.Sub(hours[end].At) == time.Hour && hourlyEvent(hours[end+1]) == kind {
			end++
		}
		pick(event - 1)
		pick(event)
		pick(event + 1)
		pick(end)
		if kind == "强风" {
			appendEvent(event, end, kind)
		}
	}
	// Minutely guidance only covers the next two hours. Deduplicate bands wholly
	// within that window, never independent later rain or a band continuing past
	// it. Scan each precipitation band so a near one cannot hide a later one.
	nearRain := compactRainSummary(weather.RainSummary)
	nearRainActive := weatherComponentAvailable(weather.Components.Rain) && nearRain != "" && nearRain != "未来2小时无明显降雨" && nearRain != "暂未见明显降雨"
	for start := 0; start < len(hours); start++ {
		kind := hourlyEvent(hours[start])
		if kind != "雨" && kind != "雪" {
			continue
		}
		end := start
		for end+1 < len(hours) && hours[end+1].At.Sub(hours[end].At) == time.Hour && hourlyEvent(hours[end+1]) == kind {
			end++
		}
		if kind != "雨" || !nearRainActive || hours[end].At.After(now.Add(2*time.Hour)) {
			appendEvent(start, end, kind)
		}
		start = end
	}
	if windEvent >= 0 && (event < 0 || hourlyEvent(hours[event]) != "强风") {
		pick(windEvent)
		appendDetail(hourLabel(hours[windEvent].At, now)+"可能有强风", false)
	}
	// Fill quiet periods with the nearest available even-hour anchors. The
	// selection itself does not request upstream data or renew cache freshness.
	for offset := 2; offset <= 12; offset += 2 {
		target := now.Truncate(time.Hour).Add(time.Duration(offset) * time.Hour)
		for i, hour := range hours {
			if !hour.At.Before(target) {
				pick(i)
				break
			}
		}
	}
	for i := range hours {
		pick(i)
	}
	indexes := make([]int, 0, len(selected))
	for index := range selected {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, i := range indexes {
		hour := hours[i]
		icon, condition := hourlyPresentation(hour, weather)
		list = append(list, WeatherHour{At: hour.At.Format(time.RFC3339), Label: hourLabel(hour.At, now), Icon: icon, Condition: condition, Temperature: weatherTemperature(hour.Temperature, weather.Units), Rain: forecastProbability(hour.PrecipitationProbability, hour.PrecipitationMissing)})
	}
	if len(list) > 0 {
		activity.ForecastHours = &list
	}
}

func sameLocalDate(first, second time.Time) bool {
	second = second.In(first.Location())
	return first.Year() == second.Year() && first.YearDay() == second.YearDay()
}

func hourLabel(at, now time.Time) string {
	prefix := ""
	if !sameLocalDate(at, now) {
		prefix = "次日"
	}
	return prefix + strconv.Itoa(at.Hour()) + "时"
}

func hourlyEvent(hour model.WeatherHour) string {
	// Presentation icons are not evidence of precipitation (e.g. 901 means
	// cold, not snow). Keep meteorological event classification independent.
	code, _ := strconv.Atoi(strings.TrimSpace(hour.ConditionCode))
	if code >= 400 && code <= 499 {
		return "雪"
	}
	// Explicit precipitation phenomena take priority over a conflicting type;
	// never turn a stated thunderstorm into a generic snow symbol/reminder.
	if code >= 300 && code <= 399 {
		return "雨"
	}
	if !invalidNumber(hour.PrecipitationAmount) && hour.PrecipitationAmount > 0 || !hour.PrecipitationMissing && !invalidNumber(hour.PrecipitationProbability) && hour.PrecipitationProbability >= 0.3 && hour.PrecipitationProbability <= 1 {
		if hour.PrecipitationType == "snow" {
			return "雪"
		}
		return "雨"
	}
	if hourlyWind(hour) {
		return "强风"
	}
	return ""
}

// The same precipitation evidence drives selection, reminders and icons.
// Preserve explicit phenomena (especially thunderstorms), source probability
// and current observations. Supplement a dry condition's description only
// when amount/probability establishes a future precipitation risk.
func hourlyPresentation(hour model.WeatherHour, weather model.WeatherStatus) (string, string) {
	icon, condition := weatherIconAt(hour.ConditionCode, weather, hour.At), hour.Condition
	switch hourlyEvent(hour) {
	case "雨":
		if icon != "weather-rain" && icon != "weather-storm" {
			return "weather-rain", condition + " · 可能有雨"
		}
	case "雪":
		if icon != "weather-snow" {
			return "weather-snow", condition + " · 可能有雪"
		}
	}
	return icon, condition
}

func hourlyWind(hour model.WeatherHour) bool {
	return hour.WindScale >= 6 && hour.WindScale <= 17 || !invalidNumber(hour.WindGustMetersPerSecond) && hour.WindGustMetersPerSecond >= 17.2
}
