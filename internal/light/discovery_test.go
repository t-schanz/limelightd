package light

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestCachedSource_RefreshPopulatesCache(t *testing.T) {
	var calls int32
	discover := func(ctx context.Context, timeout time.Duration) ([]Light, error) {
		atomic.AddInt32(&calls, 1)
		return []Light{{Host: "10.0.0.1:9123"}}, nil
	}

	s := NewCachedSource(discover, time.Minute, 500*time.Millisecond)

	s.Refresh(context.Background())

	l := s.Lights()
	if len(l) != 1 || l[0].Host != "10.0.0.1:9123" {
		t.Fatalf("lights = %+v", l)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("discover called %d times, want 1", got)
	}

	// Further calls to Lights() should not re-query.
	for i := 0; i < 10; i++ {
		_ = s.Lights()
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("after Lights() calls: discover called %d times, want 1", got)
	}
}

func TestCachedSource_RunDoesNotRefreshImmediately(t *testing.T) {
	var calls int32
	discover := func(ctx context.Context, timeout time.Duration) ([]Light, error) {
		atomic.AddInt32(&calls, 1)
		return nil, nil
	}

	s := NewCachedSource(discover, time.Hour, 500*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	// With a 1-hour refresh interval and no Invalidate(), discover should not
	// be called during Run's lifetime.
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("discover called %d times, want 0 (Run must not auto-refresh)", got)
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

	s.Invalidate()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&calls) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := atomic.LoadInt32(&calls); got < 1 {
		t.Errorf("discover called %d times after invalidate, want >=1", got)
	}
}
