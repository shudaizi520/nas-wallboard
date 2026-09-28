package collector

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTicker struct {
	ch      chan time.Time
	stopped atomic.Bool
}

func (t *fakeTicker) C() <-chan time.Time { return t.ch }
func (t *fakeTicker) Stop()               { t.stopped.Store(true) }

type fakeClock struct {
	mu        sync.Mutex
	tickers   []*fakeTicker
	durations []time.Duration
}

func (c *fakeClock) NewTicker(duration time.Duration) Ticker {
	ticker := &fakeTicker{ch: make(chan time.Time, 16)}
	c.mu.Lock()
	c.tickers = append(c.tickers, ticker)
	c.durations = append(c.durations, duration)
	c.mu.Unlock()
	return ticker
}

func TestRunnerClampsUnsafeIntervalsToJobMinimum(t *testing.T) {
	clock := &fakeClock{}
	runs := make(chan struct{}, 1)
	_, _ = runTestRunner(t, clock, Job{Name: "safe", Every: time.Second, MinimumEvery: 15 * time.Second, Timeout: time.Second, Run: func(context.Context) error { runs <- struct{}{}; return nil }})
	waitSignal(t, runs, "job did not run")
	clock.mu.Lock()
	durations := append([]time.Duration(nil), clock.durations...)
	clock.mu.Unlock()
	if len(durations) != 1 || durations[0] != 15*time.Second {
		t.Fatalf("ticker durations = %#v", durations)
	}
}

func (c *fakeClock) tickAll() {
	c.mu.Lock()
	tickers := append([]*fakeTicker(nil), c.tickers...)
	c.mu.Unlock()
	for _, ticker := range tickers {
		ticker.ch <- time.Now()
	}
}

func waitSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func runTestRunner(t *testing.T, clock Clock, jobs ...Job) (context.CancelFunc, <-chan struct{}) {
	t.Helper()
	runner := NewRunner(jobs)
	runner.clock = clock
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		waitSignal(t, done, "runner did not stop")
	})
	return cancel, done
}

func TestRunnerDoesNotOverlapSlowJob(t *testing.T) {
	clock := &fakeClock{}
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	job := Job{Name: "slow", Every: time.Second, Timeout: time.Minute, Run: func(ctx context.Context) error {
		current := active.Add(1)
		if current > maximum.Load() {
			maximum.Store(current)
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		active.Add(-1)
		return nil
	}}
	_, _ = runTestRunner(t, clock, job)
	waitSignal(t, started, "job did not run immediately")
	clock.tickAll()
	clock.tickAll()
	time.Sleep(20 * time.Millisecond)
	if maximum.Load() != 1 || len(started) != 0 {
		t.Fatalf("slow job overlapped: max=%d queued-starts=%d", maximum.Load(), len(started))
	}
	close(release)
}

func TestRunnerSlowJobDoesNotBlockOtherJobs(t *testing.T) {
	clock := &fakeClock{}
	slowStarted := make(chan struct{}, 1)
	fastStarted := make(chan struct{}, 2)
	jobs := []Job{
		{Name: "slow", Every: time.Second, Timeout: time.Minute, Run: func(ctx context.Context) error {
			slowStarted <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		}},
		{Name: "fast", Every: time.Second, Timeout: time.Minute, Run: func(context.Context) error {
			fastStarted <- struct{}{}
			return nil
		}},
	}
	_, _ = runTestRunner(t, clock, jobs...)
	waitSignal(t, slowStarted, "slow job did not start")
	waitSignal(t, fastStarted, "fast job was blocked")
	clock.tickAll()
	waitSignal(t, fastStarted, "fast interval run was blocked")
}

func TestRunnerRunsImmediatelyThenOnInterval(t *testing.T) {
	clock := &fakeClock{}
	runs := make(chan struct{}, 3)
	job := Job{Name: "job", Every: time.Second, Timeout: time.Second, Run: func(context.Context) error {
		runs <- struct{}{}
		return nil
	}}
	_, _ = runTestRunner(t, clock, job)
	waitSignal(t, runs, "job did not run immediately")
	clock.tickAll()
	waitSignal(t, runs, "job did not run on interval")
}

func TestRunnerStopsOnContextCancel(t *testing.T) {
	clock := &fakeClock{}
	started := make(chan struct{}, 1)
	job := Job{Name: "job", Every: time.Second, Timeout: time.Minute, Run: func(ctx context.Context) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}}
	cancel, done := runTestRunner(t, clock, job)
	waitSignal(t, started, "job did not start")
	cancel()
	waitSignal(t, done, "runner did not return after cancellation")
}
