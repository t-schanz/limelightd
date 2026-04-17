package light

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestCachedSource_CachesResultsBetweenRefreshes(t *testing.T) {
	var calls int32
	discover := func(ctx context.Context, timeout time.Duration) ([]Light, error) {
		atomic.AddInt32(&calls, 1)
		return []Light{{Host: "10.0.0.1:9123"}}, nil
	}

	s := NewCachedSource(discover, time.Minute, 500*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	// Wait for initial discovery.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(s.Lights()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	l := s.Lights()
	if len(l) != 1 || l[0].Host != "10.0.0.1:9123" {
		t.Fatalf("lights = %+v", l)
	}

	// Further calls to Lights() should not re-query.
	for i := 0; i < 10; i++ {
		_ = s.Lights()
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("discover called %d times, want 1", got)
	}
}

func TestCachedSource_InvalidateForcesRefresh(t *testing.T) {
	var calls int32
	discover := func(ctx context.Context, timeout time.Duration) ([]Light, error) {
		atomic.AddInt32(&calls, 1)
		return []Light{{Host: "10.0.0.1:9123"}}, nil
	}

	s := NewCachedSource(discover, time.Hour, 500*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	// Wait for initial.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&calls) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	s.Invalidate()

	deadline = time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&calls) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := atomic.LoadInt32(&calls); got < 2 {
		t.Errorf("discover called %d times after invalidate, want >=2", got)
	}
}
