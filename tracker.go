package blksnap

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Tracker manages CBT and snapshot participation for a single block device
// through the /dev/bdevfilter interface (VAL-13.0 standalone module).
// It opens the bdevfilter device once and passes the block device path
// as a string pointer in every ioctl.
type Tracker struct {
	file    *os.File
	fd      uintptr
	devPath string
}

// OpenTracker opens the bdevfilter device for blksnap CBT tracking.
// The devicePath is the block device to operate on (e.g., /dev/sda1).
func OpenTracker(devicePath string) (*Tracker, error) {
	f, err := os.OpenFile(FilterDevice, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("blksnap: failed to open %s: %w", FilterDevice, err)
	}
	return &Tracker{
		file:    f,
		fd:      f.Fd(),
		devPath: devicePath,
	}, nil
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
		return fmt.Errorf("blksnap: failed to close %s: %w", FilterDevice, err)
	}
	return nil
}

// devPathPtr returns a uintptr to the block device path string.
func (t *Tracker) devPathPtr() uintptr {
	b := append([]byte(t.devPath), 0)
	return uintptr(unsafe.Pointer(&b[0]))
}

// Attach attaches the blksnap filter to the block device through bdevfilter.
// Returns true if the filter was newly attached, false if already attached (EALREADY).
func (t *Tracker) Attach() (bool, error) {
	buf := bdevfilterAttachBuf(t.devPathPtr())
	if err := ioctl(t.fd, IoctlBdevfilterAttach, bytesPtr(buf)); err != nil {
		if isErrno(err, unix.EALREADY) {
			return false, nil
		}
		return false, fmt.Errorf("blksnap: attach filter to %s: %w", t.devPath, errnoToError(err))
	}
	return true, nil
}

// Detach detaches the blksnap filter from the block device.
func (t *Tracker) Detach() error {
	buf := bdevfilterNameBuf(t.devPathPtr())
	if err := ioctl(t.fd, IoctlBdevfilterDetach, bytesPtr(buf)); err != nil {
		return fmt.Errorf("blksnap: detach filter from %s: %w", t.devPath, errnoToError(err))
	}
	return nil
}

// CBTInfo retrieves change block tracking metadata for the device.
func (t *Tracker) CBTInfo() (CBTInfo, error) {
	optBuf := make([]byte, 40) // sizeof(blksnap_cbtinfo)
	ctlBuf := bdevfilterCtlBuf(blkfilterCtlCBTInfo, optBuf, t.devPathPtr())
	setOptPtr(ctlBuf, bytesPtr(optBuf))

	if err := ioctl(t.fd, IoctlBdevfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return CBTInfo{}, fmt.Errorf("blksnap: get CBT info for %s: %w", t.devPath, errnoToError(err))
	}
	return unmarshalCBTInfo(optBuf), nil
}

// ReadCBTMap reads a portion of the CBT bitmap.
func (t *Tracker) ReadCBTMap(offset, length uint32, dst []byte) error {
	if uint32(len(dst)) < length {
		return fmt.Errorf("blksnap: destination buffer too small: need %d, got %d", length, len(dst))
	}

	cbtBuf := make([]byte, 16)
	nativeEndian.PutUint32(cbtBuf[0:4], offset)
	nativeEndian.PutUint32(cbtBuf[4:8], length)
	setOptPtr(cbtBuf, bytesPtr(dst[:length]))

	ctlBuf := bdevfilterCtlBuf(blkfilterCtlCBTMap, cbtBuf, t.devPathPtr())
	setOptPtr(ctlBuf, bytesPtr(cbtBuf))

	if err := ioctl(t.fd, IoctlBdevfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return fmt.Errorf("blksnap: read CBT map for %s: %w", t.devPath, errnoToError(err))
	}
	return nil
}

// MarkDirty marks sectors as changed in the CBT map.
func (t *Tracker) MarkDirty(ranges []SectorRange) error {
	if len(ranges) == 0 {
		return nil
	}
	cbtBuf := make([]byte, 16)
	nativeEndian.PutUint32(cbtBuf[0:4], uint32(len(ranges)))

	sectorsBuf := make([]byte, len(ranges)*16)
	for i, r := range ranges {
		off := i * 16
		nativeEndian.PutUint64(sectorsBuf[off:off+8], r.Offset)
		nativeEndian.PutUint64(sectorsBuf[off+8:off+16], r.Count)
	}
	setOptPtr(cbtBuf, bytesPtr(sectorsBuf))

	ctlBuf := bdevfilterCtlBuf(blkfilterCtlCBTDirty, cbtBuf, t.devPathPtr())
	setOptPtr(ctlBuf, bytesPtr(cbtBuf))

	if err := ioctl(t.fd, IoctlBdevfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return fmt.Errorf("blksnap: mark dirty for %s: %w", t.devPath, errnoToError(err))
	}
	return nil
}

// SnapshotAdd adds the device to a snapshot identified by id.
func (t *Tracker) SnapshotAdd(id UUID) error {
	optBuf := make([]byte, 16)
	marshalUUID(optBuf, 0, id)

	ctlBuf := bdevfilterCtlBuf(blkfilterCtlSnapshotAdd, optBuf, t.devPathPtr())
	setOptPtr(ctlBuf, bytesPtr(optBuf))

	if err := ioctl(t.fd, IoctlBdevfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return fmt.Errorf("blksnap: add %s to snapshot: %w", t.devPath, errnoToError(err))
	}
	return nil
}

// SnapshotInfo retrieves snapshot image information for the device.
func (t *Tracker) SnapshotInfo() (SnapshotImageInfo, error) {
	optBuf := make([]byte, 36) // sizeof(blksnap_snapshotinfo)
	ctlBuf := bdevfilterCtlBuf(blkfilterCtlSnapshotInfo, optBuf, t.devPathPtr())
	setOptPtr(ctlBuf, bytesPtr(optBuf))

	if err := ioctl(t.fd, IoctlBdevfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return SnapshotImageInfo{}, fmt.Errorf("blksnap: get snapshot info for %s: %w", t.devPath, errnoToError(err))
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

// setOptPtr writes a pointer value into the opt field of a struct.
// The opt field is always the last 8 bytes.
func setOptPtr(buf []byte, ptr uintptr) {
	nativeEndian.PutUint64(buf[len(buf)-8:], uint64(ptr))
}

// isErrno checks if err matches a specific unix.Errno value.
func isErrno(err error, target unix.Errno) bool {
	e, ok := err.(unix.Errno)
	return ok && e == target
}
