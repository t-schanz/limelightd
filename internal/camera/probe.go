package camera

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	debug := slog.Default().Enabled(context.Background(), slog.LevelDebug)

	entries, err := os.ReadDir(p.root())
	if err != nil {
		return false, err
	}
	var holders []string
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
				if !debug {
					return true, nil
				}
				holders = append(holders, p.describeHolder(e.Name()))
				break // one FD match per process is enough
			}
		}
	}
	if debug {
		slog.Debug("probe scan", "device", devicePath, "in_use", len(holders) > 0, "holders", holders)
	}
	return len(holders) > 0, nil
}

// describeHolder returns "pid=NNN comm=foo" for the given PID dir name, best-effort.
func (p *ProcProbe) describeHolder(pid string) string {
	comm := ""
	if b, err := os.ReadFile(filepath.Join(p.root(), pid, "comm")); err == nil {
		comm = strings.TrimSpace(string(b))
	}
	if comm == "" {
		return "pid=" + pid
	}
	return "pid=" + pid + " comm=" + comm
}

// readDirIgnoreENOENT is a small helper that treats a missing path as an empty dir.
func readDirIgnoreENOENT(path string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}
