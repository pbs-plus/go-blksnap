package blksnap

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"
)

// --- Sentinel errors ---

var (
	ErrNotFound      = errors.New("blksnap: not found")
	ErrAlreadyExists = errors.New("blksnap: already exists")
	ErrInterrupted   = errors.New("blksnap: interrupted")
	ErrNoData        = errors.New("blksnap: no data available")
	ErrNoSpace       = errors.New("blksnap: no space left on device")
	ErrCorrupted     = errors.New("blksnap: snapshot corrupted")
)

// --- Service ---

// Service provides low-level access to the blksnap control device.
type Service struct {
	ctl *os.File
}

// OpenService opens a connection to the control device and auto-detects
// the API version.
func OpenService() (*Service, error) {
	if _, err := Detect(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(ControlDevice, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("blksnap: open %s: %w", ControlDevice, err)
	}
	return &Service{ctl: f}, nil
}

func (s *Service) Close() error { return s.ctl.Close() }

// Version returns the kernel module version.
func (s *Service) Version() (ModuleVersion, error) {
	buf := make([]byte, 8)
	req := v2IoctlVersion
	if detected == APIV1 {
		req = v1IoctlVersion
	}
	if err := ioctlSys(s.ctl.Fd(), req, bytesPtr(buf)); err != nil {
		return ModuleVersion{}, fmt.Errorf("blksnap: get version: %w", errnoToError(err))
	}
	return unmarshalVersion(buf), nil
}

// Collect returns the UUIDs of all active snapshots.
func (s *Service) Collect() ([]UUID, error) {
	if detected == APIV1 {
		return s.collectV1()
	}
	return s.collectV2()
}

func (s *Service) collectV2() ([]UUID, error) {
	param := make([]byte, 16)
	if err := ioctlSys(s.ctl.Fd(), v2IoctlSnapshotCollect, bytesPtr(param)); err != nil {
		return nil, fmt.Errorf("blksnap: collect snapshots: %w", errnoToError(err))
	}
	count := nativeEndian.Uint32(param[0:4])
	if count == 0 {
		return nil, nil
	}
	idsBuf := make([]byte, count*16)
	nativeEndian.PutUint32(param[0:4], count)
	setOptPtr(param, bytesPtr(idsBuf))
	if err := ioctlSys(s.ctl.Fd(), v2IoctlSnapshotCollect, bytesPtr(param)); err != nil {
		return nil, fmt.Errorf("blksnap: collect snapshots: %w", errnoToError(err))
	}
	ids := make([]UUID, count)
	for i := range ids {
		ids[i] = unmarshalUUID(idsBuf, i*16)
	}
	return ids, nil
}

func (s *Service) collectV1() ([]UUID, error) {
	param := make([]byte, 16)
	if err := ioctlSys(s.ctl.Fd(), v1IoctlSnapshotCollect, bytesPtr(param)); err != nil {
		return nil, fmt.Errorf("blksnap: collect snapshots: %w", errnoToError(err))
	}
	count := nativeEndian.Uint32(param[0:4])
	if count == 0 {
		return nil, nil
	}
	idsBuf := make([]byte, count*16)
	nativeEndian.PutUint32(param[0:4], count)
	setOptPtr(param, bytesPtr(idsBuf))
	if err := ioctlSys(s.ctl.Fd(), v1IoctlSnapshotCollect, bytesPtr(param)); err != nil {
		return nil, fmt.Errorf("blksnap: collect snapshots: %w", errnoToError(err))
	}
	ids := make([]UUID, count)
	for i := range ids {
		ids[i] = unmarshalUUID(idsBuf, i*16)
	}
	return ids, nil
}

// FD returns the control device file descriptor.
func (s *Service) FD() uintptr { return s.ctl.Fd() }

// --- Snapshot (v2) ---

// Snapshot manages a single blksnap snapshot.
type Snapshot struct {
	id  UUID
	ctl *os.File
}

// CreateSnapshot creates a new snapshot with a difference storage file and limit.
// v2 only — v1 creates snapshots differently (via CreateSnapshotV1).
func CreateSnapshot(diffStorageFile string, limitBytes uint64) (*Snapshot, error) {
	if diffStorageFile == "" {
		return nil, fmt.Errorf("blksnap: diffStorageFile must not be empty")
	}
	f, err := os.OpenFile(ControlDevice, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("blksnap: open %s: %w", ControlDevice, err)
	}
	limitSectors := limitBytes / sectorSize
	filenameBytes := append([]byte(diffStorageFile), 0)
	filenamePtr := uintptr(unsafe.Pointer(&filenameBytes[0]))

	paramBuf := make([]byte, 32)
	nativeEndian.PutUint64(paramBuf[0:8], limitSectors)
	nativeEndian.PutUint64(paramBuf[8:16], uint64(filenamePtr))

	if err := ioctlSys(f.Fd(), v2IoctlSnapshotCreate, bytesPtr(paramBuf)); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("blksnap: create snapshot: %w", errnoToError(err))
	}
	runtime.KeepAlive(filenameBytes)

	id := unmarshalUUID(paramBuf, 16)
	return &Snapshot{id: id, ctl: f}, nil
}

