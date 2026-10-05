package weather

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/collector"
)

type recoveryTicker struct {
	delay   time.Duration
	ch      chan time.Time
	stopped atomic.Bool
}

func (t *recoveryTicker) C() <-chan time.Time { return t.ch }
func (t *recoveryTicker) Stop()               { t.stopped.Store(true) }

type recoveryClock struct{ scheduled chan *recoveryTicker }

func (c *recoveryClock) NewTicker(delay time.Duration) collector.Ticker {
	t := &recoveryTicker{delay: delay, ch: make(chan time.Time, 1)}
	c.scheduled <- t
	return t
}

func nextRecoveryTick(t *testing.T, c *recoveryClock, delay time.Duration) *recoveryTicker {
	t.Helper()
	select {
	case tick := <-c.scheduled:
		if tick.delay != delay {
			t.Fatalf("scheduled %s, want %s", tick.delay, delay)
		}
		return tick
	case <-time.After(time.Second):
		t.Fatal("independent source retry was not scheduled")
		return nil
	}
}

func waitWeatherRound(t *testing.T, rounds <-chan struct{}) {
	t.Helper()
	select {
	case <-rounds:
	case <-time.After(time.Second):
		t.Fatal("weather round did not complete")
	}
}

func TestWeatherRunnerRetriesNewFailureDespitePersistentSiblingFailure(t *testing.T) {
	now := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	counts := map[string]int{}
	var mu sync.Mutex
	failCurrent := false
	client, _ := New(weatherConfig(), &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		counts[r.URL.Path]++
		mu.Unlock()
		switch {
		case strings.Contains(r.URL.Path, "daily"):
			return response(503, `{}`), nil
		case strings.Contains(r.URL.Path, "weatheralert"):
			return response(200, `{"alerts":[]}`), nil
		case strings.Contains(r.URL.Path, "current"):
			if failCurrent {
				return response(503, `{}`), nil
			}
			return response(200, `{"condition":{"text":"多云","code":"101"},"temperature":{"value":29}}`), nil
		default:
			return response(200, validRainPayload(now)), nil
		}
	})})
	client.now = func() time.Time { return now }
	clock := &recoveryClock{scheduled: make(chan *recoveryTicker, 16)}
	rounds := make(chan struct{}, 16)
	runner := collector.NewRunnerWithClock([]collector.Job{{
		Name: "weather", Every: 5 * time.Minute, Timeout: time.Second, RetryOnFailure: true,
		Run: func(ctx context.Context) error { _, err := client.Refresh(ctx); return err },
		RetryAfter: func() time.Duration {
			delay := client.RetryAfter()
			rounds <- struct{}{}
			return delay
		},
	}}, clock)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runner.Run(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("runner did not stop")
		}
	})
	normal := nextRecoveryTick(t, clock, 5*time.Minute)
	waitWeatherRound(t, rounds)
	first := nextRecoveryTick(t, clock, time.Minute)
	now = now.Add(time.Minute)
	first.ch <- now
	waitWeatherRound(t, rounds)
	second := nextRecoveryTick(t, clock, 2*time.Minute)
	now = now.Add(2 * time.Minute)
	second.ch <- now
	waitWeatherRound(t, rounds)
	third := nextRecoveryTick(t, clock, 4*time.Minute)
	// The normal tick at minute five must not postpone the retry due at seven.
	now = now.Add(2 * time.Minute)
	normal.ch <- now
	waitWeatherRound(t, rounds)
	remaining := nextRecoveryTick(t, clock, 2*time.Minute)
	if !third.stopped.Load() {
		t.Fatal("replaced retry timer remained active")
	}
	now = now.Add(2 * time.Minute)
	remaining.ch <- now
	waitWeatherRound(t, rounds)
	// Daily has exhausted its budget. Current starts a new outage at minute ten.
	now = now.Add(3 * time.Minute)
	failCurrent = true
	normal.ch <- now
	waitWeatherRound(t, rounds)
	currentRetry := nextRecoveryTick(t, clock, time.Minute)
	now = now.Add(time.Minute)
	failCurrent = false
	currentRetry.ch <- now
	waitWeatherRound(t, rounds)
	if client.cache.status.Components.Current.Error != "" || !client.cache.status.Components.Current.UpdatedAt.Equal(now) {
		t.Fatal("persistent daily failure blocked current weather recovery")
	}
	mu.Lock()
	defer mu.Unlock()
	for path, want := range map[string]int{
		"/weather/v1/daily/22.54/114.06":        4,
		"/weather/v1/current/22.54/114.06":      3,
		"/v7/minutely/5m":                       2,
		"/weatheralert/v1/current/22.54/114.06": 3,
	} {
		if counts[path] != want {
			t.Fatalf("source %s requests = %d, want %d", path, counts[path], want)
		}
	}
}
