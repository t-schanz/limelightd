package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"limelightd/internal/camera"
	"limelightd/internal/debounce"
	"limelightd/internal/events"
	"limelightd/internal/light"
)

// fakeUdev lets the test shove events through the pipeline.
type fakeUdev struct{ out chan string }

func (f *fakeUdev) Events() <-chan string { return f.out }
func (f *fakeUdev) Close() error          { return nil }

type stubProbe struct{ state atomic.Bool }

func (s *stubProbe) IsInUse(string) (bool, error) { return s.state.Load(), nil }

type staticLights struct {
	mu          sync.Mutex
	l           []light.Light
	invalidated atomic.Int32
}

func (s *staticLights) Lights() []light.Light {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]light.Light, len(s.l))
	copy(out, s.l)
	return out
}
func (s *staticLights) Invalidate() { s.invalidated.Add(1) }

func TestEndToEnd_OpenThenClose(t *testing.T) {
	var (
		mu       sync.Mutex
		payloads []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		payloads = append(payloads, string(body))
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	udev := &fakeUdev{out: make(chan string, 16)}
	probe := &stubProbe{}
	lights := &staticLights{l: []light.Light{{Host: strings.TrimPrefix(srv.URL, "http://")}}}

	watcher := camera.NewWatcher(udev, probe)
	rawEvents := make(chan events.CameraEvent, 16)
	intents := make(chan events.LightIntent, 16)

	deb := debounce.New(debounce.Config{
		OnDelay:  50 * time.Millisecond,
		OffDelay: 50 * time.Millisecond,
	}, debounce.SystemClock())

	controller := light.NewController(lights, light.Preset{Brightness: 42, Temperature: 200}, 1*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watcher.Run(ctx, rawEvents)
	go deb.Run(ctx, rawEvents, intents)
	go controller.Run(ctx, intents)

	// Open
	probe.state.Store(true)
	udev.out <- "/dev/video0"
	waitFor(t, 500*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(payloads) == 1
	}, "initial On payload")

	// Close
	probe.state.Store(false)
	udev.out <- "/dev/video0"
	waitFor(t, 500*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(payloads) == 2
	}, "Off payload")

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(payloads[0], `"on":1`) {
		t.Errorf("first payload not On: %s", payloads[0])
	}
	if !strings.Contains(payloads[0], `"brightness":42`) {
		t.Errorf("first payload missing brightness: %s", payloads[0])
	}
	if !strings.Contains(payloads[1], `"on":0`) {
		t.Errorf("second payload not Off: %s", payloads[1])
	}
}

func waitFor(t *testing.T, d time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", what)
}
