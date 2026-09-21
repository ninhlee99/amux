package guard

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_ExecuteBatch(t *testing.T) {
	wp := NewWorkerPool(4, 64)
	defer wp.Close()

	const numTasks = 100
	var sum atomic.Int64

	tasks := make([]Task, numTasks)
	for i := 0; i < numTasks; i++ {
		val := int64(i + 1)
		tasks[i] = func() {
			sum.Add(val)
		}
	}

	wp.ExecuteBatch(tasks)

	expectedSum := int64(numTasks * (numTasks + 1) / 2)
	if sum.Load() != expectedSum {
		t.Fatalf("expected sum %d, got %d", expectedSum, sum.Load())
	}
	if wp.CompletedTasks() < uint64(numTasks) {
		t.Errorf("expected completed tasks >= %d, got %d", numTasks, wp.CompletedTasks())
	}
}

func TestWorkerPool_ConcurrencyLimit(t *testing.T) {
	const maxWorkers = 5
	wp := NewWorkerPool(maxWorkers, 100)
	defer wp.Close()

	var activeMax atomic.Int32
	var currentActive atomic.Int32
	var wg sync.WaitGroup

	const numTasks = 50
	wg.Add(numTasks)

	for i := 0; i < numTasks; i++ {
		wp.Submit(func() {
			defer wg.Done()
			cur := currentActive.Add(1)
			for {
				old := activeMax.Load()
				if cur > old {
					if activeMax.CompareAndSwap(old, cur) {
						break
					}
				} else {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			currentActive.Add(-1)
		})
	}

	wg.Wait()

	maxObserved := activeMax.Load()
	if maxObserved > maxWorkers {
		t.Errorf("observed concurrent workers (%d) exceeded max limit (%d)", maxObserved, maxWorkers)
	}
}

func TestWorkerPool_PanicRecovery(t *testing.T) {
	wp := NewWorkerPool(2, 10)
	defer wp.Close()

	done := make(chan bool)
	wp.Submit(func() {
		defer close(done)
		panic("simulated worker task panic")
	})

	select {
	case <-done:
		// Passed: task completed with recovery
	case <-time.After(500 * time.Millisecond):
		t.Fatal("worker pool hung after panic")
	}

	// Verify worker pool still processes subsequent tasks
	var followUp atomic.Bool
	wp.ExecuteBatch([]Task{
		func() {
			followUp.Store(true)
		},
	})

	if !followUp.Load() {
		t.Fatal("worker pool did not recover after panic to process next task")
	}
}

func TestWorkerPool_SessionTelemetry(t *testing.T) {
	wp := NewWorkerPool(4, 32)
	defer wp.Close()

	var wg sync.WaitGroup
	const sessionA = "session-claude-test"
	const sessionB = "session-gemini-test"

	wg.Add(4)
	for i := 0; i < 4; i++ {
		sess := sessionA
		if i%2 == 1 {
			sess = sessionB
		}
		wp.SubmitSession(sess, func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
		})
	}

	wg.Wait()

	statsA := wp.GetSessionStats(sessionA)
	if statsA.CompletedTasks != 2 {
		t.Errorf("expected 2 completed tasks for sessionA, got %d", statsA.CompletedTasks)
	}
	if statsA.TotalExecutionTimeMs < 0 {
		t.Errorf("expected non-negative execution time for sessionA, got %d", statsA.TotalExecutionTimeMs)
	}

	statsB := wp.GetSessionStats(sessionB)
	if statsB.CompletedTasks != 2 {
		t.Errorf("expected 2 completed tasks for sessionB, got %d", statsB.CompletedTasks)
	}

	all := wp.GetAllSessionStats()
	if len(all) != 2 {
		t.Errorf("expected 2 sessions in all stats, got %d", len(all))
	}
}
