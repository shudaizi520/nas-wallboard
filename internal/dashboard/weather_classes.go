package dashboard

import (
	"strings"

	"example.com/nas-wallboard/internal/model"
)

// Presentation classes do not rewrite observations or forecast descriptions.
// Current, hourly and daily views share these exact supported-code mappings.
type weatherClass string

const (
	weatherClear        weatherClass = "weather-sunny"
	weatherCloudy       weatherClass = "weather-cloudy"
	weatherOvercast     weatherClass = "weather-overcast"
	weatherRain         weatherClass = "weather-rain"
	weatherHeavyRain    weatherClass = "weather-heavy-rain"
	weatherStorm        weatherClass = "weather-storm"
	weatherHail         weatherClass = "weather-hail"
	weatherFreezingRain weatherClass = "weather-freezing-rain"
	weatherSleet        weatherClass = "weather-sleet"
	weatherSnow         weatherClass = "weather-snow"
	weatherHeavySnow    weatherClass = "weather-heavy-snow"
	weatherFog          weatherClass = "weather-fog"
	weatherHaze         weatherClass = "weather-haze"
	weatherDust         weatherClass = "weather-dust"
	weatherHot          weatherClass = "weather-hot"
	weatherCold         weatherClass = "weather-cold"
	weatherUnknown      weatherClass = "weather-unknown"
)

func classifyWeather(code string) weatherClass {
	switch strings.TrimSpace(code) {
	case "100", "102", "150", "152":
		return weatherClear
	case "101", "103", "151", "153":
		return weatherCloudy
	case "104":
		return weatherOvercast
	case "300", "305", "306", "309", "314", "350", "399":
		return weatherRain
	case "301", "307", "308", "310", "311", "312", "315", "316", "317", "318", "351":
		return weatherHeavyRain
	case "302", "303":
		return weatherStorm
	case "304":
		return weatherHail
	case "313":
		return weatherFreezingRain
	case "404", "405", "406", "456":
		return weatherSleet
	case "400", "401", "407", "408", "457", "499":
		return weatherSnow
	case "402", "403", "409", "410":
		return weatherHeavySnow
	case "500", "501", "509", "510", "514", "515":
		return weatherFog
	case "502", "511", "512", "513":
		return weatherHaze
	case "503", "504", "507", "508":
		return weatherDust
	case "900":
		return weatherHot
	case "901":
		return weatherCold
	default:
		return weatherUnknown
	}
}

func (c weatherClass) precipitation() string {
	switch c {
	case weatherRain, weatherHeavyRain, weatherStorm:
		return "雨"
	case weatherHail:
		return "冰雹"
	case weatherFreezingRain:
		return "冻雨"
	case weatherSleet:
		return "雨夹雪"
	case weatherSnow, weatherHeavySnow:
		return "雪"
	default:
		return ""
	}
}

func isWeatherPrecipitationIcon(icon string) bool {
	return weatherClass(icon).precipitation() != ""
}

// This is a screen-selection priority, never an official warning severity.
func (c weatherClass) protectionRank() int {
	switch c {
	case weatherHail, weatherFreezingRain:
		return 5
	case weatherStorm:
		return 4
	case weatherHeavySnow:
		return 3
	case weatherHeavyRain, weatherSnow, weatherSleet:
		return 2
	}
	return 0
}

func hourlyProtectionRank(hour model.WeatherHour) int {
	return hourlyForecastPeriod(hour).protectionRank()
}

func hourlyWeatherClass(hour model.WeatherHour) weatherClass {
	return hourlyForecastPeriod(hour).class()
}

// Keep the source period together so its icon and probability never borrow
// fields from the other period. Numerical guidance does not classify weather.
type forecastPeriod struct {
	code, condition    string
	probability        float64
	probabilityMissing bool
	amount             *float64
}

func dailyForecastPeriod(day model.WeatherForecast, night bool) forecastPeriod {
	if night {
		return forecastPeriod{code: day.NightConditionCode, condition: day.NightCondition, probability: day.NightPrecipitationProbability, probabilityMissing: !day.NightPrecipitationKnown, amount: day.NightPrecipitationAmount}
	}
	return forecastPeriod{code: day.ConditionCode, condition: day.Condition, probability: day.PrecipitationProbability, probabilityMissing: day.PrecipitationMissing, amount: day.PrecipitationAmount}
}

func hourlyForecastPeriod(hour model.WeatherHour) forecastPeriod {
	var amount *float64
	if !invalidNumber(hour.PrecipitationAmount) && hour.PrecipitationAmount > 0 {
		amount = &hour.PrecipitationAmount
	}
	return forecastPeriod{code: hour.ConditionCode, condition: hour.Condition, probability: hour.PrecipitationProbability, probabilityMissing: hour.PrecipitationMissing, amount: amount}
}

func (p forecastPeriod) class() weatherClass {
	return classifyWeather(p.code)
}

func (p forecastPeriod) presentation() (weatherClass, string) {
	return p.class(), p.condition
}

func (p forecastPeriod) protectionRank() int {
	class := p.class()
	if rank := class.protectionRank(); rank > 0 {
		return rank
	}
	if class == weatherRain && (!briefRainCode(p.code) || p.amount != nil && !invalidNumber(*p.amount) && *p.amount >= 2.5) {
		return 1
	}
	return 0
}
