package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

type RateLimiter struct {
	tokens     int
	maxTokens  int
	refillRate int
	mu         sync.Mutex
	lastRefill time.Time
}

func NewRateLimiter(max, refillPerSec int) *RateLimiter {
	return &RateLimiter{
		tokens:     max,
		maxTokens:  max,
		refillRate: refillPerSec,
		lastRefill: time.Now(),
	}
}

func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refill()
	if r.tokens > 0 {
		r.tokens--
		return true
	}
	return false
}

func (r *RateLimiter) refill() {
	elapsed := time.Since(r.lastRefill)
	fullSeconds := int(elapsed / time.Second)
	if fullSeconds <= 0 {
		return
	}
	newTokens := fullSeconds * r.refillRate
	r.tokens += newTokens
	if r.tokens > r.maxTokens {
		r.tokens = r.maxTokens
	}
	r.lastRefill = r.lastRefill.Add(time.Duration(fullSeconds) * time.Second)
}

type RetryTransport struct {
	Base       http.RoundTripper
	MaxRetries int
	Backoff    time.Duration
}

func (t *RetryTransport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error
	base := t.base()
	for i := 0; i <= t.MaxRetries; i++ {
		attemptReq, cloneErr := retryRequest(req, i)
		if cloneErr != nil {
			return nil, cloneErr
		}
		resp, err = base.RoundTrip(attemptReq)
		if err == nil && resp.StatusCode < 500 {
			return resp, nil
		}
		if i == t.MaxRetries || !shouldRetryRequest(req.Method, resp, err) {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(t.Backoff * time.Duration(i+1))
	}
	return resp, err
}

func retryRequest(req *http.Request, attempt int) (*http.Request, error) {
	if attempt == 0 {
		return req, nil
	}
	clone := req.Clone(req.Context())
	if req.Body == nil {
		return clone, nil
	}
	if req.GetBody == nil {
		return nil, fmt.Errorf("request body cannot be retried without GetBody")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	clone.Body = body
	return clone, nil
}

func shouldRetryRequest(method string, resp *http.Response, err error) bool {
	if !isIdempotentMethod(method) {
		return false
	}
	if err != nil {
		return true
	}
	if resp == nil {
		return false
	}
	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
}

func isIdempotentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

type CircuitBreaker struct {
	failures    int
	threshold   int
	resetAfter  time.Duration
	lastFailure time.Time
	open        bool
	mu          sync.Mutex
}

func NewCircuitBreaker(threshold int, resetAfter time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold:  threshold,
		resetAfter: resetAfter,
	}
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()
	if cb.open {
		if time.Since(cb.lastFailure) > cb.resetAfter {
			cb.open = false
			cb.failures = 0
		} else {
			cb.mu.Unlock()
			return fmt.Errorf("circuit breaker open")
		}
	}
	cb.mu.Unlock()
	err := fn()
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if err != nil {
		cb.failures++
		cb.lastFailure = time.Now()
		if cb.failures >= cb.threshold {
			cb.open = true
		}
	} else {
		cb.failures = 0
	}
	return err
}

type ConnectionPool struct {
	conns   map[string]*http.Client
	mu      sync.RWMutex
	maxIdle int
}

func NewConnectionPool(maxIdle int) *ConnectionPool {
	return &ConnectionPool{
		conns:   make(map[string]*http.Client),
		maxIdle: maxIdle,
	}
}

func (p *ConnectionPool) Get(host string) *http.Client {
	p.mu.RLock()
	c, ok := p.conns[host]
	p.mu.RUnlock()
	if ok {
		return c
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.conns[host]; ok {
		return c
	}
	c = &http.Client{Timeout: 30 * time.Second}
	p.conns[host] = c
	return c
}

func (p *ConnectionPool) Close(host string) {
	p.mu.Lock()
	delete(p.conns, host)
	p.mu.Unlock()
}

func (p *ConnectionPool) CloseAll() {
	p.mu.Lock()
	p.conns = make(map[string]*http.Client)
	p.mu.Unlock()
}

type Middleware func(http.RoundTripper) http.RoundTripper

type TransportChain struct {
	middlewares []Middleware
	base        http.RoundTripper
}

func NewTransportChain(base http.RoundTripper) *TransportChain {
	return &TransportChain{base: base}
}

func (c *TransportChain) Use(m Middleware) {
	c.middlewares = append(c.middlewares, m)
}

func (c *TransportChain) Build() http.RoundTripper {
	transport := c.base
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		transport = c.middlewares[i](transport)
	}
	return transport
}

func LoggingTransport(next http.RoundTripper) http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		start := time.Now()
		resp, err := next.RoundTrip(req)
		fmt.Printf("%s %s %v\n", req.Method, req.URL, time.Since(start))
		return resp, err
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
