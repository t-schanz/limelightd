package light

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"limelightd/internal/events"
)

type staticSource struct {
	mu     sync.Mutex
	lights []Light
}

func (s *staticSource) Lights() []Light {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Light, len(s.lights))
	copy(out, s.lights)
	return out
}

func (s *staticSource) Invalidate() {} // noop

func TestController_SendsOnPayload(t *testing.T) {
	var (
		mu       sync.Mutex
		received []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/elgato/lights" {
			t.Errorf("path = %s, want /elgato/lights", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	src := &staticSource{lights: []Light{{Host: host}}}
	c := NewController(src, Preset{Brightness: 40, Temperature: 213}, 2*time.Second)

	in := make(chan events.LightIntent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { c.Run(ctx, in); close(done) }()

	in <- events.LightIntent{On: true, At: time.Now()}
	// Give the controller a moment to send.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("got %d requests, want 1", len(received))
	}

	var payload struct {
		NumberOfLights int `json:"numberOfLights"`
		Lights         []struct {
			On          int `json:"on"`
			Brightness  int `json:"brightness"`
			Temperature int `json:"temperature"`
		} `json:"lights"`
	}
	if err := json.Unmarshal([]byte(received[0]), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.NumberOfLights != 1 {
		t.Errorf("numberOfLights = %d", payload.NumberOfLights)
	}
	if len(payload.Lights) != 1 {
		t.Fatalf("len(lights) = %d", len(payload.Lights))
	}
	l := payload.Lights[0]
	if l.On != 1 || l.Brightness != 40 || l.Temperature != 213 {
		t.Errorf("payload = %+v, want on=1 brightness=40 temperature=213", l)
	}
}

func TestController_SendsOffPayload(t *testing.T) {
	var (
		mu       sync.Mutex
		received []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	src := &staticSource{lights: []Light{{Host: host}}}
	c := NewController(src, Preset{Brightness: 40, Temperature: 213}, 2*time.Second)

	in := make(chan events.LightIntent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx, in)

	in <- events.LightIntent{On: false, At: time.Now()}
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("got %d requests, want 1", len(received))
	}
	var payload struct {
		Lights []struct {
			On int `json:"on"`
		} `json:"lights"`
	}
	if err := json.Unmarshal([]byte(received[0]), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Lights[0].On != 0 {
		t.Errorf("on = %d, want 0", payload.Lights[0].On)
	}
}

func TestController_InvalidatesCacheOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	src := &staticSource{lights: []Light{{Host: host}}}
	invalidatingSrc := &invalidatingSource{staticSource: src}

	c := NewController(invalidatingSrc, Preset{Brightness: 40, Temperature: 213}, 500*time.Millisecond)

	in := make(chan events.LightIntent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx, in)

	in <- events.LightIntent{On: true, At: time.Now()}
	time.Sleep(300 * time.Millisecond)

	if !invalidatingSrc.wasInvalidated() {
		t.Error("expected Invalidate() to be called after HTTP failure")
	}
}

type invalidatingSource struct {
	*staticSource
	invMu       sync.Mutex
	invalidated bool
}

func (s *invalidatingSource) Invalidate() {
	s.invMu.Lock()
	defer s.invMu.Unlock()
	s.invalidated = true
}

func (s *invalidatingSource) wasInvalidated() bool {
	s.invMu.Lock()
	defer s.invMu.Unlock()
	return s.invalidated
}
