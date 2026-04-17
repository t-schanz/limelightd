# limelightd Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `limelightd` — a single-binary Go daemon that detects Linux webcam open/close via udev and toggles all Elgato Key Lights on the local network to a configured preset.

**Architecture:** Three in-process components (Camera Watcher → Debouncer → Light Controller) wired via Go channels. udev netlink for events (zero polling), `/proc/*/fd/*` scan to resolve open/close direction, mDNS for light discovery, HTTP PUT to port 9123 for control. Runs as a systemd user service.

**Tech Stack:**
- Go 1.22+ (stdlib `log/slog`, `net/http`, `testing`)
- `github.com/BurntSushi/toml` — TOML config parsing
- `github.com/pilebones/go-udev/netlink` — pure-Go udev netlink monitoring
- `github.com/hashicorp/mdns` — mDNS client for `_elg._tcp.local.` discovery

---

## Working Directory & Project Root

All paths in this plan are relative to the git repository root: `/home/tobias/PycharmProjects/PythonProject/PythonProject/`. The Go module lives at the repo root (module name: `limelightd`).

---

## File Structure

Files created by this plan:

| Path | Responsibility |
| --- | --- |
| `go.mod`, `go.sum` | Module declaration and dependency locks |
| `cmd/limelightd/main.go` | Entry point: config load, component wiring, signal handling |
| `internal/config/config.go` | TOML config types, loader, defaults |
| `internal/config/config_test.go` | Config loader tests |
| `internal/debounce/debounce.go` | Debouncer: raw events → stable intents |
| `internal/debounce/debounce_test.go` | Debouncer tests |
| `internal/camera/probe.go` | `DeviceProbe` interface + `/proc/*/fd/*` scan implementation |
| `internal/camera/probe_test.go` | Probe tests (fake procfs) |
| `internal/camera/watcher.go` | udev subscription, state resolution, startup scan |
| `internal/camera/watcher_test.go` | Watcher tests (injected event source) |
| `internal/light/controller.go` | HTTP PUT logic; consumes LightIntents |
| `internal/light/controller_test.go` | Controller tests (httptest.Server) |
| `internal/light/discovery.go` | mDNS discovery + cache |
| `internal/light/discovery_test.go` | Discovery cache/refresh tests (mDNS behavior is manual) |
| `cmd/limelightd/main_test.go` | End-to-end integration test with fakes |
| `systemd/limelightd.service` | systemd user service unit |
| `README.md` | Install instructions + manual smoke test |
| `.gitignore` | Ignore build artifacts |

---

## Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `README.md` (stub)

- [ ] **Step 1.1: Initialize Go module**

Run in repo root:

```bash
cd /home/tobias/PycharmProjects/PythonProject/PythonProject
go mod init limelightd
```

Expected: creates `go.mod` with `module limelightd` and `go 1.22` (or newer).

- [ ] **Step 1.2: Write .gitignore**

Create `.gitignore`:

```gitignore
# Build artifacts
/limelightd
/dist/
*.test
*.out

# IDE (allow .idea since the repo already tracks it)

# OS
.DS_Store
```

- [ ] **Step 1.3: Write README stub**

Create `README.md`:

```markdown
# limelightd

A tiny Linux daemon that turns your Elgato Key Lights on when your webcam opens and off when it closes. Event-driven, zero polling, ~3 MB RAM idle.

## Status

In development. See [`docs/superpowers/specs/2026-04-17-limelightd-design.md`](docs/superpowers/specs/2026-04-17-limelightd-design.md) for the design.

## Build

    go build -o limelightd ./cmd/limelightd

## Install (after build)

    install -Dm755 limelightd ~/.local/bin/limelightd
    install -Dm644 systemd/limelightd.service ~/.config/systemd/user/limelightd.service
    systemctl --user daemon-reload
    systemctl --user enable --now limelightd
```

- [ ] **Step 1.4: Verify the module compiles (empty module)**

Run:

```bash
go build ./...
```

Expected: exits 0 with no output (no Go files yet, which is fine).

- [ ] **Step 1.5: Commit**

```bash
git add go.mod .gitignore README.md
git commit -m "chore: scaffold limelightd Go module"
```

---

## Task 2: Config Loader

The config layer is pure logic, no external dependencies. Start here to establish types used by later tasks.

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 2.1: Add TOML dependency**

Run:

```bash
go get github.com/BurntSushi/toml@latest
```

Expected: adds to `go.mod` and `go.sum`.

- [ ] **Step 2.2: Write failing test for default config**

