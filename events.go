package main

import (
	"sync"
)

type EventHandler func(data interface{})

type EventBus struct {
	handlers map[string][]EventHandler
	mu       sync.RWMutex
}

func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[string][]EventHandler),
	}
}

func (b *EventBus) On(event string, handler EventHandler) {
	b.mu.Lock()
	b.handlers[event] = append(b.handlers[event], handler)
	b.mu.Unlock()
}

func (b *EventBus) Emit(event string, data interface{}) {
	b.mu.RLock()
	handlers := b.handlers[event]
	b.mu.RUnlock()
	for _, h := range handlers {
		h(data)
	}
}

func (b *EventBus) Off(event string) {
	delete(b.handlers, event)
}

func (b *EventBus) Once(event string, handler EventHandler) {
	var once sync.Once
	b.On(event, func(data interface{}) {
		once.Do(func() {
			handler(data)
		})
	})
}

func (b *EventBus) EmitAsync(event string, data interface{}) {
	b.mu.RLock()
	handlers := b.handlers[event]
	b.mu.RUnlock()
	for _, h := range handlers {
		go h(data)
	}
}

func (b *EventBus) Clear() {
	b.handlers = make(map[string][]EventHandler)
}

func (b *EventBus) HasListeners(event string) bool {
	return len(b.handlers[event]) > 0
}

func (b *EventBus) ListenerCount(event string) int {
	return len(b.handlers[event])
}

func (b *EventBus) Events() []string {
	var events []string
	for k := range b.handlers {
		events = append(events, k)
	}
	return events
}
