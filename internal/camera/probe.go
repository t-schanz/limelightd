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
