// Package scheduler provides a pluggable background task scheduler.
package scheduler

import (
	"context"
	"log"
	"sync"
	"time"
)

// Task is a background job that runs periodically.
type Task interface {
	Name() string
	Run(ctx context.Context) error
}

// FuncTask wraps a function as a Task.
type FuncTask struct {
	name string
	fn   func(ctx context.Context) error
}

// NewFuncTask creates a Task from a function.
func NewFuncTask(name string, fn func(ctx context.Context) error) *FuncTask {
	return &FuncTask{name: name, fn: fn}
}

func (f *FuncTask) Name() string                    { return f.name }
func (f *FuncTask) Run(ctx context.Context) error   { return f.fn(ctx) }

// TaskConfig wraps a Task with scheduling options.
type TaskConfig struct {
	Task     Task
	Every    int  // run every N ticks (1 = every tick, 96 = daily at 15min intervals)
	RunFirst bool // run immediately on first tick
}

// Config configures the scheduler.
type Config struct {
	Interval time.Duration // tick interval (default 15 min)
	Tasks    []TaskConfig
	Logger   *log.Logger // optional custom logger
}

// Scheduler runs tasks periodically.
type Scheduler struct {
	interval time.Duration
	logger   *log.Logger
	cancel   context.CancelFunc

	mu    sync.Mutex
	tasks []TaskConfig
	ticks int
}

// New creates a scheduler.
func New(cfg Config) *Scheduler {
	if cfg.Interval == 0 {
		cfg.Interval = 15 * time.Minute
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &Scheduler{
		interval: cfg.Interval,
		logger:   logger,
		tasks:    append([]TaskConfig{}, cfg.Tasks...),
	}
}

// AddTask adds a task after creation. Safe to call after Start.
func (s *Scheduler) AddTask(tc TaskConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, tc)
}

// TickCount returns the number of completed ticks.
func (s *Scheduler) TickCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ticks
}

// Start begins the scheduler loop in a goroutine.
func (s *Scheduler) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.run(ctx)
	s.logger.Printf("[scheduler] started with interval %v", s.interval)
}

// Stop shuts down the scheduler and waits for the current tick to finish.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.logger.Println("[scheduler] stopped")
}

func (s *Scheduler) run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.tick(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	s.mu.Lock()
	s.ticks++
	tick := s.ticks
	tasks := make([]TaskConfig, len(s.tasks))
	copy(tasks, s.tasks)
	s.mu.Unlock()

	for _, tc := range tasks {
		if ctx.Err() != nil {
			return
		}

		every := tc.Every
		if every <= 0 {
			every = 1
		}

		// On first tick, run if RunFirst is set OR if it's a regular interval match.
		if tick == 1 && tc.RunFirst {
			// always run
		} else if tick%every != 0 {
			continue
		}

		start := time.Now()
		if err := tc.Task.Run(ctx); err != nil {
			s.logger.Printf("[scheduler] task %q error: %v", tc.Task.Name(), err)
		} else {
			s.logger.Printf("[scheduler] task %q done in %v", tc.Task.Name(), time.Since(start))
		}
	}
}