// CreateSnapshotV1 creates a snapshot with the given device IDs (v1 API).
// The devices are added to the snapshot at creation time.
func CreateSnapshotV1(devices []DevID) (*Snapshot, UUID, error) {
	f, err := os.OpenFile(ControlDevice, os.O_RDWR, 0)
	if err != nil {
		return nil, UUID{}, fmt.Errorf("blksnap: open %s: %w", ControlDevice, err)
	}
	// blk_snap_snapshot_create: __u32 count, ptr dev_id_array, uuid_t id
	devBuf := make([]byte, len(devices)*8)
	for i, d := range devices {
		off := i * 8
		nativeEndian.PutUint32(devBuf[off:off+4], d.Major)
		nativeEndian.PutUint32(devBuf[off+4:off+8], d.Minor)
	}
	paramBuf := make([]byte, 32)
	nativeEndian.PutUint32(paramBuf[0:4], uint32(len(devices)))
	nativeEndian.PutUint64(paramBuf[8:16], uint64(bytesPtr(devBuf)))
	runtime.KeepAlive(devBuf)

	if err := ioctlSys(f.Fd(), v1IoctlSnapshotCreate, bytesPtr(paramBuf)); err != nil {
		_ = f.Close()
		return nil, UUID{}, fmt.Errorf("blksnap: create snapshot v1: %w", errnoToError(err))
	}
	id := unmarshalUUID(paramBuf, 16)
	return &Snapshot{id: id, ctl: f}, id, nil
}

// OpenSnapshot opens an existing snapshot by UUID.
func OpenSnapshot(id UUID) (*Snapshot, error) {
	f, err := os.OpenFile(ControlDevice, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("blksnap: open %s: %w", ControlDevice, err)
	}
	return &Snapshot{id: id, ctl: f}, nil
}

func (s *Snapshot) ID() UUID { return s.id }

func (s *Snapshot) Close() error {
	if s.ctl == nil {
		return nil
	}
	err := s.ctl.Close()
	s.ctl = nil
	return err
}

// Take takes the snapshot.
func (s *Snapshot) Take() error {
	buf := make([]byte, 16)
	marshalUUID(buf, 0, s.id)
	req := v2IoctlSnapshotTake
	if detected == APIV1 {
		req = v1IoctlSnapshotTake
	}
	if err := ioctlSys(s.ctl.Fd(), req, bytesPtr(buf)); err != nil {
		return fmt.Errorf("blksnap: take snapshot %s: %w", s.id, errnoToError(err))
	}
	return nil
}

// Destroy releases and destroys the snapshot.
func (s *Snapshot) Destroy() error {
	buf := make([]byte, 16)
	marshalUUID(buf, 0, s.id)
	req := v2IoctlSnapshotDestroy
	if detected == APIV1 {
		req = v1IoctlSnapshotDestroy
	}
	if err := ioctlSys(s.ctl.Fd(), req, bytesPtr(buf)); err != nil {
		return fmt.Errorf("blksnap: destroy snapshot %s: %w", s.id, errnoToError(err))
	}
	return nil
}

// WaitEvent waits for an event from the held snapshot.
func (s *Snapshot) WaitEvent(timeoutMs uint32) (SnapshotEvent, bool, error) {
	buf := snapshotEventBuf(s.id, timeoutMs)
	req := v2IoctlSnapshotWaitEvent
	if detected == APIV1 {
		req = v1IoctlSnapshotWaitEvent
	}
	if err := ioctlSys(s.ctl.Fd(), req, bytesPtr(buf)); err != nil {
		se := errnoToError(err)
		if se == ErrNotFound || se == ErrInterrupted {
			return SnapshotEvent{}, false, nil
		}
		return SnapshotEvent{}, false, fmt.Errorf("blksnap: wait event for %s: %w", s.id, err)
	}
	return unmarshalSnapshotEvent(buf), true, nil
}

