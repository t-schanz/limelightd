package camera

import (
	"context"
	"sync"
	"testing"
	"time"

	"limelightd/internal/events"
)

// fakeSource emits arbitrary device paths to simulate udev `change` events.
type fakeSource struct {
	events chan string
}

func (f *fakeSource) Events() <-chan string { return f.events }
func (f *fakeSource) Close() error          { return nil }

// scriptedProbe returns true/false in the order given, per device.
type scriptedProbe struct {
	mu      sync.Mutex
	answers map[string][]bool
}

func (s *scriptedProbe) IsInUse(device string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seq := s.answers[device]
	if len(seq) == 0 {
		return false, nil
	}
	v := seq[0]
	s.answers[device] = seq[1:]
	return v, nil
}

func TestWatcher_EmitsOpenThenClose(t *testing.T) {
	source := &fakeSource{events: make(chan string, 8)}
	probe := &scriptedProbe{answers: map[string][]bool{
		"/dev/video0": {true, false},
	}}

	out := make(chan events.CameraEvent, 8)
	w := NewWatcher(source, probe)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, out)

	source.events <- "/dev/video0"
	select {
	case ev := <-out:
		if ev.Kind != events.Open {
			t.Errorf("first event = %v, want Open", ev.Kind)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for Open")
	}

	source.events <- "/dev/video0"
	select {
	case ev := <-out:
		if ev.Kind != events.Close {
			t.Errorf("second event = %v, want Close", ev.Kind)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for Close")
	}
}

func TestWatcher_DoesNotEmitOnUnchangedState(t *testing.T) {
	source := &fakeSource{events: make(chan string, 8)}
	probe := &scriptedProbe{answers: map[string][]bool{
		"/dev/video0": {false, false, false},
	}}

	out := make(chan events.CameraEvent, 8)
	w := NewWatcher(source, probe)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, out)

	for i := 0; i < 3; i++ {
		source.events <- "/dev/video0"
	}

	select {
	case ev := <-out:
		t.Fatalf("unexpected event: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWatcher_StartupScanEmitsOpenForInUseDevice(t *testing.T) {
	source := &fakeSource{events: make(chan string, 8)}
	probe := &scriptedProbe{answers: map[string][]bool{
		"/dev/video0": {true},
	}}

	out := make(chan events.CameraEvent, 8)
	w := NewWatcher(source, probe)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.RunWithInitialScan(ctx, out, []string{"/dev/video0"})

	select {
	case ev := <-out:
		if ev.Kind != events.Open {
			t.Errorf("initial event = %v, want Open", ev.Kind)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for startup Open")
	}
}
