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
