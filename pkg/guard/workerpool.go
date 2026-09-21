package guard

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Task represents an executable unit of work.
type Task func()

// SessionWorkerStats tracks worker pool telemetry for a specific session.
type SessionWorkerStats struct {
	ActiveWorkers        int    `json:"active_workers"`
	CompletedTasks       uint64 `json:"completed_tasks"`
	TotalExecutionTimeMs int64  `json:"total_execution_time_ms"`
}

type sessionTracker struct {
	activeWorkers atomic.Int32
	completed     atomic.Uint64
	execDuration  atomic.Int64 // nanoseconds
}

// WorkerPool manages a bounded pool of concurrent worker goroutines
// to process multiplexed tasks without unbounded goroutine growth or resource exhaustion.
type WorkerPool struct {
	maxWorkers     int
	taskQueue      chan Task
	activeWorkers  atomic.Int32
	completedTasks atomic.Uint64
	wg             sync.WaitGroup
	closed         atomic.Bool
	stopCh         chan struct{}

	sessionMu sync.RWMutex
	sessions  map[string]*sessionTracker
}

var (
	globalWorkerPool     *WorkerPool
	globalWorkerPoolOnce sync.Once
)

// GlobalWorkerPool returns the singleton worker pool instance.
func GlobalWorkerPool() *WorkerPool {
	globalWorkerPoolOnce.Do(func() {
		globalWorkerPool = NewWorkerPool(0, 0)
	})
	return globalWorkerPool
}

// NewWorkerPool initializes and starts a bounded worker pool.
func NewWorkerPool(maxWorkers int, queueSize int) *WorkerPool {
	if maxWorkers <= 0 {
		maxWorkers = runtime.NumCPU() * 2
		if maxWorkers < 4 {
			maxWorkers = 4
		}
		if maxWorkers > 32 {
			maxWorkers = 32
		}
	}
	if queueSize <= 0 {
		queueSize = 512
	}

	wp := &WorkerPool{
		maxWorkers: maxWorkers,
		taskQueue:  make(chan Task, queueSize),
		stopCh:     make(chan struct{}),
		sessions:   make(map[string]*sessionTracker),
	}

	for i := 0; i < maxWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}

	return wp
}

func (wp *WorkerPool) getSessionTracker(sessionID string) *sessionTracker {
	if sessionID == "" {
		return nil
	}
	wp.sessionMu.RLock()
	st, exists := wp.sessions[sessionID]
	wp.sessionMu.RUnlock()
	if exists {
		return st
	}

	wp.sessionMu.Lock()
	defer wp.sessionMu.Unlock()
	if st, exists = wp.sessions[sessionID]; exists {
		return st
	}
	st = &sessionTracker{}
	wp.sessions[sessionID] = st
	return st
}

// SubmitSession enqueues a task associated with a specific session ID for telemetry tracking.
func (wp *WorkerPool) SubmitSession(sessionID string, task Task) bool {
	if task == nil || wp.closed.Load() {
		return false
	}

	st := wp.getSessionTracker(sessionID)
	wrapped := func() {
		start := time.Now()
		if st != nil {
			st.activeWorkers.Add(1)
		}
		defer func() {
			dur := time.Since(start)
			if st != nil {
				st.activeWorkers.Add(-1)
				st.completed.Add(1)
				st.execDuration.Add(dur.Nanoseconds())
			}
		}()
		task()
	}

	return wp.Submit(wrapped)
}

// GetSessionStats returns telemetry statistics for a given session ID.
func (wp *WorkerPool) GetSessionStats(sessionID string) SessionWorkerStats {
	wp.sessionMu.RLock()
	st, exists := wp.sessions[sessionID]
	wp.sessionMu.RUnlock()

	if !exists || st == nil {
		return SessionWorkerStats{}
	}

	return SessionWorkerStats{
		ActiveWorkers:        int(st.activeWorkers.Load()),
		CompletedTasks:       st.completed.Load(),
		TotalExecutionTimeMs: st.execDuration.Load() / 1e6,
	}
}

// GetAllSessionStats returns a snapshot of worker metrics for all tracked sessions.
func (wp *WorkerPool) GetAllSessionStats() map[string]SessionWorkerStats {
	wp.sessionMu.RLock()
	defer wp.sessionMu.RUnlock()

	res := make(map[string]SessionWorkerStats, len(wp.sessions))
	for id, st := range wp.sessions {
		if st != nil {
			res[id] = SessionWorkerStats{
				ActiveWorkers:        int(st.activeWorkers.Load()),
				CompletedTasks:       st.completed.Load(),
				TotalExecutionTimeMs: st.execDuration.Load() / 1e6,
			}
		}
	}
	return res
}

func (wp *WorkerPool) worker() {
	defer wp.wg.Done()
	for {
		select {
		case <-wp.stopCh:
			// Drain remaining tasks before exiting
			for {
				select {
				case task, ok := <-wp.taskQueue:
					if !ok {
						return
					}
					wp.executeTask(task)
				default:
					return
				}
			}
		case task, ok := <-wp.taskQueue:
			if !ok {
				return
			}
			wp.executeTask(task)
		}
	}
}

func (wp *WorkerPool) executeTask(task Task) {
	if task == nil {
		return
	}
	wp.activeWorkers.Add(1)
	defer func() {
		wp.activeWorkers.Add(-1)
		wp.completedTasks.Add(1)
		_ = recover() // Catch any panic inside task to protect worker loop
	}()
	task()
}

// Submit enqueues a task non-blockingly. Returns false if the pool is closed or queue is full.
func (wp *WorkerPool) Submit(task Task) bool {
	if task == nil || wp.closed.Load() {
		return false
	}
	select {
	case wp.taskQueue <- task:
		return true
	default:
		// Queue full: execute inline to avoid dropping critical work, but protect concurrency
		wp.executeTask(task)
		return true
	}
}

// SubmitWait blocks until the task can be enqueued or executed.
func (wp *WorkerPool) SubmitWait(task Task) bool {
	if task == nil || wp.closed.Load() {
		return false
	}
	select {
	case <-wp.stopCh:
		return false
	case wp.taskQueue <- task:
		return true
	}
}

// ExecuteBatch dispatches multiple tasks across the worker pool and blocks until all finish.
func (wp *WorkerPool) ExecuteBatch(tasks []Task) {
	if len(tasks) == 0 {
		return
	}

	var batchWg sync.WaitGroup
	batchWg.Add(len(tasks))

	for _, t := range tasks {
		fn := t
		wrapped := func() {
			defer batchWg.Done()
			fn()
		}
		if !wp.Submit(wrapped) {
			// If submit failed, execute directly
			wrapped()
		}
	}

	batchWg.Wait()
}

// ActiveWorkers returns the current number of workers actively executing tasks.
func (wp *WorkerPool) ActiveWorkers() int {
	return int(wp.activeWorkers.Load())
}

// CompletedTasks returns the total number of tasks executed by this pool.
func (wp *WorkerPool) CompletedTasks() uint64 {
	return wp.completedTasks.Load()
}

// QueuedTasks returns the number of tasks currently waiting in the queue.
func (wp *WorkerPool) QueuedTasks() int {
	return len(wp.taskQueue)
}

// MaxWorkers returns the maximum worker capacity of this pool.
func (wp *WorkerPool) MaxWorkers() int {
	return wp.maxWorkers
}

// Close gracefully stops the worker pool and waits for active tasks to complete.
func (wp *WorkerPool) Close() {
	if wp.closed.CompareAndSwap(false, true) {
		close(wp.stopCh)
		close(wp.taskQueue)
		wp.wg.Wait()
	}
}
