package dashboard

import (
	"time"

	"example.com/nas-wallboard/internal/model"
)

// Only select cached forecast samples, never generate observations or alerts.
// Ordinary turns are lower priority than the existing precipitation/wind picks.
func hourlyTurningPairs(hours []model.WeatherHour) [][2]int {
	pairs := [][2]int{}
	best := [2]int{-1, -1}
	bestDrop, bestDuration := 0.0, time.Duration(0)
	for i := range hours {
		if i > 0 && hours[i].At.Sub(hours[i-1].At) == time.Hour && hours[i].Temperature > hours[i-1].Temperature+1 && (i+1 >= len(hours) || hours[i+1].Temperature < hours[i].Temperature-1) {
			continue // An isolated warm spike is not evidence of sustained cooling.
		}
		for j := i + 1; j+1 < len(hours); j++ {
			duration := hours[j].At.Sub(hours[i].At)
			if duration > 3*time.Hour || hours[j].At.Sub(hours[j-1].At) != time.Hour {
				break
			}
			drop := hours[i].Temperature - hours[j].Temperature
			// Require a follow-up sample; isolated dips and immediately reversed
			// model noise should not crowd out the spaced later outlook.
			if drop < 4 || hours[j+1].At.Sub(hours[j].At) != time.Hour || hours[j+1].Temperature > hours[j].Temperature+1 {
				continue
			}
			if best[0] < 0 || drop > bestDrop || drop == bestDrop && duration < bestDuration {
				best, bestDrop, bestDuration = [2]int{i, j}, drop, duration
			}
		}
	}
	if best[0] >= 0 {
		pairs = append(pairs, best)
	}
	for i := 2; i+1 < len(hours); i++ {
		before, after := hourlyDryGroup(hours[i-1]), hourlyDryGroup(hours[i])
		if before == 0 || after == 0 || before == after || hourlyDryGroup(hours[i-2]) != before || hourlyDryGroup(hours[i+1]) != after || hours[i-1].At.Sub(hours[i-2].At) != time.Hour || hours[i].At.Sub(hours[i-1].At) != time.Hour || hours[i+1].At.Sub(hours[i].At) != time.Hour {
			continue
		}
		pairs = append(pairs, [2]int{i - 1, i})
	}
	return pairs
}

func hourlyDryGroup(hour model.WeatherHour) int {
	if hourlyEvent(hour) != "" {
		return 0
	}
	switch hour.ConditionCode {
	case "100", "102", "103", "150", "152", "153":
		return 1 // Clear/few clouds: ignore small changes and day/night variants.
	case "101", "104", "151":
		return 2 // Cloudy/overcast.
	default:
		return 0 // Unknown or other phenomena are not evidence of a dry reversal.
	}
}
