package debounce

import (
	"context"
	"testing"
	"time"

	"limelightd/internal/events"
)

// Test timings kept short so the suite runs fast. Margins are deliberately
// generous (>=2x delay) to tolerate scheduler jitter on loaded CI machines.
const (
	testOn  = 50 * time.Millisecond
	testOff = 200 * time.Millisecond
)

func newTestDebouncer(t *testing.T) (chan events.CameraEvent, chan events.LightIntent, context.CancelFunc) {
	t.Helper()
	in := make(chan events.CameraEvent, 8)
	out := make(chan events.LightIntent, 8)
	d := New(Config{OnDelay: testOn, OffDelay: testOff}, SystemClock())
	ctx, cancel := context.WithCancel(context.Background())
	go d.Run(ctx, in, out)
	return in, out, cancel
}

// expectNoEmit asserts that nothing arrives on out within window.
func expectNoEmit(t *testing.T, out <-chan events.LightIntent, window time.Duration) {
	t.Helper()
	select {
	case got := <-out:
		t.Fatalf("unexpected emit: %+v", got)
	case <-time.After(window):
	}
}

// expectEmit waits up to window for an emission and returns it.
func expectEmit(t *testing.T, out <-chan events.LightIntent, window time.Duration) events.LightIntent {
	t.Helper()
	select {
	case got := <-out:
		return got
	case <-time.After(window):
		t.Fatalf("timeout waiting for emission after %v", window)
		return events.LightIntent{}
	}
}

func TestDebouncer_OpenEmitsAfterOnDelay(t *testing.T) {
	in, out, cancel := newTestDebouncer(t)
	defer cancel()

	in <- events.CameraEvent{Kind: events.Open, At: time.Now()}

	// Before the window closes, nothing should have been emitted.
	expectNoEmit(t, out, testOn/2)

	// After the window, an On intent should arrive.
	got := expectEmit(t, out, 2*testOn)
	if !got.On {
		t.Errorf("expected On=true, got %+v", got)
	}
}

func TestDebouncer_QuickCloseCancelsOpen(t *testing.T) {
	in, out, cancel := newTestDebouncer(t)
	defer cancel()

	in <- events.CameraEvent{Kind: events.Open, At: time.Now()}
	// Close well within the on-debounce window.
	time.Sleep(testOn / 5)
	in <- events.CameraEvent{Kind: events.Close, At: time.Now()}

	// State was never flipped, so no intent should ever fire even after
	// well past both windows.
	expectNoEmit(t, out, 3*testOff)
}

func TestDebouncer_CloseAfterOnEmitsOff(t *testing.T) {
	in, out, cancel := newTestDebouncer(t)
	defer cancel()

	in <- events.CameraEvent{Kind: events.Open, At: time.Now()}
	onIntent := expectEmit(t, out, 2*testOn)
	if !onIntent.On {
		t.Fatalf("expected On first, got %+v", onIntent)
	}

	in <- events.CameraEvent{Kind: events.Close, At: time.Now()}
	// Off should not arrive before the off window.
	expectNoEmit(t, out, testOff/2)

	// After the off window, Off should arrive.
	offIntent := expectEmit(t, out, 2*testOff)
	if offIntent.On {
		t.Errorf("expected On=false, got %+v", offIntent)
	}
}

func TestDebouncer_QuickReopenCancelsClose(t *testing.T) {
	in, out, cancel := newTestDebouncer(t)
	defer cancel()

	// Get into the On state.
	in <- events.CameraEvent{Kind: events.Open, At: time.Now()}
	if _, ok := <-out; !ok {
		t.Fatal("on channel closed prematurely")
	}

	// Close, then re-open well within the off window.
	in <- events.CameraEvent{Kind: events.Close, At: time.Now()}
	time.Sleep(testOff / 5)
	in <- events.CameraEvent{Kind: events.Open, At: time.Now()}

	// No further emissions should arrive — state stayed On throughout.
	expectNoEmit(t, out, 3*testOff)
}
