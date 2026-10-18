//go:build linux

package image

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// rasterLimits are the hard ceilings of an rlimit-isolated raster worker.
// RLIMIT_DATA bounds committed private writable memory (the Go heap; the
// decoded 20 MP worst case and its three re-encoded variants fit under it,
// a decompression bomb does not), RLIMIT_AS bounds the address space
// including the runtime's PROT_NONE arena reservations, RLIMIT_CPU bounds CPU
// seconds (SIGXCPU/SIGKILL), RLIMIT_NOFILE leaves only the runtime's own
// descriptors, RLIMIT_FSIZE forbids writing files and RLIMIT_CORE forbids
// core dumps of untrusted memory.
var rasterLimits = []struct {
	resource int
	value    uint64
}{
	{unix.RLIMIT_DATA, 512 << 20},
	{unix.RLIMIT_AS, 4 << 30},
	{unix.RLIMIT_CPU, 5},
	{unix.RLIMIT_NOFILE, 16},
	{unix.RLIMIT_FSIZE, 0},
	{unix.RLIMIT_CORE, 0},
}

// applyRasterLimits sets every ceiling on the worker process (soft = hard)
// and reads each one back; any failure or mismatch fails closed.
func applyRasterLimits(pid int) error {
	if pid <= 1 {
		return ErrUnavailable
	}
	for _, limit := range rasterLimits {
		want := unix.Rlimit{Cur: limit.value, Max: limit.value}
		if unix.Prlimit(pid, limit.resource, &want, nil) != nil {
			return ErrUnavailable
		}
		var actual unix.Rlimit
		if unix.Prlimit(pid, limit.resource, nil, &actual) != nil || actual != want {
			return ErrUnavailable
		}
	}
	return nil
}

// rlimitSupported verifies the kernel accepts prlimit(2) on this process.
func rlimitSupported() error {
	var current unix.Rlimit
	if unix.Prlimit(0, unix.RLIMIT_AS, nil, &current) != nil {
		return ErrUnavailable
	}
	return nil
}

// workerProcAttr kills the worker if the service dies and keeps it out of
// the service's process group.
func workerProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, Setpgid: true}
}