Create `internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	c := Defaults()

	if c.Light.Brightness != 20 {
		t.Errorf("Brightness = %d, want 20", c.Light.Brightness)
	}
	if c.Light.Temperature != 213 {
		t.Errorf("Temperature = %d, want 213", c.Light.Temperature)
	}
	if c.Debounce.OnMs != 200 {
		t.Errorf("OnMs = %d, want 200", c.Debounce.OnMs)
	}
	if c.Debounce.OffMs != 1000 {
		t.Errorf("OffMs = %d, want 1000", c.Debounce.OffMs)
	}
	if c.Discovery.RefreshInterval != 5*time.Minute {
		t.Errorf("RefreshInterval = %v, want 5m", c.Discovery.RefreshInterval)
	}
}

func TestLoad_MissingFileReturnsDefaults(t *testing.T) {
	c, err := Load("/nonexistent/path.toml")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if c.Light.Brightness != 20 {
		t.Errorf("expected defaults, got Brightness=%d", c.Light.Brightness)
	}
}

func TestLoad_ParsesOverrides(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	body := `
[light]
brightness = 75
temperature = 180

[debounce]
on_ms = 50
off_ms = 500

[discovery]
refresh_interval = "2m"
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Light.Brightness != 75 {
		t.Errorf("Brightness = %d, want 75", c.Light.Brightness)
	}
	if c.Light.Temperature != 180 {
		t.Errorf("Temperature = %d, want 180", c.Light.Temperature)
	}
	if c.Debounce.OnMs != 50 {
		t.Errorf("OnMs = %d, want 50", c.Debounce.OnMs)
	}
	if c.Debounce.OffMs != 500 {
		t.Errorf("OffMs = %d, want 500", c.Debounce.OffMs)
	}
	if c.Discovery.RefreshInterval != 2*time.Minute {
		t.Errorf("RefreshInterval = %v, want 2m", c.Discovery.RefreshInterval)
	}
}

func TestLoad_MalformedReturnsError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte("this is not = valid = toml ="), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(p); err == nil {
		t.Fatal("expected error on malformed TOML, got nil")
	}
}
```

- [ ] **Step 2.3: Run tests to confirm they fail**

Run:

```bash
go test ./internal/config/...
```

Expected: FAIL with compile errors (`Config`, `Load`, `Defaults` undefined).

- [ ] **Step 2.4: Implement config.go**

Create `internal/config/config.go`:

```go
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Light     LightConfig     `toml:"light"`
	Debounce  DebounceConfig  `toml:"debounce"`
	Discovery DiscoveryConfig `toml:"discovery"`
}

type LightConfig struct {
	Brightness  int `toml:"brightness"`
	Temperature int `toml:"temperature"`
}

type DebounceConfig struct {
	OnMs  int `toml:"on_ms"`
	OffMs int `toml:"off_ms"`
}

type DiscoveryConfig struct {
	RefreshInterval time.Duration `toml:"refresh_interval"`
}

func Defaults() Config {
	return Config{
		Light:     LightConfig{Brightness: 20, Temperature: 213},
		Debounce:  DebounceConfig{OnMs: 200, OffMs: 1000},
		Discovery: DiscoveryConfig{RefreshInterval: 5 * time.Minute},
	}
}

// Load reads a TOML config file from path and merges it over defaults.
// A missing file is not an error — defaults are returned.
// A malformed file returns an error.
func Load(path string) (Config, error) {
	c := Defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return c, nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	if _, err := toml.Decode(string(data), &c); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return c, nil
}
```

Note: `time.Duration` parses TOML strings like `"5m"` automatically via BurntSushi/toml's built-in `time.Duration` support.

- [ ] **Step 2.5: Run tests to confirm they pass**

Run:

```bash
go test ./internal/config/... -v
```

Expected: all four tests PASS.

- [ ] **Step 2.6: Commit**

```bash
git add go.mod go.sum internal/config/
git commit -m "feat(config): TOML loader with defaults"
```

---

## Task 3: Event Types (Shared)

Define the event types used across debouncer, watcher, and controller in a small shared package so the packages don't import each other.

**Files:**
- Create: `internal/events/events.go`

- [ ] **Step 3.1: Write events.go**

Create `internal/events/events.go`:

```go
package events

import "time"

// EventKind describes whether the camera was opened or closed.
type EventKind int

const (
	Open EventKind = iota + 1
	Close
)

func (k EventKind) String() string {
	switch k {
	case Open:
		return "open"
	case Close:
		return "close"
	default:
		return "unknown"
	}
}

// CameraEvent is a raw camera state change emitted by the watcher.
type CameraEvent struct {
	Kind EventKind
	At   time.Time
}

// LightIntent is a stable, debounced instruction for the light controller.
type LightIntent struct {
	On bool
	At time.Time
}
```

- [ ] **Step 3.2: Verify it compiles**

Run:

```bash
go build ./...
```

Expected: exits 0.

- [ ] **Step 3.3: Commit**

```bash
git add internal/events/
git commit -m "feat(events): shared CameraEvent and LightIntent types"
```

---

## Task 4: Debouncer

Pure logic: consumes `CameraEvent`s on an input channel, emits `LightIntent`s on an output channel after the debounce window. Uses a `Clock` interface so tests don't depend on real time.

**Files:**
- Create: `internal/debounce/debounce.go`
- Create: `internal/debounce/debounce_test.go`

- [ ] **Step 4.1: Write failing test for immediate passthrough when no contradiction**

Create `internal/debounce/debounce_test.go`:

```go
package debounce

