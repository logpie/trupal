package main

import (
	"sync"
	"time"
)

type Task struct {
	ID       string
	Fn       func()
	Interval time.Duration
	LastRun  time.Time
	stop     chan struct{}
}

type Scheduler struct {
	tasks map[string]*Task
	mu    sync.Mutex
}

func NewScheduler() *Scheduler {
	return &Scheduler{tasks: make(map[string]*Task)}
}

func (s *Scheduler) Add(id string, fn func(), interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := &Task{ID: id, Fn: fn, Interval: interval, stop: make(chan struct{})}
	s.tasks[id] = t
	go s.run(t)
}

func (s *Scheduler) run(t *Task) {
	ticker := time.NewTicker(t.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.Fn()
			t.LastRun = time.Now()
		case <-t.stop:
			return
		}
	}
}

func (s *Scheduler) Remove(id string) {
	t, ok := s.tasks[id]
	if ok {
		close(t.stop)
		delete(s.tasks, id)
	}
}

func (s *Scheduler) Pause(id string) {
	t, ok := s.tasks[id]
	if ok {
		close(t.stop)
	}
}

func (s *Scheduler) Resume(id string) {
	t, ok := s.tasks[id]
	if ok {
		t.stop = make(chan struct{})
		go s.run(t)
	}
}

func (s *Scheduler) StopAll() {
	for id, t := range s.tasks {
		close(t.stop)
		delete(s.tasks, id)
	}
}

func (s *Scheduler) RunNow(id string) {
	t, ok := s.tasks[id]
	if ok {
		t.Fn()
	}
}

func (s *Scheduler) List() []string {
	var ids []string
	for id := range s.tasks {
		ids = append(ids, id)
	}
	return ids
}

func (s *Scheduler) IsRunning(id string) bool {
	_, ok := s.tasks[id]
	return ok
}

func (s *Scheduler) Reschedule(id string, interval time.Duration) {
	t, ok := s.tasks[id]
	if ok {
		close(t.stop)
		t.Interval = interval
		t.stop = make(chan struct{})
		go s.run(t)
	}
}

func (s *Scheduler) Count() int {
	return len(s.tasks)
}
