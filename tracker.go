package blksnap

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Tracker manages CBT and snapshot participation for a single block device.
type Tracker struct {
	api APIVersion

	// v2 fields
	v2file    *os.File
	v2fd      uintptr
	v2devPath string
	v2pathBuf []byte

	// v1 fields
	v1ctl *os.File
	v1dev DevID
}

// OpenTracker opens the appropriate device for the detected API version.
// devicePath is the block device path (e.g., /dev/sda1) — used for v2 directly,
// stat'd for major:minor in v1.
func OpenTracker(devicePath string) (*Tracker, error) {
	if _, err := Detect(); err != nil {
		return nil, err
	}
	if detected == APIV1 {
		return openTrackerV1(devicePath)
	}
	return openTrackerV2(devicePath)
}

// --- v2 tracker ---

func openTrackerV2(devicePath string) (*Tracker, error) {
	f, err := os.OpenFile(FilterDevice, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("blksnap: open %s: %w", FilterDevice, err)
	}
	return &Tracker{
		api:       APIV2,
		v2file:    f,
		v2fd:      f.Fd(),
		v2devPath: devicePath,
		v2pathBuf: append([]byte(devicePath), 0),
	}, nil
}

func (t *Tracker) v2devPathPtr() uintptr {
	if len(t.v2pathBuf) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&t.v2pathBuf[0]))
}

func (t *Tracker) v2attachBuf() []byte {
	buf := make([]byte, 56)
	nativeEndian.PutUint64(buf[0:8], uint64(t.v2devPathPtr()))
	copy(buf[8:], filterName)
	return buf
}

func (t *Tracker) v2detachBuf() []byte {
	buf := make([]byte, 40)
	nativeEndian.PutUint64(buf[0:8], uint64(t.v2devPathPtr()))
	copy(buf[8:], filterName)
	return buf
}

func (t *Tracker) v2ctlBuf(cmd uint32, optBuf []byte) []byte {
	buf := make([]byte, 56)
	nativeEndian.PutUint64(buf[0:8], uint64(t.v2devPathPtr()))
	copy(buf[8:], filterName)
	nativeEndian.PutUint32(buf[40:44], cmd)
	nativeEndian.PutUint32(buf[44:48], uint32(len(optBuf)))
	return buf
}

// --- v1 tracker ---

func openTrackerV1(devicePath string) (*Tracker, error) {
	var st unix.Stat_t
	if err := unix.Stat(devicePath, &st); err != nil {
		return nil, fmt.Errorf("blksnap: stat %s: %w", devicePath, err)
	}
	ctl, err := os.OpenFile(ControlDevice, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("blksnap: open %s: %w", ControlDevice, err)
	}
	return &Tracker{
		api:   APIV1,
		v1ctl: ctl,
		v1dev: DevID{Major: unix.Major(st.Rdev), Minor: unix.Minor(st.Rdev)},
	}, nil
}

// --- Public API ---

func (t *Tracker) Close() error {
	if t.api == APIV2 && t.v2file != nil {
		err := t.v2file.Close()
		t.v2file = nil
		t.v2fd = 0
		return err
	}
	if t.api == APIV1 && t.v1ctl != nil {
		err := t.v1ctl.Close()
		t.v1ctl = nil
		return err
	}
	return nil
}

func (t *Tracker) Attach() (bool, error) {
	if t.api == APIV1 {
		return true, nil // v1: devices are attached at snapshot creation time
	}
	buf := t.v2attachBuf()
	if err := ioctlSys(t.v2fd, v2IoctlBdevfilterAttach, bytesPtr(buf)); err != nil {
		if isErrno(err, unix.EALREADY) {
			return false, nil
		}
		return false, fmt.Errorf("blksnap: attach filter to %s: %w", t.v2devPath, errnoToError(err))
	}
	return true, nil
}

func (t *Tracker) Detach() error {
	if t.api == APIV1 {
		return nil // v1: no detach needed
	}
	buf := t.v2detachBuf()
	if err := ioctlSys(t.v2fd, v2IoctlBdevfilterDetach, bytesPtr(buf)); err != nil {
		return fmt.Errorf("blksnap: detach filter from %s: %w", t.v2devPath, errnoToError(err))
	}
	return nil
}

