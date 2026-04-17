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
