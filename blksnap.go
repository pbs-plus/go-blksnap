// Package blksnap provides a pure Go interface to the veeamblksnap kernel
// module for block device snapshot management.
//
// It auto-detects the loaded module version and supports both API generations:
//
//   - v2 (VAL-13.x): Uses /dev/veeamblksnap + /dev/bdevfilter with path-based
//     device identification. The ioctl protocol aligns with the VAL-13.0/13.0.1
//     standalone branches.
//
//   - v1 (VAL-6.x): Uses /dev/veeamblksnap only with major:minor device
//     identification. The ioctl protocol aligns with the VAL-6.x standalone
//     branches.
//
// # Auto-detection
//
// The first call to OpenService or OpenTracker probes the system and locks in
// the API version. No explicit version selection is needed.
//
// # Requirements
//
// The veeamblksnap (and for v2, bdevfilter) kernel modules must be loaded.
// This package targets Linux only (linux/amd64, linux/arm64).
package blksnap

import (
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// APIVersion identifies the kernel module API generation.
type APIVersion int

const (
	// APIV1 is the VAL-6.x API. All operations use /dev/veeamblksnap only,
	// devices are identified by major:minor numbers, and the snapshot create
	// ioctl takes a device array directly.
	APIV1 APIVersion = 1

	// APIV2 is the VAL-13.x API. Snapshot control uses /dev/veeamblksnap,
	// block device filter operations use /dev/bdevfilter, and devices are
	// identified by filesystem path.
	APIV2 APIVersion = 2
)

// ControlDevice returns the control device path for snapshot management.
const ControlDevice = "/dev/veeamblksnap"

// FilterDevice is the bdevfilter path (v2 only).
const FilterDevice = "/dev/bdevfilter"

const filterName = "blksnap"
const imageDiskNameLen = 32
const sectorSize = 512

// Image prefixes used by the kernel module for snapshot image device names.
const (
	V1ImagePrefix = "veeamblksnapimg" // VAL-6.x
	V2ImagePrefix = "vbsnap"          // VAL-13.x
)

// BLKSNAP ioctl magic byte (same for v1 and v2).
const blksnapMagic = 'V'

// BDEVFILTER ioctl magic byte (v2 only).
const bdevfilterMagic = 'F'

// IOC direction bits and shifts (Linux ABI).
const (
	iocNone      = 0
	iocWrite     = 1
	iocRead      = 2
	iocReadWrite = 3

	iocNrShift   = 0
	iocTypeShift = 8
	iocSizeShift = 16
	iocDirShift  = 30
)

// ioctl issues a raw ioctl syscall.
func ioctlSys(fd uintptr, req uintptr, arg uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, req, arg)
	if errno != 0 {
		return errno
	}
	return nil
}

// --- Auto-detection ---

var (
	detectOnce sync.Once
	detected   APIVersion
	detectErr  error
)

// Detect probes the system to determine which API version the loaded
// kernel module supports. It opens the control device and checks whether
// /dev/bdevfilter exists (v2) or not (v1). The result is cached.
func Detect() (APIVersion, error) {
	detectOnce.Do(func() {
		// Try v2 first: bdevfilter device exists.
		if _, err := os.Stat(FilterDevice); err == nil {
			detected = APIV2
			return
		}
		// Fall back to v1: control device must exist.
		if _, err := os.Stat(ControlDevice); err == nil {
			detected = APIV1
			return
		}
		detectErr = fmt.Errorf("blksnap: neither %s nor %s found — is the kernel module loaded?",
			ControlDevice, FilterDevice)
	})
	return detected, detectErr
}

// Detected returns the detected API version. It calls Detect internally.
func Detected() APIVersion {
	v, _ := Detect()
	return v
}