func (t *Tracker) CBTInfo() (CBTInfo, error) {
	if t.api == APIV1 {
		return t.cbtInfoV1()
	}
	return t.cbtInfoV2()
}

func (t *Tracker) cbtInfoV2() (CBTInfo, error) {
	optBuf := make([]byte, 40)
	ctlBuf := t.v2ctlBuf(ctlCBTInfo, optBuf)
	setOptPtr(ctlBuf, bytesPtr(optBuf))
	if err := ioctlSys(t.v2fd, v2IoctlBdevfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return CBTInfo{}, fmt.Errorf("blksnap: CBT info for %s: %w", t.v2devPath, errnoToError(err))
	}
	return unmarshalCBTInfo(optBuf), nil
}

func (t *Tracker) cbtInfoV1() (CBTInfo, error) {
	// Collect all trackers, find the one matching our device.
	param := make([]byte, 16)
	if err := ioctlSys(t.v1ctl.Fd(), v1IoctlTrackerCollect, bytesPtr(param)); err != nil {
		return CBTInfo{}, fmt.Errorf("blksnap: collect trackers: %w", errnoToError(err))
	}
	count := nativeEndian.Uint32(param[0:4])
	if count == 0 {
		return CBTInfo{}, fmt.Errorf("blksnap: no trackers found")
	}
	infoBuf := make([]byte, count*48)
	nativeEndian.PutUint32(param[0:4], count)
	setOptPtr(param, bytesPtr(infoBuf))
	if err := ioctlSys(t.v1ctl.Fd(), v1IoctlTrackerCollect, bytesPtr(param)); err != nil {
		return CBTInfo{}, fmt.Errorf("blksnap: collect trackers: %w", errnoToError(err))
	}
	for i := range count {
		off := int(i) * 48
		mj := nativeEndian.Uint32(infoBuf[off : off+4])
		mn := nativeEndian.Uint32(infoBuf[off+4 : off+8])
		if mj == t.v1dev.Major && mn == t.v1dev.Minor {
			return unmarshalCBTInfoV1(infoBuf[off:]), nil
		}
	}
	return CBTInfo{}, fmt.Errorf("blksnap: device %d:%d not tracked", t.v1dev.Major, t.v1dev.Minor)
}

func (t *Tracker) ReadCBTMap(offset, length uint32, dst []byte) error {
	if uint32(len(dst)) < length {
		return fmt.Errorf("blksnap: dst too small: need %d, got %d", length, len(dst))
	}
	if t.api == APIV1 {
		return t.readCBTMapV1(offset, length, dst)
	}
	return t.readCBTMapV2(offset, length, dst)
}

func (t *Tracker) readCBTMapV2(offset, length uint32, dst []byte) error {
	cbtBuf := make([]byte, 16)
	nativeEndian.PutUint32(cbtBuf[0:4], offset)
	nativeEndian.PutUint32(cbtBuf[4:8], length)
	setOptPtr(cbtBuf, bytesPtr(dst[:length]))
	ctlBuf := t.v2ctlBuf(ctlCBTMap, cbtBuf)
	setOptPtr(ctlBuf, bytesPtr(cbtBuf))
	if err := ioctlSys(t.v2fd, v2IoctlBdevfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return fmt.Errorf("blksnap: read CBT map for %s: %w", t.v2devPath, errnoToError(err))
	}
	return nil
}

func (t *Tracker) readCBTMapV1(offset, length uint32, dst []byte) error {
	// blk_snap_tracker_read_cbt_bitmap: dev_t(8) + u32 offset(4) + u32 length(4) + ptr buff(8) = 24
	buf := make([]byte, 24)
	nativeEndian.PutUint32(buf[0:4], t.v1dev.Major)
	nativeEndian.PutUint32(buf[4:8], t.v1dev.Minor)
	nativeEndian.PutUint32(buf[8:12], offset)
	nativeEndian.PutUint32(buf[12:16], length)
	nativeEndian.PutUint64(buf[16:24], uint64(bytesPtr(dst[:length])))
	if err := ioctlSys(t.v1ctl.Fd(), v1IoctlTrackerReadCBTMap, bytesPtr(buf)); err != nil {
		return fmt.Errorf("blksnap: read CBT map v1: %w", errnoToError(err))
	}
	return nil
}

