package collector

import (
	"context"
	"errors"
	"sync"
	"time"

	"example.com/nas-wallboard/internal/retry"
)

type Job struct {
	Name         string
	Every        time.Duration
	MinimumEvery time.Duration
	Timeout      time.Duration
	Run          func(context.Context) error
	// Only explicitly opted-in, low-frequency sources may retry early.
	RetryOnFailure bool
	// Optional per-source deadline for jobs containing independent subrequests.
	// Zero means no early retry is due; the normal ticker still runs.
	RetryAfter func() time.Duration
}

type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type Clock interface {
	NewTicker(time.Duration) Ticker
}

type Runner struct {
	jobs    []Job
	clock   Clock
	mu      sync.RWMutex
	results map[string]jobResult
}

var ErrSkipped = errors.New("cached poll; no upstream request")

type jobResult struct {
	success time.Time
	failed  bool
}

func (r *Runner) recordResult(name string, err error) {
	if errors.Is(err, ErrSkipped) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.results[name]
	value.failed = err != nil
	if err == nil {
		value.success = time.Now()
	}
	r.results[name] = value
}

func (r *Runner) CollectionHealth() (bool, string, time.Time) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var latest time.Time
	waiting, failed := false, false
	for _, job := range r.jobs {
		value := r.results[job.Name]
		if value.success.After(latest) {
			latest = value.success
		}
		every := max(job.Every, job.MinimumEvery)
		waiting = waiting || value.success.IsZero()
		failed = failed || value.failed || (!value.success.IsZero() && time.Since(value.success) > max(3*every, 3*time.Minute))
	}
	if failed {
		return false, "采集失败", latest
	}
	if waiting {
		return false, "等待首次采集", latest
	}
	return true, "采集正常", latest
}

func NewRunner(jobs []Job) *Runner {
	return NewRunnerWithClock(jobs, realClock{})
}

// NewRunnerWithClock lets independent source and scheduler clocks be supplied.
// The normal constructor always uses the system clock.
func NewRunnerWithClock(jobs []Job, clock Clock) *Runner {
	if clock == nil {
		clock = realClock{}
	}
	return &Runner{jobs: append([]Job(nil), jobs...), clock: clock, results: map[string]jobResult{}}
}

func (r *Runner) Run(ctx context.Context) error {
	var workers sync.WaitGroup
	for _, job := range r.jobs {
		if job.Run == nil || job.Every <= 0 || job.Timeout <= 0 {
			continue
		}
		workers.Add(1)
		go func(job Job) {
			defer workers.Done()
			r.runJob(ctx, job)
		}(job)
	}
	workers.Wait()
	return nil
}

func (r *Runner) runJob(ctx context.Context, job Job) {
	every := job.Every
	if job.MinimumEvery > every {
		every = job.MinimumEvery
	}
	ticker := r.clock.NewTicker(every)
	defer ticker.Stop()
	var retryTicker Ticker
	var retryC <-chan time.Time
	stopRetry := func() {
		if retryTicker != nil {
			retryTicker.Stop()
		}
		retryTicker, retryC = nil, nil
	}
	defer stopRetry()
	failures := 0

	done := make(chan error, 1)
	running := false
	start := func() {
		if running {
			return
		}
		running = true
		jobCtx, cancel := context.WithTimeout(ctx, job.Timeout)
		go func() {
			defer cancel()
			done <- job.Run(jobCtx)
		}()
	}

	start()
	for {
		select {
		case <-ctx.Done():
			if running {
				<-done
			}
			return
		case <-ticker.C():
			start()
		case <-retryC:
			stopRetry()
			start()
		case err := <-done:
			r.recordResult(job.Name, err)
			running = false
			if !job.RetryOnFailure || ctx.Err() != nil {
				continue
			}
			stopRetry()
			if err == nil {
				failures = 0
			} else if !errors.Is(err, ErrSkipped) {
				failures = min(4, failures+1)
			}
			delay := retry.Delay(every, failures)
			if job.RetryAfter != nil {
				// Composite jobs own independent retry budgets. Consult their
				// deadlines even when another component has exhausted its budget.
				delay = job.RetryAfter()
			}
			if delay > 0 && delay < every {
				retryTicker = r.clock.NewTicker(delay)
				retryC = retryTicker.C()
			}
		}
	}
}

type realClock struct{}

func (realClock) NewTicker(duration time.Duration) Ticker {
	return realTicker{ticker: time.NewTicker(duration)}
}

type realTicker struct {
	ticker *time.Ticker
}

func (t realTicker) C() <-chan time.Time { return t.ticker.C }
func (t realTicker) Stop()               { t.ticker.Stop() }
