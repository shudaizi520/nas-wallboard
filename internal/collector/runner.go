package collector

import (
	"context"
	"sync"
	"time"
)

type Job struct {
	Name         string
	Every        time.Duration
	MinimumEvery time.Duration
	Timeout      time.Duration
	Run          func(context.Context) error
}

type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type Clock interface {
	NewTicker(time.Duration) Ticker
}

type Runner struct {
	jobs  []Job
	clock Clock
}

func NewRunner(jobs []Job) *Runner {
	return &Runner{jobs: append([]Job(nil), jobs...), clock: realClock{}}
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
		case <-done:
			running = false
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
