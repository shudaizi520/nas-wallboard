package dashboard

import (
	"reflect"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func calmTurningWeather(now time.Time) model.WeatherStatus {
	w := reminderWeather(now)
	w.UVIndex = nil
	for i := range w.Hourly {
		w.Hourly[i] = model.WeatherHour{At: now.Add(time.Duration(i+1) * time.Hour), Condition: "晴", ConditionCode: "100", Temperature: 30}
	}
	return w
}

func turningLabels(t *testing.T, w model.WeatherStatus, now time.Time) []string {
	t.Helper()
	a, _ := buildReminderActivity(t, w, now)
	if a.ForecastHours == nil || len(*a.ForecastHours) != 6 {
		t.Fatalf("six future slots lost: %+v", a)
	}
	labels := []string{}
	for _, h := range *a.ForecastHours {
		labels = append(labels, h.Label)
	}
	return labels
}

func TestHourlyPersistentDryTurnRetainsBothSidesInsteadOfOnlyEvenHourAnchors(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, reverse := range []bool{false, true} {
		w := calmTurningWeather(now)
		for i := range w.Hourly {
			cloudy := i >= 7
			if reverse {
				cloudy = i < 7
			}
			if cloudy {
				w.Hourly[i].Condition, w.Hourly[i].ConditionCode = "阴", "104"
			}
		}
		labels := turningLabels(t, w, now)
		if !containsLabel(labels, "19时") || !containsLabel(labels, "20时") {
			t.Fatalf("dry reversal context lost: %v", labels)
		}
		if w.Hourly[6].At.Hour() != 19 {
			t.Fatal("source mutated")
		}
	}
}

func TestHourlyStableCoolingRetainsItsWarmAndCoolEndpoints(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	w := calmTurningWeather(now)
	for i := range w.Hourly {
		if i >= 6 {
			w.Hourly[i].Temperature = 25
		}
	}
	labels := turningLabels(t, w, now)
	if !containsLabel(labels, "18时") || !containsLabel(labels, "19时") {
		t.Fatalf("significant cooling endpoint skipped: %v", labels)
	}
}

func TestHourlyMinorOrTransientChangesKeepQuietSpacedSelection(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, change := range []func(*model.WeatherStatus){
		func(w *model.WeatherStatus) { w.Hourly[6].Condition, w.Hourly[6].ConditionCode = "阴", "104" },
		func(w *model.WeatherStatus) { w.Hourly[6].Temperature = 25 },
		func(w *model.WeatherStatus) { w.Hourly[6].Temperature = 35 },
		func(w *model.WeatherStatus) {
			for i := range w.Hourly {
				w.Hourly[i].Temperature = 30 - float64(i)/2
			}
		},
		func(w *model.WeatherStatus) {
			for i := range w.Hourly {
				if i%2 == 0 {
					w.Hourly[i].Condition, w.Hourly[i].ConditionCode = "少云", "102"
				}
			}
		},
		func(w *model.WeatherStatus) {
			for i := range w.Hourly {
				if i >= 6 {
					w.Hourly[i].ConditionCode = "150"
				}
			}
		},
	} {
		w := calmTurningWeather(now)
		change(&w)
		if got := turningLabels(t, w, now); !reflect.DeepEqual(got, []string{"14时", "16时", "18时", "20时", "22时", "次日0时"}) {
			t.Fatalf("noise displaced meaningful later coverage: %v", got)
		}
	}
}

func TestHourlyRiskPrioritySurvivesDryTurnsAndCooling(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	w := calmTurningWeather(now)
	for i := range w.Hourly {
		if i >= 7 {
			w.Hourly[i].Condition, w.Hourly[i].ConditionCode, w.Hourly[i].Temperature = "阴", "104", 25
		}
	}
	w.Hourly[1].WindScale = 7
	for _, i := range []int{5, 6} {
		w.Hourly[i].Condition, w.Hourly[i].ConditionCode, w.Hourly[i].PrecipitationProbability = "小雨", "305", .65
	}
	labels := turningLabels(t, w, now)
	for _, label := range []string{"14时", "17时", "18时", "19时"} {
		if !containsLabel(labels, label) {
			t.Fatalf("ordinary turn crowded out rain/wind: %v", labels)
		}
	}
}

func TestCoolingTurnThresholdAndContinuousCoverage(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		drop float64
		gap  bool
		want bool
	}{
		{"exact four degrees", 4, false, true},
		{"below threshold", 3.9, false, false},
		{"missing continuity", 4, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := calmTurningWeather(now)
			w.Hourly = w.Hourly[:3]
			w.Hourly[1].Temperature, w.Hourly[2].Temperature = 30-tc.drop, 30-tc.drop
			if tc.gap {
				w.Hourly[1].At = w.Hourly[1].At.Add(time.Hour)
				w.Hourly[2].At = w.Hourly[2].At.Add(time.Hour)
			}
			if got := len(hourlyTurningPairs(w.Hourly)) > 0; got != tc.want {
				t.Fatalf("cooling selected=%v, want %v", got, tc.want)
			}
		})
	}
	w := calmTurningWeather(now)
	for i := range w.Hourly {
		w.Hourly[i].Temperature = 30 - float64(i)
	}
	if got := hourlyTurningPairs(w.Hourly); len(got) != 0 {
		t.Fatalf("four-hour gradual cooling is not a three-hour turn: %v", got)
	}
}

func TestCoolingAfterStableWarmPlateauIsNotDiscardedAsSpike(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	w := calmTurningWeather(now)
	w.Hourly[5].Temperature, w.Hourly[6].Temperature = 35, 35
	labels := turningLabels(t, w, now)
	if !containsLabel(labels, "19时") || !containsLabel(labels, "20时") {
		t.Fatalf("stable warm plateau cooling lost: %v", labels)
	}
}

func containsLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}