import (
	"context"
	"testing"
	"time"

	"limelightd/internal/events"
)

// fakeClock lets tests advance time deterministically.
type fakeClock struct {
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	c      *fakeClock
	fireAt time.Time
	ch     chan time.Time
	fired  bool
	stopped bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(0, 0)}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	t := &fakeTimer{
		c:      c,
		fireAt: c.now.Add(d),
		ch:     make(chan time.Time, 1),
	}
	c.timers = append(c.timers, t)
	return t.ch
}

func (c *fakeClock) advance(d time.Duration) {
	c.now = c.now.Add(d)
	for _, t := range c.timers {
		if !t.fired && !t.stopped && !c.now.Before(t.fireAt) {
			t.fired = true
			t.ch <- c.now
		}
	}
}

func TestDebouncer_OpenEmitsAfterOnDelay(t *testing.T) {
	clk := newFakeClock()
	in := make(chan events.CameraEvent, 8)
	out := make(chan events.LightIntent, 8)

	d := New(Config{OnDelay: 200 * time.Millisecond, OffDelay: 1 * time.Second}, clk)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx, in, out)

	in <- events.CameraEvent{Kind: events.Open, At: clk.Now()}
	clk.advance(199 * time.Millisecond)

	select {
	case intent := <-out:
		t.Fatalf("emitted too early: %+v", intent)
	case <-time.After(20 * time.Millisecond):
	}

	clk.advance(2 * time.Millisecond) // cross the 200ms threshold
	select {
	case intent := <-out:
		if !intent.On {
			t.Errorf("expected On=true, got %+v", intent)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for intent")
	}
}

func TestDebouncer_QuickCloseCancelsOpen(t *testing.T) {
	clk := newFakeClock()
	in := make(chan events.CameraEvent, 8)
	out := make(chan events.LightIntent, 8)

	d := New(Config{OnDelay: 200 * time.Millisecond, OffDelay: 1 * time.Second}, clk)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx, in, out)

	in <- events.CameraEvent{Kind: events.Open, At: clk.Now()}
	clk.advance(50 * time.Millisecond)
	in <- events.CameraEvent{Kind: events.Close, At: clk.Now()}

	// Advance well past both windows; neither On nor Off should fire because
	// state never left the resting "off" value.
	clk.advance(5 * time.Second)

	select {
	case intent := <-out:
		t.Fatalf("no intent expected, got %+v", intent)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDebouncer_CloseAfterOnEmitsOff(t *testing.T) {
	clk := newFakeClock()
	in := make(chan events.CameraEvent, 8)
	out := make(chan events.LightIntent, 8)

	d := New(Config{OnDelay: 200 * time.Millisecond, OffDelay: 1 * time.Second}, clk)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx, in, out)

	// 1. Open → On after 200 ms
	in <- events.CameraEvent{Kind: events.Open, At: clk.Now()}
	clk.advance(201 * time.Millisecond)
	<-out // consume the On intent

	// 2. Close → Off after 1000 ms
	in <- events.CameraEvent{Kind: events.Close, At: clk.Now()}
	clk.advance(999 * time.Millisecond)
	select {
	case <-out:
		t.Fatal("Off emitted too early")
	case <-time.After(20 * time.Millisecond):
	}

	clk.advance(2 * time.Millisecond)
	select {
	case intent := <-out:
		if intent.On {
			t.Errorf("expected On=false, got %+v", intent)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for off intent")
	}
}

func TestDebouncer_QuickReopenCancelsClose(t *testing.T) {
	clk := newFakeClock()
	in := make(chan events.CameraEvent, 8)
	out := make(chan events.LightIntent, 8)

	d := New(Config{OnDelay: 200 * time.Millisecond, OffDelay: 1 * time.Second}, clk)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx, in, out)

	// Open → On
	in <- events.CameraEvent{Kind: events.Open, At: clk.Now()}
	clk.advance(201 * time.Millisecond)
	<-out

	// Close then re-open within the off-debounce window
	in <- events.CameraEvent{Kind: events.Close, At: clk.Now()}
	clk.advance(500 * time.Millisecond)
	in <- events.CameraEvent{Kind: events.Open, At: clk.Now()}

	// Past the full off window — no Off should fire because we re-opened.
	clk.advance(2 * time.Second)
	select {
	case intent := <-out:
		t.Fatalf("unexpected intent: %+v", intent)
	case <-time.After(50 * time.Millisecond):
	}
}
```

- [ ] **Step 4.2: Run tests to confirm they fail**

Run:

```bash
go test ./internal/debounce/...
```

Expected: FAIL, `New`, `Config`, `Debouncer.Run` undefined.

- [ ] **Step 4.3: Implement debounce.go**

Create `internal/debounce/debounce.go`:

```go
package debounce

