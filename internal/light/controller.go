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
		slog.Warn("no lights known; triggering rediscovery", "intent_on", intent.On)
		c.src.Invalidate()
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
