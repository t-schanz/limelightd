package camera

import (
	"context"
	"sync"
	"testing"
	"time"

	"limelightd/internal/events"
)

// scriptedProbe returns a sequence of bool answers per device, one per call.
// Once a device's sequence is exhausted, the last value is repeated.
type scriptedProbe struct {
	mu      sync.Mutex
	answers map[string][]bool
	last    map[string]bool
}

func newScriptedProbe(script map[string][]bool) *scriptedProbe {
	return &scriptedProbe{answers: script, last: make(map[string]bool)}
}

func (s *scriptedProbe) IsInUse(device string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seq := s.answers[device]
	if len(seq) == 0 {
		return s.last[device], nil
	}
	v := seq[0]
	s.answers[device] = seq[1:]
	s.last[device] = v
	return v, nil
}

func TestPollingWatcher_EmitsOpenWhenDeviceBecomesInUse(t *testing.T) {
	probe := newScriptedProbe(map[string][]bool{
		"/dev/video0": {false, true},
	})
	out := make(chan events.CameraEvent, 8)
	w := NewPollingWatcher(probe, []string{"/dev/video0"}, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, out)

	select {
	case ev := <-out:
		if ev.Kind != events.Open {
			t.Errorf("event = %v, want Open", ev.Kind)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for Open event")
	}
}

func TestPollingWatcher_EmitsCloseWhenDeviceFreed(t *testing.T) {
	probe := newScriptedProbe(map[string][]bool{
		"/dev/video0": {true, false},
	})
	out := make(chan events.CameraEvent, 8)
	w := NewPollingWatcher(probe, []string{"/dev/video0"}, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, out)

	first := <-out
	if first.Kind != events.Open {
		t.Fatalf("first event = %v, want Open", first.Kind)
	}

	select {
	case ev := <-out:
		if ev.Kind != events.Close {
			t.Errorf("event = %v, want Close", ev.Kind)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for Close event")
	}
}

func TestPollingWatcher_DoesNotEmitOnUnchangedState(t *testing.T) {
	probe := newScriptedProbe(map[string][]bool{
		"/dev/video0": {false, false, false, false},
	})
	out := make(chan events.CameraEvent, 8)
	w := NewPollingWatcher(probe, []string{"/dev/video0"}, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, out)

	select {
	case ev := <-out:
		t.Fatalf("unexpected event: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPollingWatcher_InitialScanEmitsOpenForAlreadyInUseDevice(t *testing.T) {
	probe := newScriptedProbe(map[string][]bool{
		"/dev/video0": {true},
	})
	out := make(chan events.CameraEvent, 8)
	w := NewPollingWatcher(probe, []string{"/dev/video0"}, time.Hour) // long interval; only initial scan runs

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, out)

	select {
	case ev := <-out:
		if ev.Kind != events.Open {
			t.Errorf("initial event = %v, want Open", ev.Kind)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for initial Open event")
	}
}

func TestPollingWatcher_MultipleDevicesIndependentState(t *testing.T) {
	probe := newScriptedProbe(map[string][]bool{
		"/dev/video0": {true, true, false},
		"/dev/video1": {false, true, true},
	})
	out := make(chan events.CameraEvent, 8)
	w := NewPollingWatcher(probe, []string{"/dev/video0", "/dev/video1"}, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, out)

	// Expect Open(video0), then Open(video1), then Close(video0).
	// Order within a scan tick follows device-list order.
	kinds := []events.EventKind{}
	deadline := time.After(1 * time.Second)
	for len(kinds) < 3 {
		select {
		case ev := <-out:
			kinds = append(kinds, ev.Kind)
		case <-deadline:
			t.Fatalf("got %d events, want 3: %v", len(kinds), kinds)
		}
	}
	want := []events.EventKind{events.Open, events.Open, events.Close}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("kinds[%d] = %v, want %v", i, kinds[i], want[i])
		}
	}
}
