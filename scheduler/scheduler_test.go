package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTasksRunAtCorrectFrequency(t *testing.T) {
	var countA, countB int32

	s := New(Config{
		Interval: 10 * time.Millisecond,
		Tasks: []TaskConfig{
			{
				Task:  NewFuncTask("every-tick", func(ctx context.Context) error { atomic.AddInt32(&countA, 1); return nil }),
				Every: 1,
			},
			{
				Task:  NewFuncTask("every-3", func(ctx context.Context) error { atomic.AddInt32(&countB, 1); return nil }),
				Every: 3,
			},
		},
	})

	s.Start()
	time.Sleep(105 * time.Millisecond)
	s.Stop()

	a := atomic.LoadInt32(&countA)
	b := atomic.LoadInt32(&countB)

	if a < 5 {
		t.Errorf("every-tick ran %d times, expected at least 5", a)
	}
	// every-3 should run on ticks 3, 6, 9 — at least 2 times in ~10 ticks
	if b < 2 {
		t.Errorf("every-3 ran %d times, expected at least 2", b)
	}
	if b >= a {
		t.Errorf("every-3 (%d) should run less than every-tick (%d)", b, a)
	}
}

func TestRunFirst(t *testing.T) {
	var ran int32

	s := New(Config{
		Interval: 10 * time.Millisecond,
		Tasks: []TaskConfig{
			{
				Task:     NewFuncTask("run-first", func(ctx context.Context) error { atomic.AddInt32(&ran, 1); return nil }),
				Every:    100, // would normally not run for a long time
				RunFirst: true,
			},
		},
	})

	s.Start()
	time.Sleep(25 * time.Millisecond) // just a couple ticks
	s.Stop()

	if atomic.LoadInt32(&ran) < 1 {
		t.Error("RunFirst task should have run on first tick")
	}
}

func TestGracefulShutdown(t *testing.T) {
	var count int32

	s := New(Config{
		Interval: 10 * time.Millisecond,
		Tasks: []TaskConfig{
			{Task: NewFuncTask("counter", func(ctx context.Context) error { atomic.AddInt32(&count, 1); return nil }), Every: 1},
		},
	})

	s.Start()
	time.Sleep(35 * time.Millisecond)
	s.Stop()

	countAfterStop := atomic.LoadInt32(&count)
	time.Sleep(30 * time.Millisecond)
	countLater := atomic.LoadInt32(&count)

	if countLater != countAfterStop {
		t.Errorf("task kept running after Stop: %d -> %d", countAfterStop, countLater)
	}
}

func TestErrorDoesNotStopOtherTasks(t *testing.T) {
	var goodCount int32

	s := New(Config{
		Interval: 10 * time.Millisecond,
		Tasks: []TaskConfig{
			{Task: NewFuncTask("bad", func(ctx context.Context) error { return errors.New("boom") }), Every: 1},
			{Task: NewFuncTask("good", func(ctx context.Context) error { atomic.AddInt32(&goodCount, 1); return nil }), Every: 1},
		},
	})

	s.Start()
	time.Sleep(55 * time.Millisecond)
	s.Stop()

	if atomic.LoadInt32(&goodCount) < 3 {
		t.Error("good task should have kept running despite bad task errors")
	}
}

func TestAddTaskAfterStart(t *testing.T) {
	var count int32

	s := New(Config{
		Interval: 10 * time.Millisecond,
	})

	s.Start()
	time.Sleep(15 * time.Millisecond)

	s.AddTask(TaskConfig{
		Task:  NewFuncTask("late", func(ctx context.Context) error { atomic.AddInt32(&count, 1); return nil }),
		Every: 1,
	})

	time.Sleep(55 * time.Millisecond)
	s.Stop()

	if atomic.LoadInt32(&count) < 2 {
		t.Errorf("dynamically added task ran %d times, expected at least 2", atomic.LoadInt32(&count))
	}
}

func TestFuncTask(t *testing.T) {
	ft := NewFuncTask("test-func", func(ctx context.Context) error { return nil })

	if ft.Name() != "test-func" {
		t.Errorf("Name() = %q, want %q", ft.Name(), "test-func")
	}
	if err := ft.Run(context.Background()); err != nil {
		t.Errorf("Run() returned error: %v", err)
	}

	errTask := NewFuncTask("err-func", func(ctx context.Context) error { return errors.New("fail") })
	if err := errTask.Run(context.Background()); err == nil {
		t.Error("expected error from err-func")
	}
}

func TestTickCount(t *testing.T) {
	s := New(Config{
		Interval: 10 * time.Millisecond,
		Tasks: []TaskConfig{
			{Task: NewFuncTask("noop", func(ctx context.Context) error { return nil }), Every: 1},
		},
	})

	if s.TickCount() != 0 {
		t.Errorf("TickCount before start = %d, want 0", s.TickCount())
	}

	s.Start()
	time.Sleep(55 * time.Millisecond)
	s.Stop()

	tc := s.TickCount()
	if tc < 3 {
		t.Errorf("TickCount = %d, expected at least 3", tc)
	}
}

func TestContextPassedToTasks(t *testing.T) {
	var mu sync.Mutex
	var ctxErr error

	s := New(Config{
		Interval: 10 * time.Millisecond,
		Tasks: []TaskConfig{
			{
				Task: NewFuncTask("ctx-check", func(ctx context.Context) error {
					mu.Lock()
					defer mu.Unlock()
					ctxErr = ctx.Err()
					return nil
				}),
				Every: 1,
			},
		},
	})

	s.Start()
	time.Sleep(25 * time.Millisecond)

	mu.Lock()
	if ctxErr != nil {
		t.Errorf("context should not be cancelled while running: %v", ctxErr)
	}
	mu.Unlock()

	s.Stop()
}

func TestDefaultEveryIsOne(t *testing.T) {
	var count int32

	s := New(Config{
		Interval: 10 * time.Millisecond,
		Tasks: []TaskConfig{
			{
				Task:  NewFuncTask("default-every", func(ctx context.Context) error { atomic.AddInt32(&count, 1); return nil }),
				Every: 0, // should default to 1
			},
		},
	})

	s.Start()
	time.Sleep(55 * time.Millisecond)
	s.Stop()

	if atomic.LoadInt32(&count) < 3 {
		t.Errorf("task with Every=0 ran %d times, expected at least 3 (should default to every tick)", atomic.LoadInt32(&count))
	}
}
