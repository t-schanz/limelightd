package debounce

import (
	"context"
	"log/slog"
	"time"

	"limelightd/internal/events"
)

// Clock abstracts time sources so debouncer logic can be tested deterministically.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// SystemClock returns a Clock backed by the real system clock.
func SystemClock() Clock { return realClock{} }

type Config struct {
	OnDelay  time.Duration
	OffDelay time.Duration
}

type Debouncer struct {
	cfg   Config
	clock Clock
}

func New(cfg Config, clock Clock) *Debouncer {
	return &Debouncer{cfg: cfg, clock: clock}
}

// Run consumes CameraEvents on in and emits debounced LightIntents on out
// until ctx is done or in is closed.
//
// State model:
//   - emittedOn: the last stable intent we emitted (initial value: false).
//   - pending:   an event waiting for its debounce window.
//
// Rules:
//   - An event that matches emittedOn cancels any pending timer and does not emit.
//   - An event that contradicts emittedOn schedules emission after the appropriate delay.
//   - If a counter-event arrives before the timer fires, the pending timer is cancelled.
func (d *Debouncer) Run(ctx context.Context, in <-chan events.CameraEvent, out chan<- events.LightIntent) {
	emittedOn := false

	var pendingFire <-chan time.Time
	var pendingOn bool

	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-in:
			if !ok {
				return
			}
			wantOn := ev.Kind == events.Open
			if wantOn == emittedOn {
				// Cancels any pending transition back to the current state.
				if pendingFire != nil {
					slog.Debug("debouncer cancelled pending transition", "event", ev.Kind.String(), "emitted_on", emittedOn)
				}
				pendingFire = nil
				continue
			}
			// Schedule a transition.
			pendingOn = wantOn
			delay := d.cfg.OffDelay
			if wantOn {
				delay = d.cfg.OnDelay
			}
			slog.Debug("debouncer scheduled transition", "event", ev.Kind.String(), "want_on", wantOn, "delay", delay)
			pendingFire = d.clock.After(delay)

		case now := <-pendingFire:
			emittedOn = pendingOn
			pendingFire = nil
			slog.Info("debouncer emitted intent", "on", emittedOn)
			select {
			case out <- events.LightIntent{On: emittedOn, At: now}:
			case <-ctx.Done():
				return
			}
		}
	}
}