func (t *Tracker) MarkDirty(ranges []SectorRange) error {
	if len(ranges) == 0 {
		return nil
	}
	if t.api == APIV1 {
		return t.markDirtyV1(ranges)
	}
	return t.markDirtyV2(ranges)
}

func (t *Tracker) markDirtyV2(ranges []SectorRange) error {
	cbtBuf := make([]byte, 16)
	nativeEndian.PutUint32(cbtBuf[0:4], uint32(len(ranges)))
	sectorsBuf := make([]byte, len(ranges)*16)
	for i, r := range ranges {
		off := i * 16
		nativeEndian.PutUint64(sectorsBuf[off:off+8], r.Offset)
		nativeEndian.PutUint64(sectorsBuf[off+8:off+16], r.Count)
	}
	setOptPtr(cbtBuf, bytesPtr(sectorsBuf))
	ctlBuf := t.v2ctlBuf(ctlCBTDirty, cbtBuf)
	setOptPtr(ctlBuf, bytesPtr(cbtBuf))
	if err := ioctlSys(t.v2fd, v2IoctlBdevfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return fmt.Errorf("blksnap: mark dirty for %s: %w", t.v2devPath, errnoToError(err))
	}
	return nil
}

func (t *Tracker) markDirtyV1(ranges []SectorRange) error {
	// blk_snap_tracker_mark_dirty_blocks: dev_t(8) + u32 count(4) + pad(4) + ptr(8) = 24
	sectorsBuf := make([]byte, len(ranges)*16)
	for i, r := range ranges {
		off := i * 16
		nativeEndian.PutUint64(sectorsBuf[off:off+8], r.Offset)
		nativeEndian.PutUint64(sectorsBuf[off+8:off+16], r.Count)
	}
	buf := make([]byte, 24)
	nativeEndian.PutUint32(buf[0:4], t.v1dev.Major)
	nativeEndian.PutUint32(buf[4:8], t.v1dev.Minor)
	nativeEndian.PutUint32(buf[8:12], uint32(len(ranges)))
	nativeEndian.PutUint64(buf[16:24], uint64(bytesPtr(sectorsBuf)))
	if err := ioctlSys(t.v1ctl.Fd(), v1IoctlTrackerMarkDirty, bytesPtr(buf)); err != nil {
		return fmt.Errorf("blksnap: mark dirty v1: %w", errnoToError(err))
	}
	return nil
}

// SnapshotAdd adds the device to a snapshot. v2 only — v1 adds at create time.
func (t *Tracker) SnapshotAdd(id UUID) error {
	if t.api == APIV1 {
		return nil // v1: devices added at snapshot creation
	}
	optBuf := make([]byte, 16)
	marshalUUID(optBuf, 0, id)
	ctlBuf := t.v2ctlBuf(ctlSnapshotAdd, optBuf)
	setOptPtr(ctlBuf, bytesPtr(optBuf))
	if err := ioctlSys(t.v2fd, v2IoctlBdevfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return fmt.Errorf("blksnap: add %s to snapshot: %w", t.v2devPath, errnoToError(err))
	}
	return nil
}

// SnapshotInfo retrieves snapshot image information (v2 only).
func (t *Tracker) SnapshotInfo() (SnapshotImageInfo, error) {
	optBuf := make([]byte, 36)
	ctlBuf := t.v2ctlBuf(ctlSnapshotInfo, optBuf)
	setOptPtr(ctlBuf, bytesPtr(optBuf))
	if err := ioctlSys(t.v2fd, v2IoctlBdevfilterCtl, bytesPtr(ctlBuf)); err != nil {
		return SnapshotImageInfo{}, fmt.Errorf("blksnap: snapshot info for %s: %w", t.v2devPath, errnoToError(err))
	}
	return unmarshalSnapshotInfo(optBuf), nil
}

// V1DevID returns the device major:minor (v1 only, zero value otherwise).
func (t *Tracker) V1DevID() DevID { return t.v1dev }

func isErrno(err error, target unix.Errno) bool {
	e, ok := err.(unix.Errno)
	return ok && e == target
}
