package blksnap

import (
	"encoding/binary"
	"fmt"
)

// UUID is a 16-byte unique identifier used by blksnap for snapshots.
type UUID [16]byte

// String returns the UUID in standard 8-4-4-4-12 hex format.
func (u UUID) String() string {
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		u[0], u[1], u[2], u[3], u[4], u[5], u[6], u[7],
		u[8], u[9], u[10], u[11], u[12], u[13], u[14], u[15])
}

// IsZero reports whether u is the zero UUID.
func (u UUID) IsZero() bool {
	return u == UUID{}
}

// ParseUUID parses a UUID string in standard 8-4-4-4-12 hex format.
func ParseUUID(s string) (UUID, error) {
	var u UUID
	if err := parseUUIDHex(s, u[:]); err != nil {
		return UUID{}, err
	}
	return u, nil
}

// MustParseUUID parses a UUID string and panics on error.
func MustParseUUID(s string) UUID {
	u, err := ParseUUID(s)
	if err != nil {
		panic(fmt.Sprintf("blksnap: ParseUUID(%q): %v", s, err))
	}
	return u
}

func parseUUIDHex(s string, dst []byte) error {
	if len(s) != 36 {
		return fmt.Errorf("blksnap: invalid UUID length: %d", len(s))
	}
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return fmt.Errorf("blksnap: invalid UUID format")
	}
	hex := make([]byte, 32)
	j := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			continue
		}
		hex[j] = s[i]
		j++
	}
	_, err := fmt.Sscanf(string(hex), "%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x",
		&dst[0], &dst[1], &dst[2], &dst[3], &dst[4], &dst[5], &dst[6], &dst[7],
		&dst[8], &dst[9], &dst[10], &dst[11], &dst[12], &dst[13], &dst[14], &dst[15])
	if err != nil {
		return fmt.Errorf("blksnap: invalid UUID hex: %w", err)
	}
	return nil
}

// unmarshalUUID reads a UUID from an ioctl result buffer at the given offset.
func unmarshalUUID(data []byte, offset int) UUID {
	var u UUID
	copy(u[:], data[offset:offset+16])
	return u
}

// marshalUUID writes a UUID into an ioctl argument buffer at the given offset.
func marshalUUID(data []byte, offset int, u UUID) {
	copy(data[offset:offset+16], u[:])
}

// Version represents the blksnap kernel module version.
type Version struct {
	Major    uint16
	Minor    uint16
	Revision uint16
	Build    uint16
}

// String returns the version as "major.minor.revision.build".
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d.%d", v.Major, v.Minor, v.Revision, v.Build)
}

// unmarshalVersion decodes a blksnap_version struct from the ioctl buffer.
func unmarshalVersion(data []byte) Version {
	return Version{
		Major:    nativeEndian.Uint16(data[0:2]),
		Minor:    nativeEndian.Uint16(data[2:4]),
		Revision: nativeEndian.Uint16(data[4:6]),
		Build:    nativeEndian.Uint16(data[6:8]),
	}
}

// nativeEndian is the host byte order. All supported targets (linux/amd64,
// linux/arm64) are little-endian.
var nativeEndian = binary.LittleEndian

// CBTInfo holds change block tracking metadata for a block device.
type CBTInfo struct {
	DeviceCapacity uint64 // bytes
	BlockSize      uint32 // bytes
	BlockCount     uint32
	GenerationID   UUID
	ChangesNumber  uint8
}

// CBTMap represents a portion of the CBT bitmap.
type CBTMap struct {
	Offset uint32 // byte offset into the CBT bitmap
	Data   []byte // read data
}

// SectorRange describes a region on a block device in sectors.
type SectorRange struct {
	Offset uint64 // sector offset from the beginning of the disk
	Count  uint64 // number of sectors
}

// SnapshotEventCode identifies the type of snapshot event.
type SnapshotEventCode int

const (
	EventCorrupted SnapshotEventCode = 0
	EventNoSpace   SnapshotEventCode = 1
)

// SnapshotEvent represents an event received from a held snapshot.
type SnapshotEvent struct {
	Code      SnapshotEventCode
	TimeLabel int64 // timestamp of the event
	// Event-specific data:
	Corrupted *SnapshotEventCorrupted
	NoSpace   *SnapshotEventNoSpace
}

