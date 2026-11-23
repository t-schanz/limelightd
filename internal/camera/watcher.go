package camera

import (
	"context"
	"log/slog"
	"time"

	"limelightd/internal/events"
)

// PollingWatcher observes a set of v4l2 devices by periodically asking a
// DeviceProbe whether each is in use. Transitions emit CameraEvents.
//
// Polling is used instead of udev because the uvcvideo kernel driver does not
// reliably emit `change` uevents on device open/close across all hardware.
type PollingWatcher struct {
	probe    DeviceProbe
	devices  []string
	interval time.Duration
	state    map[string]bool
}

// NewPollingWatcher constructs a watcher that polls the given devices at the
// given interval using probe.
func NewPollingWatcher(probe DeviceProbe, devices []string, interval time.Duration) *PollingWatcher {
	return &PollingWatcher{
		probe:    probe,
		devices:  devices,
		interval: interval,
		state:    make(map[string]bool),
	}
}

// Run polls until ctx is done. The first poll fires immediately so that any
// device already in use at startup is reported as an Open event.
func (w *PollingWatcher) Run(ctx context.Context, out chan<- events.CameraEvent) {
	w.scan(ctx, out)

	t := time.NewTicker(w.interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.scan(ctx, out)
		}
	}
}

func (w *PollingWatcher) scan(ctx context.Context, out chan<- events.CameraEvent) {
	for _, d := range w.devices {
		inUse, err := w.probe.IsInUse(d)
		if err != nil {
			slog.Warn("probe failed", "device", d, "err", err)
			continue
		}
		prev := w.state[d]
		slog.Debug("watcher tick", "device", d, "in_use", inUse, "previous", prev)
		if inUse == prev {
			continue
		}
		w.state[d] = inUse
		kind := events.Close
		if inUse {
			kind = events.Open
		}
		slog.Info("camera state changed", "device", d, "kind", kind.String(), "in_use", inUse, "was", prev)
		select {
		case out <- events.CameraEvent{Kind: kind, At: time.Now()}:
		case <-ctx.Done():
			return
		}
	}
}

// ListVideoDevices returns a best-effort list of /dev/videoN paths currently
// present on the system, enumerated via /sys/class/video4linux.
func ListVideoDevices() ([]string, error) {
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
