package projektovemeeting

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	DefaultQueueWorkers      = 1
	DefaultQueuePollInterval = 500 * time.Millisecond
	DefaultQueueTaskTimeout  = 5 * time.Minute
)

// InferenceQueue is a SQLite-backed task queue. Workers poll the tasks table,
// atomically claim one pending task at a time and run the provided handler.
type InferenceQueue struct {
	repo     Repository
	workers  int
	interval time.Duration
	handler  func(ctx context.Context, job InferenceJob) error

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewInferenceQueue(repo Repository, workers int, interval time.Duration, handler func(ctx context.Context, job InferenceJob) error) *InferenceQueue {
	if workers < 1 {
		workers = 1
	}
	if interval <= 0 {
		interval = DefaultQueuePollInterval
	}
	return &InferenceQueue{
		repo:     repo,
		workers:  workers,
		interval: interval,
		handler:  handler,
	}
}

func (q *InferenceQueue) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	q.cancel = cancel

	if err := q.repo.ResetOrphanedTasks(ctx); err != nil {
		slog.Error("Failed to reset orphaned tasks", "err", err.Error())
	}

	q.wg.Add(q.workers)
	for i := 0; i < q.workers; i++ {
		go q.work(ctx)
	}
}

func (q *InferenceQueue) Stop() {
	if q.cancel == nil {
		return
	}
	q.cancel()
	q.wg.Wait()
}

func (q *InferenceQueue) work(ctx context.Context) {
	defer q.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		task, ok, err := q.repo.ClaimTask(ctx)
		if err != nil {
			slog.Error("Failed to claim task", "err", err.Error())
			if !q.sleep(ctx) {
				return
			}
			continue
		}
		if !ok {
			if !q.sleep(ctx) {
				return
			}
			continue
		}

		jobCtx, cancel := context.WithTimeout(ctx, DefaultQueueTaskTimeout)
		err = q.handler(jobCtx, task.Payload)
		cancel()

		status := TaskStatusDone
		errMsg := ""
		if err != nil {
			status = TaskStatusFailed
			errMsg = err.Error()
		}
		if err := q.repo.CompleteTask(ctx, task.ID, status, errMsg); err != nil {
			slog.Error("Failed to complete task", "task_id", task.ID, "err", err.Error())
		}
	}
}

func (q *InferenceQueue) sleep(ctx context.Context) bool {
	timer := time.NewTimer(q.interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