// SnapshotEventCorrupted provides details for the EventCorrupted event.
type SnapshotEventCorrupted struct {
	OrigDevIDMajor uint32
	OrigDevIDMinor uint32
	ErrorCode      int32
}

// SnapshotEventNoSpace provides details for the EventNoSpace event.
type SnapshotEventNoSpace struct {
	RequestedSectors uint64
}

// SnapshotImageInfo describes the snapshot image for a device.
type SnapshotImageInfo struct {
	ErrorCode int32  // 0 if no errors, -ENOSPC if overflow, etc.
	Image     string // block device name of the snapshot image, or empty
}

// SnapshotCreateParams holds parameters for creating a snapshot.
type SnapshotCreateParams struct {
	DiffStorageLimitSectors uint64
	DiffStorageFilename     string
}

// blkfilterAttachBuf builds the blkfilter_attach argument buffer.
func blkfilterAttachBuf() []byte {
	buf := make([]byte, 48)
	copy(buf[:], filterName)
	return buf
}

// blkfilterDetachBuf builds the blkfilter_detach argument buffer.
func blkfilterDetachBuf() []byte {
	buf := make([]byte, 32)
	copy(buf[:], filterName)
	return buf
}

// blkfilterCtlBuf builds the blkfilter_ctl argument buffer for a given
// subcommand and option buffer pointer.
func blkfilterCtlBuf(cmd uint32, optBuf []byte) []byte {
	// blkfilter_ctl layout (48 bytes):
	//   u8 name[32] (offset 0)
	//   u32 cmd     (offset 32)
	//   u32 optlen  (offset 36)
	//   u64 opt     (offset 40) — userspace pointer to opt data
	buf := make([]byte, 48)
	copy(buf[:], filterName)
	nativeEndian.PutUint32(buf[32:36], cmd)
	nativeEndian.PutUint32(buf[36:40], uint32(len(optBuf)))
	return buf
}

// snapshotEventBuf creates the argument buffer for IOCTL_BLKSNAP_SNAPSHOT_WAIT_EVENT.
// id is set at offset 0 (16 bytes), timeout_ms at offset 20 (4 bytes).
func snapshotEventBuf(id UUID, timeoutMs uint32) []byte {
	buf := make([]byte, 4096)
	marshalUUID(buf, 0, id)
	nativeEndian.PutUint32(buf[20:24], timeoutMs)
	return buf
}

// unmarshalSnapshotEvent decodes the result of IOCTL_BLKSNAP_SNAPSHOT_WAIT_EVENT.
func unmarshalSnapshotEvent(buf []byte) SnapshotEvent {
	ev := SnapshotEvent{
		Code:      SnapshotEventCode(nativeEndian.Uint32(buf[24:28])),
		TimeLabel: int64(nativeEndian.Uint64(buf[28:36])),
	}
	data := buf[36:]
	switch ev.Code {
	case EventCorrupted:
		ev.Corrupted = &SnapshotEventCorrupted{
			OrigDevIDMajor: nativeEndian.Uint32(data[0:4]),
			OrigDevIDMinor: nativeEndian.Uint32(data[4:8]),
			ErrorCode:      int32(nativeEndian.Uint32(data[8:12])),
		}
	case EventNoSpace:
		ev.NoSpace = &SnapshotEventNoSpace{
			RequestedSectors: nativeEndian.Uint64(data[0:8]),
		}
	}
	return ev
}

// unmarshalCBTInfo decodes blksnap_cbtinfo from the ioctl buffer.
func unmarshalCBTInfo(buf []byte) CBTInfo {
	return CBTInfo{
		DeviceCapacity: nativeEndian.Uint64(buf[0:8]),
		BlockSize:      nativeEndian.Uint32(buf[8:12]),
		BlockCount:     nativeEndian.Uint32(buf[12:16]),
		GenerationID:   unmarshalUUID(buf, 16),
		ChangesNumber:  buf[32],
	}
}

// unmarshalSnapshotInfo decodes blksnap_snapshotinfo from the ioctl buffer.
func unmarshalSnapshotInfo(buf []byte) SnapshotImageInfo {
	var info SnapshotImageInfo
	info.ErrorCode = int32(nativeEndian.Uint32(buf[0:4]))
	// image is a null-terminated string at offset 4, max length imageDiskNameLen
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
