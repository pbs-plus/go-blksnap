package blksnap

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Tracker manages CBT and snapshot participation for a single block device.
// It opens the block device with O_DIRECT and communicates with the blksnap
// kernel filter via ioctl.
type Tracker struct {
	file *os.File
	fd   uintptr
	path string
}

// OpenTracker opens a block device for blksnap CBT tracking.
// The device must be a valid block device (e.g., /dev/sda1). The file is
// opened with O_DIRECT as required by the kernel filter interface.
func OpenTracker(devicePath string) (*Tracker, error) {
	f, err := os.OpenFile(devicePath, os.O_RDONLY|unixFlags(unix.O_DIRECT), 0600)
	if err != nil {
		return nil, fmt.Errorf("blksnap: failed to open device %s: %w", devicePath, err)
	}
	return &Tracker{
		file: f,
		fd:   f.Fd(),
		path: devicePath,
	}, nil
}

// unixFlags converts golang.org/x/sys/unix flag values to int for os.OpenFile.
func unixFlags(flags int) int {
	return flags
}

// Close releases the file descriptor. The Tracker is unusable after Close.
func (t *Tracker) Close() error {
	if t.file == nil {
		return nil
	}
	err := t.file.Close()
	t.file = nil
	t.fd = 0
	if err != nil {
		return fmt.Errorf("blksnap: failed to close device %s: %w", t.path, err)
	}
	return nil
}

// Attach attaches the blksnap filter to the block device.
// Returns true if the filter was newly attached, false if it was already
// attached (EALREADY).
func (t *Tracker) Attach() (bool, error) {
	buf := blkfilterAttachBuf()
	if err := ioctl(t.fd, IoctlBlkfilterAttach, bytesPtr(buf)); err != nil {
		if isErrno(err, ealready) {
			return false, nil
		}
		return false, fmt.Errorf("blksnap: attach filter to %s: %w", t.path, errnoToError(err))
	}
	return true, nil
}

// Detach detaches the blksnap filter from the block device.
func (t *Tracker) Detach() error {
	buf := blkfilterDetachBuf()
	if err := ioctl(t.fd, IoctlBlkfilterDetach, bytesPtr(buf)); err != nil {
		return fmt.Errorf("blksnap: detach filter from %s: %w", t.path, errnoToError(err))
	}
	return nil
}

// CBTInfo retrieves change block tracking metadata for the device.
func (t *Tracker) CBTInfo() (CBTInfo, error) {
	optBuf := make([]byte, 40) // sizeof(blksnap_cbtinfo)
	ctlBuf := blkfilterCtlBuf(blkfilterCtlCBTInfo, optBuf)

	// Set opt pointer to the option buffer.
	setOptPtr(ctlBuf, bytesPtr(optBuf))

	if err := ioctl(t.fd, IoctlBlkfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return CBTInfo{}, fmt.Errorf("blksnap: get CBT info for %s: %w", t.path, errnoToError(err))
	}
	return unmarshalCBTInfo(optBuf), nil
}

// ReadCBTMap reads a portion of the CBT bitmap.
// offset is the byte offset into the bitmap, length is the number of bytes to read.
func (t *Tracker) ReadCBTMap(offset, length uint32, dst []byte) error {
	if uint32(len(dst)) < length {
		return fmt.Errorf("blksnap: destination buffer too small: need %d, got %d", length, len(dst))
	}

	// blksnap_cbtmap layout: u32 offset, u32 length, u64 buffer
	cbtBuf := make([]byte, 16)
	nativeEndian.PutUint32(cbtBuf[0:4], offset)
	nativeEndian.PutUint32(cbtBuf[4:8], length)
	setOptPtr(cbtBuf, bytesPtr(dst[:length]))

	ctlBuf := blkfilterCtlBuf(blkfilterCtlCBTMap, cbtBuf)
	setOptPtr(ctlBuf, bytesPtr(cbtBuf))

	if err := ioctl(t.fd, IoctlBlkfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return fmt.Errorf("blksnap: read CBT map for %s: %w", t.path, errnoToError(err))
	}
	return nil
}

// MarkDirty marks sectors as changed in the CBT map.
func (t *Tracker) MarkDirty(ranges []SectorRange) error {
	if len(ranges) == 0 {
		return nil
	}
	// blksnap_cbtdirty layout: u32 count, u64 dirty_sectors
	cbtBuf := make([]byte, 16)
	nativeEndian.PutUint32(cbtBuf[0:4], uint32(len(ranges)))

	// Build the sectors array and set pointer
	sectorsBuf := make([]byte, len(ranges)*16)
	for i, r := range ranges {
		off := i * 16
		nativeEndian.PutUint64(sectorsBuf[off:off+8], r.Offset)
		nativeEndian.PutUint64(sectorsBuf[off+8:off+16], r.Count)
	}
	setOptPtr(cbtBuf, bytesPtr(sectorsBuf))

	ctlBuf := blkfilterCtlBuf(blkfilterCtlCBTDirty, cbtBuf)
	setOptPtr(ctlBuf, bytesPtr(cbtBuf))

	if err := ioctl(t.fd, IoctlBlkfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return fmt.Errorf("blksnap: mark dirty for %s: %w", t.path, errnoToError(err))
	}
	return nil
}

// SnapshotAdd adds the device to a snapshot identified by id.
func (t *Tracker) SnapshotAdd(id UUID) error {
	// blksnap_snapshotadd layout: uuid id (16 bytes)
	optBuf := make([]byte, 16)
	marshalUUID(optBuf, 0, id)

	ctlBuf := blkfilterCtlBuf(blkfilterCtlSnapshotAdd, optBuf)
	setOptPtr(ctlBuf, bytesPtr(optBuf))

	if err := ioctl(t.fd, IoctlBlkfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return fmt.Errorf("blksnap: add %s to snapshot: %w", t.path, errnoToError(err))
	}
	return nil
}

// SnapshotInfo retrieves snapshot image information for the device.
func (t *Tracker) SnapshotInfo() (SnapshotImageInfo, error) {
	optBuf := make([]byte, 36) // sizeof(blksnap_snapshotinfo)
	ctlBuf := blkfilterCtlBuf(blkfilterCtlSnapshotInfo, optBuf)
	setOptPtr(ctlBuf, bytesPtr(optBuf))

	if err := ioctl(t.fd, IoctlBlkfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return SnapshotImageInfo{}, fmt.Errorf("blksnap: get snapshot info for %s: %w", t.path, errnoToError(err))
	}
	return unmarshalSnapshotInfo(optBuf), nil
}

// bytesPtr returns a uintptr pointing to the first byte of b.
func bytesPtr(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b[0]))
}

// setOptPtr writes a pointer value into the opt field of a blkfilter_ctl
// or blksnap_cbtmap/btsnap_cbtdirty struct (at offset 40 for ctl, offset 8 for cbtmap/cbtdirty).
func setOptPtr(buf []byte, ptr uintptr) {
	// The opt field is the last 8 bytes of the struct.
	nativeEndian.PutUint64(buf[len(buf)-8:], uint64(ptr))
}

// isErrno checks if the error matches a specific errno value.
func isErrno(err error, target uintptr) bool {
	type errnoer interface {
		Errno() uintptr
	}
	if e, ok := err.(errnoer); ok {
		return e.Errno() == target
	}
	return false
}

// Linux errno values used in comparisons.
const (
	enoent   = 2
	eintr    = 4
	esrch    = 3
	enosys   = 38
	ealready = 114
	enospc   = 28
	enodata  = 61
)
