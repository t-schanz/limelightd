package light

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

// DiscoverFunc runs one mDNS query and returns the Elgato lights found.
type DiscoverFunc func(ctx context.Context, timeout time.Duration) ([]Light, error)

// CachedSource runs a DiscoverFunc on an interval (and on demand via Invalidate)
// and caches the latest result for synchronous reads via Lights().
type CachedSource struct {
	discover        DiscoverFunc
	refreshInterval time.Duration
	queryTimeout    time.Duration

	mu     sync.RWMutex
	cached []Light

	invalidate chan struct{}
}

func NewCachedSource(discover DiscoverFunc, refresh, queryTimeout time.Duration) *CachedSource {
	return &CachedSource{
		discover:        discover,
		refreshInterval: refresh,
		queryTimeout:    queryTimeout,
		invalidate:      make(chan struct{}, 1),
	}
}

func (s *CachedSource) Lights() []Light {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Light, len(s.cached))
	copy(out, s.cached)
	return out
}

func (s *CachedSource) Invalidate() {
	select {
	case s.invalidate <- struct{}{}:
	default: // already signalled
	}
}

func (s *CachedSource) Run(ctx context.Context) {
	s.refresh(ctx)

	t := time.NewTicker(s.refreshInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.refresh(ctx)
		case <-s.invalidate:
			s.refresh(ctx)
		}
	}
}

func (s *CachedSource) refresh(ctx context.Context) {
	lights, err := s.discover(ctx, s.queryTimeout)
	if err != nil {
		slog.Warn("discovery failed", "err", err)
		return
	}
	s.mu.Lock()
	s.cached = lights
	s.mu.Unlock()
	slog.Info("discovery refreshed", "lights", len(lights))
}

// DiscoverElgato is the real mDNS-backed DiscoverFunc. It queries
// _elg._tcp.local. and returns the found Elgato lights.
func DiscoverElgato(ctx context.Context, timeout time.Duration) ([]Light, error) {
	entries := make(chan *mdns.ServiceEntry, 8)
	done := make(chan error, 1)

	go func() {
		params := &mdns.QueryParam{
			Service:             "_elg._tcp",
			Domain:              "local",
			Timeout:             timeout,
			Entries:             entries,
			WantUnicastResponse: false,
			DisableIPv6:         true,
		}
		done <- mdns.Query(params)
		close(entries)
	}()

	var lights []Light
collect:
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case e, ok := <-entries:
			if !ok {
				break collect
			}
			if e.AddrV4 == nil {
				continue
			}
			lights = append(lights, Light{
				Name: e.Name,
				Host: fmt.Sprintf("%s:%d", e.AddrV4.String(), e.Port),
			})
		}
	}

	if err := <-done; err != nil {
		return lights, err
	}
	return lights, nil
}