import (
	"context"
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
				pendingFire = nil
				continue
			}
			// Schedule a transition.
			pendingOn = wantOn
			delay := d.cfg.OffDelay
			if wantOn {
				delay = d.cfg.OnDelay
			}
			pendingFire = d.clock.After(delay)

		case now := <-pendingFire:
			emittedOn = pendingOn
			pendingFire = nil
			select {
			case out <- events.LightIntent{On: emittedOn, At: now}:
			case <-ctx.Done():
				return
			}
		}
	}
}
```

- [ ] **Step 4.4: Run tests to confirm they pass**

Run:

```bash
go test ./internal/debounce/... -v
```

Expected: all four tests PASS.

- [ ] **Step 4.5: Commit**

```bash
git add internal/debounce/ internal/events/
git commit -m "feat(debounce): debouncer with injectable clock"
```

---

## Task 5: Light Controller

Consumes `LightIntent`s and sends HTTP PUTs to every known light. Discovery is injected as a simple source that returns the current known lights.

**Files:**
- Create: `internal/light/controller.go`
- Create: `internal/light/controller_test.go`

- [ ] **Step 5.1: Write failing test**

Create `internal/light/controller_test.go`:

```go
package light

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	u, _ := url.Parse(srv.URL)
	_ = u

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

	if !invalidatingSrc.invalidated {
		t.Error("expected Invalidate() to be called after HTTP failure")
	}
}

type invalidatingSource struct {
	*staticSource
	invalidated bool
}

func (s *invalidatingSource) Invalidate() { s.invalidated = true }
```

- [ ] **Step 5.2: Run tests to confirm they fail**

Run:

```bash
go test ./internal/light/...
```

Expected: FAIL with compile errors (types/functions undefined).

- [ ] **Step 5.3: Implement controller.go**

Create `internal/light/controller.go`:

```go
package light

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"limelightd/internal/events"
)

// Light is a discovered Elgato light endpoint.
type Light struct {
	Name string // optional, for logging
	Host string // "ip:port", e.g. "192.168.1.42:9123"
}

// Preset is the brightness/temperature to apply when turning a light on.
type Preset struct {
	Brightness  int
	Temperature int
}

// Source supplies the current known lights and can be told to re-discover.
type Source interface {
	Lights() []Light
	Invalidate()
}

type Controller struct {
	src     Source
	preset  Preset
	client  *http.Client
	timeout time.Duration
}

func NewController(src Source, preset Preset, httpTimeout time.Duration) *Controller {
	return &Controller{
		src:     src,
		preset:  preset,
		timeout: httpTimeout,
		client:  &http.Client{Timeout: httpTimeout},
	}
}

func (c *Controller) Run(ctx context.Context, in <-chan events.LightIntent) {
	for {
		select {
		case <-ctx.Done():
			return
		case intent, ok := <-in:
			if !ok {
				return
			}
			c.dispatch(ctx, intent)
		}
	}
}

func (c *Controller) dispatch(ctx context.Context, intent events.LightIntent) {
	lights := c.src.Lights()
	if len(lights) == 0 {
		slog.Warn("no lights known; skipping", "intent_on", intent.On)
		return
	}
	body := c.payload(intent.On)

	var wg sync.WaitGroup
	var anyFailed bool
	var failMu sync.Mutex

	for _, l := range lights {
		wg.Add(1)
		go func(l Light) {
			defer wg.Done()
			if err := c.sendOne(ctx, l, body); err != nil {
				slog.Warn("light request failed", "host", l.Host, "err", err)
				failMu.Lock()
				anyFailed = true
				failMu.Unlock()
				return
			}
			slog.Info("light updated", "host", l.Host, "on", intent.On)
		}(l)
	}
	wg.Wait()

	if anyFailed {
		c.src.Invalidate()
	}
}

// ApplyOffBlocking is used for graceful shutdown: send Off to all lights synchronously.
func (c *Controller) ApplyOffBlocking(ctx context.Context) {
	c.dispatch(ctx, events.LightIntent{On: false, At: time.Now()})
}

func (c *Controller) payload(on bool) []byte {
	type lightEntry struct {
		On          int `json:"on"`
		Brightness  int `json:"brightness,omitempty"`
		Temperature int `json:"temperature,omitempty"`
	}
	type req struct {
		NumberOfLights int          `json:"numberOfLights"`
		Lights         []lightEntry `json:"lights"`
	}
	entry := lightEntry{}
	if on {
		entry.On = 1
		entry.Brightness = c.preset.Brightness
		entry.Temperature = c.preset.Temperature
	} else {
		entry.On = 0
	}
	b, _ := json.Marshal(req{NumberOfLights: 1, Lights: []lightEntry{entry}})
	return b
}

