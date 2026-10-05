package collector

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func waitRecoverySchedule(t *testing.T, clock *fakeClock, index int, want time.Duration) *fakeTicker {
	t.Helper()
	deadline := time.After(time.Second)
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for {
		clock.mu.Lock()
		if len(clock.tickers) > index {
			got, ticker := clock.durations[index], clock.tickers[index]
			clock.mu.Unlock()
			if got != want {
				t.Fatalf("next attempt waits %s, want %s", got, want)
			}
			return ticker
		}
		clock.mu.Unlock()
		select {
		case <-deadline:
			t.Fatal("next attempt was not scheduled")
		case <-poll.C:
		}
	}
}

func TestRecoveryJobBoundsEarlyRetriesAndResetsAfterSuccess(t *testing.T) {
	clock := &fakeClock{}
	var attempts atomic.Int32
	_, _, runner := runTestRunner(t, clock, Job{
		Name: "scrutiny", Every: 30 * time.Minute, MinimumEvery: 30 * time.Minute,
		Timeout: time.Second, RetryOnFailure: true,
		Run: func(context.Context) error {
			attempt := attempts.Add(1)
			if attempt == 5 {
				return nil
			}
			return errors.New("upstream not ready")
		},
	})
	normal := waitRecoverySchedule(t, clock, 0, 30*time.Minute)
	for index, delay := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute} {
		ticker := waitRecoverySchedule(t, clock, index+1, delay)
		if attempts.Load() != int32(index+1) {
			t.Fatalf("unexpected extra attempts: %d", attempts.Load())
		}
		ticker.ch <- time.Now()
	}
	// After the third early retry, a normal tick is needed. Success must
	// preserve that original ticker and give the next outage a new budget.
	deadline := time.After(time.Second)
	for attempts.Load() < 4 {
		select {
		case <-deadline:
			t.Fatal("third retry did not run")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	// Observe completion rather than just request start before advancing time.
	time.Sleep(10 * time.Millisecond)
	clock.mu.Lock()
	count := len(clock.tickers)
	clock.mu.Unlock()
	if count != 4 {
		t.Fatal("exhausted failure scheduled another early retry")
	}
	normal.ch <- time.Now()
	waitJobCompleted(t, runner, "scrutiny")
	if healthy, _, _ := runner.CollectionHealth(); !healthy || normal.stopped.Load() {
		t.Fatal("successful recovery did not clear health or preserve cadence")
	}
	normal.ch <- time.Now()
	waitRecoverySchedule(t, clock, 4, time.Minute)
}

func TestRecoveryJobStopsWhileWaitingWithoutAnotherRequest(t *testing.T) {
	clock := &fakeClock{}
	var attempts atomic.Int32
	cancel, done, _ := runTestRunner(t, clock, Job{
		Name: "weather", Every: 5 * time.Minute, Timeout: time.Second, RetryOnFailure: true,
		Run: func(context.Context) error { attempts.Add(1); return errors.New("offline") },
	})
	normal := waitRecoverySchedule(t, clock, 0, 5*time.Minute)
	ticker := waitRecoverySchedule(t, clock, 1, time.Minute)
	cancel()
	waitSignal(t, done, "recovery wait did not cancel")
	if attempts.Load() != 1 || !ticker.stopped.Load() || !normal.stopped.Load() {
		t.Fatal("cancelled recovery retried or left its timer running")
	}
}

func TestRecoveryNeverSpeedsUpAlreadyFrequentJob(t *testing.T) {
	clock := &fakeClock{}
	_, _, _ = runTestRunner(t, clock, Job{
		Name: "fast", Every: 15 * time.Second, MinimumEvery: 15 * time.Second,
		Timeout: time.Second, RetryOnFailure: true,
		Run: func(context.Context) error { return errors.New("offline") },
	})
	waitRecoverySchedule(t, clock, 0, 15*time.Second)
}

func TestRecoveryKeepsNormalTickerAnchoredWhileRequestIsRunning(t *testing.T) {
	clock := &fakeClock{}
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	_, _, runner := runTestRunner(t, clock, Job{
		Name: "healthy", Every: 30 * time.Minute, Timeout: time.Minute, RetryOnFailure: true,
		Run: func(ctx context.Context) error {
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		},
	})
	waitSignal(t, started, "request did not start")
	normal := waitRecoverySchedule(t, clock, 0, 30*time.Minute)
	close(release)
	waitJobCompleted(t, runner, "healthy")
	normal.ch <- time.Now()
	waitSignal(t, started, "normal anchored tick did not run recovered job")
	clock.mu.Lock()
	count := len(clock.tickers)
	clock.mu.Unlock()
	if count != 1 || normal.stopped.Load() {
		t.Fatal("healthy completion restarted the normal ticker")
	}
}