// CollectImages returns the snapshot image info for all devices in the snapshot (v1 only).
func (s *Snapshot) CollectImages() (map[DevID]DevID, error) {
	// First call: get count.
	param := make([]byte, 32)
	marshalUUID(param, 0, s.id)
	if err := ioctlSys(s.ctl.Fd(), v1IoctlSnapshotCollectImages, bytesPtr(param)); err != nil {
		return nil, fmt.Errorf("blksnap: collect images: %w", errnoToError(err))
	}
	count := nativeEndian.Uint32(param[16:20])
	if count == 0 {
		return nil, nil
	}
	infoBuf := make([]byte, count*v1ImageInfoSize)
	nativeEndian.PutUint32(param[16:20], count)
	setOptPtr(param, bytesPtr(infoBuf))
	if err := ioctlSys(s.ctl.Fd(), v1IoctlSnapshotCollectImages, bytesPtr(param)); err != nil {
		return nil, fmt.Errorf("blksnap: collect images: %w", errnoToError(err))
	}
	result := make(map[DevID]DevID, count)
	for i := range count {
		off := int(i) * v1ImageInfoSize
		orig := DevID{
			Major: nativeEndian.Uint32(infoBuf[off : off+4]),
			Minor: nativeEndian.Uint32(infoBuf[off+4 : off+8]),
		}
		img := DevID{
			Major: nativeEndian.Uint32(infoBuf[off+8 : off+12]),
			Minor: nativeEndian.Uint32(infoBuf[off+12 : off+16]),
		}
		result[orig] = img
	}
	return result, nil
}

// --- Buffer helpers ---

func unmarshalVersion(data []byte) ModuleVersion {
	return ModuleVersion{
		Major:    nativeEndian.Uint16(data[0:2]),
		Minor:    nativeEndian.Uint16(data[2:4]),
		Revision: nativeEndian.Uint16(data[4:6]),
		Build:    nativeEndian.Uint16(data[6:8]),
	}
}

func snapshotEventBuf(id UUID, timeoutMs uint32) []byte {
	buf := make([]byte, 4096)
	marshalUUID(buf, 0, id)
	nativeEndian.PutUint32(buf[16:20], timeoutMs)
	return buf
}

func unmarshalSnapshotEvent(buf []byte) SnapshotEvent {
	ev := SnapshotEvent{
		Code:      SnapshotEventCode(nativeEndian.Uint32(buf[20:24])),
		TimeLabel: int64(nativeEndian.Uint64(buf[24:32])),
	}
	data := buf[32:]
	switch ev.Code {
	case EventCorrupted:
		// v1: orig_dev_id (u32 mj, u32 mn), err_code (s32)
		// v2: dev_id_mj (u32), dev_id_mn (u32), err_code (s32)
		ev.Corrupted = &SnapshotEventCorrupted{
			OrigDevIDMajor: nativeEndian.Uint32(data[0:4]),
			OrigDevIDMinor: nativeEndian.Uint32(data[4:8]),
			ErrorCode:      int32(nativeEndian.Uint32(data[8:12])),
		}
	case EventLowFreeSpace:
		ev.NoSpace = &SnapshotEventLowFreeSpace{
			RequestedSectors: nativeEndian.Uint64(data[0:8]),
		}
	}
	return ev
}

func unmarshalCBTInfo(buf []byte) CBTInfo {
	return CBTInfo{
		DeviceCapacity: nativeEndian.Uint64(buf[0:8]),
		BlockSize:      nativeEndian.Uint32(buf[8:12]),
		BlockCount:     nativeEndian.Uint32(buf[12:16]),
		GenerationID:   unmarshalUUID(buf, 16),
		ChangesNumber:  buf[32],
	}
}

func unmarshalCBTInfoV1(buf []byte) CBTInfo {
	// blk_snap_cbt_info: dev_t dev_id(8), u32 blk_size(4), u64 device_capacity(8),
	//   u32 blk_count(4), uuid_t generation_id(16), u8 snap_number(1) = 56 bytes
	// Pad after blk_size (offset 12→16 for u64 alignment)
	return CBTInfo{
		DeviceCapacity: nativeEndian.Uint64(buf[16:24]),
		BlockSize:      nativeEndian.Uint32(buf[8:12]),
		BlockCount:     nativeEndian.Uint32(buf[24:28]),
		GenerationID:   unmarshalUUID(buf, 28),
		ChangesNumber:  buf[44],
	}
}

func unmarshalSnapshotInfo(buf []byte) SnapshotImageInfo {
	var info SnapshotImageInfo
	info.ErrorCode = int32(nativeEndian.Uint32(buf[0:4]))
	raw := buf[4 : 4+imageDiskNameLen]
	n := len(raw)
	for i, b := range raw {
		if b == 0 {
			n = i
			break
		}
	}
	info.Image = string(raw[:n])
	return info
}

func bytesPtr(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b[0]))
}

func setOptPtr(buf []byte, ptr uintptr) {
	nativeEndian.PutUint64(buf[len(buf)-8:], uint64(ptr))
}

func errnoToError(err error) error {
	if err == nil {
		return nil
	}
	e, ok := err.(interface{ Errno() uintptr })
	if !ok {
		return err
	}
	switch e.Errno() {
	case 2:
		return ErrNotFound
	case 4:
		return ErrInterrupted
	case 17:
		return ErrAlreadyExists
	case 28:
		return ErrNoSpace
	case 61:
		return ErrNoData
	case 114:
		return ErrAlreadyExists
	}
	return err
}
