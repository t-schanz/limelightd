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
