package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestConnectionPoolCloseRemovesClient(t *testing.T) {
	pool := NewConnectionPool(2)

	first := pool.Get("example.com")
	second := pool.Get("example.com")
	if first != second {
		t.Fatal("expected cached client for same host")
	}

	pool.Close("example.com")

	third := pool.Get("example.com")
	if third == first {
		t.Fatal("expected new client after Close")
	}
}

func TestRetryTransportUsesDefaultTransportWhenBaseNil(t *testing.T) {
	rt := &RetryTransport{}
	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	base := rt.base()
	if base != http.DefaultTransport {
		t.Fatal("expected nil Base to fall back to http.DefaultTransport")
	}

	if req.URL.Host != "example.com" {
		t.Fatalf("unexpected request host %q", req.URL.Host)
	}
}

func TestRetryTransportRejectsNonReplayableBodyOnRetry(t *testing.T) {
	rt := &RetryTransport{
		Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		}),
		MaxRetries: 1,
	}
	req, err := http.NewRequest(http.MethodPut, "http://example.com", io.NopCloser(strings.NewReader("hello")))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if _, err := rt.RoundTrip(req); err == nil {
		t.Fatal("expected retry transport to reject non-replayable request body")
	}
}

func TestRateLimiterRefillAccumulatesWholeSeconds(t *testing.T) {
	rl := &RateLimiter{
		tokens:     0,
		maxTokens:  10,
		refillRate: 2,
		lastRefill: time.Now().Add(-2500 * time.Millisecond),
	}
	rl.refill()
	if rl.tokens != 4 {
		t.Fatalf("tokens = %d, want 4", rl.tokens)
	}
}
