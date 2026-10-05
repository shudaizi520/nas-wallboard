package retry

import "time"

// Delay allows at most three early attempts during an outage. Normal
// refresh intervals remain the fallback, including for permanently bad keys.
func Delay(normal time.Duration, failures int) time.Duration {
	if failures < 1 || failures > 3 {
		return normal
	}
	return min(normal, time.Minute<<uint(failures-1))
}
