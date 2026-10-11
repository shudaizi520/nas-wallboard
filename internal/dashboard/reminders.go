package dashboard

import "strings"

// Keep the complete legacy detail and its warning colors. Only explicitly
// separate ordinary messages are eligible for rotation on newer clients.
func initializeWeatherReminders(activity *Activity) {
	if activity.Detail == "" {
		return
	}
	pinned := activity.Value == "不可用" || activity.DetailParts != nil
	for _, urgent := range []string{"预警", "暂不可用", "更新失败", "强风", "阵风", "可能有雨"} {
		pinned = pinned || strings.Contains(activity.Detail, urgent)
	}
	messages := WeatherReminderList{{Text: activity.Detail, Pinned: pinned, DetailParts: activity.DetailParts}}
	activity.WeatherReminders = &messages
}

func appendWeatherReminder(activity *Activity, detail string, pinned bool) {
	if activity.WeatherReminders == nil {
		activity.WeatherReminders = &WeatherReminderList{}
	}
	*activity.WeatherReminders = append(*activity.WeatherReminders, WeatherReminder{Text: detail, Pinned: pinned})
}
