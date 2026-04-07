package main

import (
	"sync"
)

type Worker struct {
	ID   int
	busy bool
}

type Pool struct {
	workers []*Worker
	queue   chan func()
	mu      sync.Mutex
}

func NewPool(size int) *Pool {
	p := &Pool{
		queue: make(chan func(), 100),
	}
	for i := 0; i < size; i++ {
		w := &Worker{ID: i}
		p.workers = append(p.workers, w)
		go p.run(w)
	}
	return p
}

func (p *Pool) run(w *Worker) {
	for fn := range p.queue {
		w.busy = true
		fn()
		w.busy = false
	}
}

func (p *Pool) Submit(fn func()) {
	p.queue <- fn
}

func (p *Pool) ActiveCount() int {
	count := 0
	for _, w := range p.workers {
		if w.busy {
			count++
		}
	}
	return count
}

func (p *Pool) Shutdown() {
	close(p.queue)
}

func (p *Pool) Resize(newSize int) {
	if newSize > len(p.workers) {
		for i := len(p.workers); i < newSize; i++ {
			w := &Worker{ID: i}
			p.workers = append(p.workers, w)
			go p.run(w)
		}
	}
}

func (p *Pool) Wait() {
	for {
		if p.ActiveCount() == 0 {
			return
		}
	}
}

func (p *Pool) Stats() map[string]int {
	return map[string]int{
		"total":  len(p.workers),
		"active": p.ActiveCount(),
		"queued": len(p.queue),
	}
}
