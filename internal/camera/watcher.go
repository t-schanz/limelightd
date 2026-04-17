package camera

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"limelightd/internal/events"
)

// Source supplies device paths for every udev `change` event on video4linux.
// The string emitted is the absolute device node path (e.g. "/dev/video0").
type Source interface {
	Events() <-chan string
	Close() error
}

type Watcher struct {
	source Source
	probe  DeviceProbe

	mu    sync.Mutex
	state map[string]bool // per-device: true = in use
}

func NewWatcher(source Source, probe DeviceProbe) *Watcher {
	return &Watcher{
		source: source,
		probe:  probe,
		state:  make(map[string]bool),
	}
}

// Run processes events until ctx is done.
func (w *Watcher) Run(ctx context.Context, out chan<- events.CameraEvent) {
	w.loop(ctx, out)
}

// RunWithInitialScan first probes each device in devices and emits Open events
// for any already in use, then runs Run.
func (w *Watcher) RunWithInitialScan(ctx context.Context, out chan<- events.CameraEvent, devices []string) {
	for _, d := range devices {
		inUse, err := w.probe.IsInUse(d)
		if err != nil {
			slog.Warn("initial probe failed", "device", d, "err", err)
			continue
		}
		w.mu.Lock()
		w.state[d] = inUse
		w.mu.Unlock()
		if inUse {
			select {
			case out <- events.CameraEvent{Kind: events.Open, At: time.Now()}:
			case <-ctx.Done():
				return
			}
		}
	}
	w.loop(ctx, out)
}

func (w *Watcher) loop(ctx context.Context, out chan<- events.CameraEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case device, ok := <-w.source.Events():
			if !ok {
				return
			}
			w.handle(ctx, device, out)
		}
	}
}

func (w *Watcher) handle(ctx context.Context, device string, out chan<- events.CameraEvent) {
	inUse, err := w.probe.IsInUse(device)
	if err != nil {
		slog.Warn("probe failed", "device", device, "err", err)
		return
	}
	w.mu.Lock()
	prev := w.state[device] // zero value (false) for unknown devices is the correct default
	w.state[device] = inUse
	w.mu.Unlock()

	if prev == inUse {
		return
	}
	kind := events.Close
	if inUse {
		kind = events.Open
	}
	select {
	case out <- events.CameraEvent{Kind: kind, At: time.Now()}:
	case <-ctx.Done():
	}
}