func (c *Controller) sendOne(ctx context.Context, l Light, body []byte) error {
	url := fmt.Sprintf("http://%s/elgato/lights", l.Host)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 5.4: Run tests to confirm they pass**

Run:

```bash
go test ./internal/light/... -v
```

Expected: all three tests PASS.

- [ ] **Step 5.5: Commit**

```bash
git add internal/light/
git commit -m "feat(light): controller with concurrent HTTP dispatch"
```

---

## Task 6: Device Probe (`/proc/*/fd/*` Scan)

Implements the mechanism for deciding whether any process is currently holding a file descriptor on a given `/dev/videoN` device. This is what resolves udev `change` events into Open vs Close.

**Files:**
- Create: `internal/camera/probe.go`
- Create: `internal/camera/probe_test.go`

- [ ] **Step 6.1: Write failing test**

Create `internal/camera/probe_test.go`:

```go
package camera

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writeFakeProcFD builds a minimal /proc layout under root:
//
//   root/<pid>/fd/<n>  -> symlink to target
//
// so ProcProbe can scan it.
func writeFakeProcFD(t *testing.T, root string, pid int, fds map[int]string) {
	t.Helper()
	base := filepath.Join(root, strconv.Itoa(pid), "fd")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	for n, target := range fds {
		link := filepath.Join(base, strconv.Itoa(n))
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProcProbe_NoFDsPointingAtDevice(t *testing.T) {
	root := t.TempDir()
	writeFakeProcFD(t, root, 1000, map[int]string{
		0: "/dev/null",
		1: "/tmp/something",
	})

	p := &ProcProbe{procRoot: root}
	inUse, err := p.IsInUse("/dev/video0")
	if err != nil {
		t.Fatal(err)
	}
	if inUse {
		t.Error("IsInUse = true, want false")
	}
}

func TestProcProbe_FDPointingAtDevice(t *testing.T) {
	root := t.TempDir()
	writeFakeProcFD(t, root, 1000, map[int]string{
		0: "/dev/null",
	})
	writeFakeProcFD(t, root, 1001, map[int]string{
		3: "/dev/video0",
	})

	p := &ProcProbe{procRoot: root}
	inUse, err := p.IsInUse("/dev/video0")
	if err != nil {
		t.Fatal(err)
	}
	if !inUse {
		t.Error("IsInUse = false, want true")
	}
}

func TestProcProbe_IgnoresNonNumericPIDDirs(t *testing.T) {
	root := t.TempDir()
	// Build a non-numeric dir that shouldn't be scanned.
	if err := os.MkdirAll(filepath.Join(root, "self", "fd"), 0o755); err != nil {
		t.Fatal(err)
	}

	p := &ProcProbe{procRoot: root}
	inUse, err := p.IsInUse("/dev/video0")
	if err != nil {
		t.Fatal(err)
	}
	if inUse {
		t.Error("IsInUse = true, want false (no numeric pids)")
	}
}
```

- [ ] **Step 6.2: Run tests to confirm they fail**

Run:

```bash
go test ./internal/camera/...
```

Expected: FAIL, `ProcProbe` undefined.

- [ ] **Step 6.3: Implement probe.go**

Create `internal/camera/probe.go`:

```go
package camera

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// DeviceProbe resolves whether a v4l2 device is currently held open by any process.
type DeviceProbe interface {
	IsInUse(devicePath string) (bool, error)
}

// ProcProbe implements DeviceProbe by scanning /proc/<pid>/fd/* for symlinks
// pointing at the given device path.
type ProcProbe struct {
	procRoot string // defaults to "/proc" when empty
}

// NewProcProbe returns a ProcProbe that reads from /proc.
func NewProcProbe() *ProcProbe {
	return &ProcProbe{procRoot: "/proc"}
}

func (p *ProcProbe) root() string {
	if p.procRoot == "" {
		return "/proc"
	}
	return p.procRoot
}

func (p *ProcProbe) IsInUse(devicePath string) (bool, error) {
	entries, err := os.ReadDir(p.root())
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue // skip non-numeric entries like "self", "cpuinfo"
		}
		pidFD := filepath.Join(p.root(), e.Name(), "fd")
		fds, err := os.ReadDir(pidFD)
		if err != nil {
			// EACCES (foreign-user process) or ENOENT (process exited) — skip.
			if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return false, err
		}
		for _, fd := range fds {
			link := filepath.Join(pidFD, fd.Name())
			target, err := os.Readlink(link)
			if err != nil {
				continue
			}
			if target == devicePath {
				return true, nil
			}
		}
	}
	return false, nil
}
```

- [ ] **Step 6.4: Run tests to confirm they pass**

Run:

```bash
go test ./internal/camera/... -v
```

Expected: all three tests PASS.

- [ ] **Step 6.5: Commit**

```bash
git add internal/camera/probe.go internal/camera/probe_test.go
git commit -m "feat(camera): /proc-based device probe"
```

---

## Task 7: Camera Watcher (udev)

Subscribes to udev video4linux events, uses the `DeviceProbe` to resolve Open/Close, emits `CameraEvent`s. The udev source is abstracted so tests don't need real udev.

**Files:**
- Create: `internal/camera/watcher.go`
- Create: `internal/camera/watcher_test.go`

- [ ] **Step 7.1: Add udev dependency**

Run:

```bash
go get github.com/pilebones/go-udev@latest
```

Expected: adds to `go.mod` / `go.sum`.

- [ ] **Step 7.2: Write failing test**

Create `internal/camera/watcher_test.go`:

```go
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
```

- [ ] **Step 7.3: Run tests to confirm they fail**

Run:

```bash
go test ./internal/camera/... -run Watcher
```

Expected: FAIL, `NewWatcher`, `Watcher.Run`, etc. undefined.

- [ ] **Step 7.4: Implement watcher.go**

Create `internal/camera/watcher.go`:

```go
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
	prev, known := w.state[device]
	w.state[device] = inUse
	w.mu.Unlock()

	if known && prev == inUse {
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
```

- [ ] **Step 7.5: Run tests to confirm they pass**

Run:

```bash
go test ./internal/camera/... -v
```

Expected: all tests PASS (both probe and watcher).

- [ ] **Step 7.6: Implement the real udev Source**

Add to `internal/camera/watcher.go` (or a new file `internal/camera/udev_source.go` — use a new file for clarity):

Create `internal/camera/udev_source.go`:

```go
package camera

import (
	"context"
	"fmt"

	"github.com/pilebones/go-udev/netlink"
)

// UdevSource reads udev netlink events and emits device paths for
// video4linux `change` events.
type UdevSource struct {
	conn   *netlink.UEventConn
	out    chan string
	cancel context.CancelFunc
}

func NewUdevSource(ctx context.Context) (*UdevSource, error) {
	conn := &netlink.UEventConn{}
	if err := conn.Connect(netlink.UdevEvent); err != nil {
		return nil, fmt.Errorf("udev connect: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	s := &UdevSource{
		conn:   conn,
		out:    make(chan string, 16),
		cancel: cancel,
	}

	go s.pump(ctx)
	return s, nil
}

func (s *UdevSource) Events() <-chan string { return s.out }

func (s *UdevSource) Close() error {
	s.cancel()
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *UdevSource) pump(ctx context.Context) {
	defer close(s.out)

	queue := make(chan netlink.UEvent, 16)
	errors := make(chan error, 1)
	matcher := &netlink.RuleDefinitions{
		Rules: []netlink.RuleDefinition{
			{
				Env: map[string]string{
					"SUBSYSTEM": "video4linux",
					"ACTION":    "change",
				},
			},
		},
	}

	quit := s.conn.Monitor(queue, errors, matcher)
	defer func() { quit <- struct{}{} }()

	for {
		select {
		case <-ctx.Done():
			return
		case e := <-errors:
			_ = e // log at caller; errors here are typically transient
		case uev := <-queue:
			devname := uev.Env["DEVNAME"]
			if devname == "" {
				continue
			}
			path := "/dev/" + devname
			select {
			case s.out <- path:
			case <-ctx.Done():
				return
			}
		}
	}
}

// ListVideoDevices returns a best-effort list of /dev/videoN paths currently
// present, used by RunWithInitialScan.
func ListVideoDevices() ([]string, error) {
	// Enumerating via sysfs is more reliable than globbing /dev; the
	// canonical place is /sys/class/video4linux/.
	const sysPath = "/sys/class/video4linux"
	entries, err := readDirIgnoreENOENT(sysPath)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		out = append(out, "/dev/"+e.Name())
	}
	return out, nil
}
```

And add to `internal/camera/probe.go` (append at end of file):

```go
// readDirIgnoreENOENT is a small helper that treats a missing path as an empty dir.
func readDirIgnoreENOENT(path string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}
```

- [ ] **Step 7.7: Verify compile**

Run:

```bash
go build ./...
go test ./internal/camera/... -v
```

Expected: all builds and tests pass. The `UdevSource` has no unit tests — it's tested manually and by the end-to-end integration test.

- [ ] **Step 7.8: Commit**

```bash
git add go.mod go.sum internal/camera/
git commit -m "feat(camera): udev watcher with /proc-based state resolution"
```

---

## Task 8: Light Discovery (mDNS)

Discovers Elgato lights via mDNS, caches results, and refreshes them periodically or on demand.

**Files:**
- Create: `internal/light/discovery.go`
- Create: `internal/light/discovery_test.go`

- [ ] **Step 8.1: Add mDNS dependency**

Run:

```bash
go get github.com/hashicorp/mdns@latest
```

Expected: adds to `go.mod` / `go.sum`.

- [ ] **Step 8.2: Write failing test**

Discovery is split into (a) querying mDNS and (b) caching. We test the cache layer because it is pure logic. The raw mDNS query is tested manually.

Create `internal/light/discovery_test.go`:

```go
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
```

- [ ] **Step 8.3: Run test, confirm failure**

Run:

```bash
go test ./internal/light/... -run Cached
```

Expected: FAIL, `NewCachedSource` undefined.

- [ ] **Step 8.4: Implement discovery.go**

Create `internal/light/discovery.go`:

```go
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
```

- [ ] **Step 8.5: Run tests to confirm they pass**

Run:

```bash
go test ./internal/light/... -v
```

Expected: all tests (controller + discovery) PASS.

- [ ] **Step 8.6: Commit**

```bash
git add go.mod go.sum internal/light/
git commit -m "feat(light): mDNS discovery with caching"
```

---

## Task 9: Main Entry Point (Wiring + Signals)

Wires all components, loads config, manages graceful shutdown.

**Files:**
- Create: `cmd/limelightd/main.go`

- [ ] **Step 9.1: Write main.go**

Create `cmd/limelightd/main.go`:

```go
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"limelightd/internal/camera"
	"limelightd/internal/config"
	"limelightd/internal/debounce"
	"limelightd/internal/events"
	"limelightd/internal/light"
)

func main() {
	setupLogging()

	cfgPath := defaultConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("config load failed", "path", cfgPath, "err", err)
		os.Exit(1)
	}
	slog.Info("config loaded", "path", cfgPath,
		"brightness", cfg.Light.Brightness,
		"temperature", cfg.Light.Temperature,
		"on_ms", cfg.Debounce.OnMs,
		"off_ms", cfg.Debounce.OffMs,
	)

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- Components ---

	source, err := camera.NewUdevSource(rootCtx)
	if err != nil {
		slog.Error("udev setup failed", "err", err)
		os.Exit(1)
	}
	defer source.Close()

	probe := camera.NewProcProbe()
	watcher := camera.NewWatcher(source, probe)

	rawEvents := make(chan events.CameraEvent, 16)
	intents := make(chan events.LightIntent, 16)

	deb := debounce.New(
		debounce.Config{
			OnDelay:  time.Duration(cfg.Debounce.OnMs) * time.Millisecond,
			OffDelay: time.Duration(cfg.Debounce.OffMs) * time.Millisecond,
		},
		debounce.SystemClock(),
	)

	discoverySrc := light.NewCachedSource(light.DiscoverElgato, cfg.Discovery.RefreshInterval, 2*time.Second)

	controller := light.NewController(
		discoverySrc,
		light.Preset{Brightness: cfg.Light.Brightness, Temperature: cfg.Light.Temperature},
		2*time.Second,
	)

	// --- Wire goroutines ---

	go discoverySrc.Run(rootCtx)

	devices, err := camera.ListVideoDevices()
	if err != nil {
		slog.Warn("video device enumeration failed", "err", err)
	}
	go watcher.RunWithInitialScan(rootCtx, rawEvents, devices)

	go deb.Run(rootCtx, rawEvents, intents)
	go controller.Run(rootCtx, intents)

	<-rootCtx.Done()
	slog.Info("shutdown signal received")

	// Graceful: send Off synchronously, using a short detached context so
	// cancellation of rootCtx doesn't immediately kill the HTTP request.
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutCancel()
	controller.ApplyOffBlocking(shutCtx)
	slog.Info("shutdown complete")
}

func defaultConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "limelightd", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "limelightd", "config.toml")
}

func setupLogging() {
	level := slog.LevelInfo
	if v := strings.ToLower(os.Getenv("LIMELIGHTD_LOG")); v == "debug" {
		level = slog.LevelDebug
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
}
```

- [ ] **Step 9.2: Verify it builds**

Run:

```bash
go build ./cmd/limelightd
```

Expected: produces a `limelightd` binary in the repo root. Exit code 0.

- [ ] **Step 9.3: Verify the binary starts and responds to SIGTERM**

Run:

```bash
./limelightd &
PID=$!
sleep 1
kill -TERM $PID
wait $PID
rm -f limelightd
```

Expected output includes:
- `config loaded ...`
- `discovery refreshed lights=0` (or however many lights you actually have)
- `shutdown signal received`
- `shutdown complete`

Binary exits cleanly (exit code 0 from wait).

- [ ] **Step 9.4: Commit**

```bash
git add cmd/limelightd/
git commit -m "feat(cmd): wire components and signal handling in main"
```

---

## Task 10: End-to-End Integration Test

Validates the whole pipeline using fakes for udev and HTTP.

**Files:**
- Create: `cmd/limelightd/integration_test.go`

- [ ] **Step 10.1: Write test**

Create `cmd/limelightd/integration_test.go`:

```go
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
```

- [ ] **Step 10.2: Run the test**

Run:

```bash
go test ./cmd/limelightd/... -v -race
```

Expected: `TestEndToEnd_OpenThenClose` PASSES. No race warnings.

- [ ] **Step 10.3: Run full test suite with race detector**

Run:

```bash
go test ./... -race
```

Expected: all tests across all packages pass. No data race warnings.

- [ ] **Step 10.4: Commit**

```bash
git add cmd/limelightd/integration_test.go
git commit -m "test(e2e): integration test covering watcher→debounce→controller"
```

---

## Task 11: systemd Unit & README

Ship the deployment artifacts.

**Files:**
- Create: `systemd/limelightd.service`
- Modify: `README.md`

- [ ] **Step 11.1: Write systemd unit**

Create `systemd/limelightd.service`:

```ini
[Unit]
Description=Elgato Key Light camera sync daemon
After=network-online.target

[Service]
ExecStart=%h/.local/bin/limelightd
Restart=on-failure
RestartSec=5s

# Hardening — this daemon does not need any of the following.
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictNamespaces=true
LockPersonality=true
MemoryDenyWriteExecute=true

[Install]
WantedBy=default.target
```

- [ ] **Step 11.2: Expand README**

Replace `README.md` with:

````markdown
# limelightd

A tiny Linux daemon that turns your Elgato Key Lights on when your webcam opens and off when it closes.

- Event-driven (udev netlink — no polling)
- Zero-config: finds lights via mDNS
- Single static binary, ≈3 MB RAM idle
- Systemd user service, no root required

See [`docs/superpowers/specs/2026-04-17-limelightd-design.md`](docs/superpowers/specs/2026-04-17-limelightd-design.md) for the full design.

## Build

```
go build -o limelightd ./cmd/limelightd
```

## Install

```
install -Dm755 limelightd ~/.local/bin/limelightd
install -Dm644 systemd/limelightd.service ~/.config/systemd/user/limelightd.service
systemctl --user daemon-reload
systemctl --user enable --now limelightd
```

## Configure (optional)

Create `~/.config/limelightd/config.toml` to override defaults:

```toml
[light]
brightness  = 20      # 3-100
temperature = 213     # 143 (cool) - 344 (warm)

[debounce]
on_ms  = 200
off_ms = 1000

[discovery]
refresh_interval = "5m"
```

## Logs

```
journalctl --user -u limelightd -f
```

Set `LIMELIGHTD_LOG=debug` in the service unit's `[Service]` section (via `Environment=`) for verbose logs.

## Manual smoke test

After install:

1. `journalctl --user -u limelightd -f` in one terminal.
2. Open your camera in a browser (e.g. a meet site) — the key light should turn on within ≈300 ms.
3. Close the camera tab — the light should turn off within ≈1300 ms (200 ms event latency + 1 s off-debounce).
4. Quickly open and close (<1 s) — the light should not visibly flicker.

## Troubleshooting

- **Light never turns on**: confirm discovery with `journalctl --user -u limelightd | grep discovery`. If it logs `lights=0`, verify your Key Light is reachable on the network (ping it and `curl http://<ip>:9123/elgato/lights`).
- **Light turns on but not off**: the daemon watches `/proc/*/fd/*` to detect close. If your camera is being held open by a different user's process, the close event won't be observed (documented limitation).
- **Permission errors on startup**: confirm the binary is at `~/.local/bin/limelightd` and that systemd-user has access to udev netlink (it should by default on modern distros).

## Development

```
go test ./... -race
```
````

- [ ] **Step 11.3: Commit**

```bash
git add systemd/ README.md
git commit -m "chore: systemd unit and install instructions"
```

---

## Task 12: Final Verification

- [ ] **Step 12.1: Full build + test + vet**

Run:

```bash
go build ./...
go vet ./...
go test ./... -race
```

Expected: all three commands exit 0.

- [ ] **Step 12.2: Build the release binary**

Run:

```bash
go build -trimpath -ldflags="-s -w" -o limelightd ./cmd/limelightd
ls -lh limelightd
```

Expected: a single static binary under 10 MB.

- [ ] **Step 12.3: Manual smoke test on the user's own machine**

Run:

```bash
install -Dm755 limelightd ~/.local/bin/limelightd
install -Dm644 systemd/limelightd.service ~/.config/systemd/user/limelightd.service
systemctl --user daemon-reload
systemctl --user enable --now limelightd
journalctl --user -u limelightd -f &
```

Trigger a camera open in a browser. Confirm:
- Light turns on within ≈300 ms of opening the camera.
- Light turns off within ≈1300 ms of closing the camera.
- Rapid open/close does not flicker the light.

Stop the follower with `kill %1` when done.

- [ ] **Step 12.4: Clean up build artifact and commit nothing further**

```bash
rm -f limelightd
```

The working tree should be clean. If not, inspect untracked files before committing anything.

---

## Spec Coverage Check

Every spec requirement should map to a task above:

| Spec requirement | Task |
| --- | --- |
| Go daemon, single binary | Task 1, Task 9, Task 12 |
| udev subscription, `subsystem=video4linux`, `ACTION=change` | Task 7 |
| Direction resolution via `/proc/*/fd/*` | Task 6 |
| Startup scan of in-use cameras | Task 7 (`RunWithInitialScan`) |
| Debouncer 200 ms on / 1000 ms off, contradiction-cancelled | Task 4 |
| mDNS discovery `_elg._tcp.local.` with cache + refresh | Task 8 |
| HTTP PUT to `:9123/elgato/lights` with preset payload | Task 5 |
| Config TOML at `~/.config/limelightd/config.toml` with defaults | Task 2, Task 9 |
| slog to stderr, journald, `LIMELIGHTD_LOG=debug` | Task 9 |
| SIGTERM/SIGINT → send Off, exit clean | Task 9 |
| systemd user service unit | Task 11 |
| Testing: debouncer table-driven, controller httptest, watcher injected | Tasks 4, 5, 7, 10 |

No spec gaps identified.
