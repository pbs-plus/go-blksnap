package blksnap

import (
	"encoding/binary"
	"fmt"
)

// ModuleVersion represents the kernel module version.
type ModuleVersion struct {
	Major    uint16
	Minor    uint16
	Revision uint16
	Build    uint16
}

func (v ModuleVersion) String() string {
	return fmt.Sprintf("%d.%d.%d.%d", v.Major, v.Minor, v.Revision, v.Build)
}

// UUID is a 16-byte unique identifier used by blksnap for snapshots.
type UUID [16]byte

func (u UUID) String() string {
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		u[0], u[1], u[2], u[3], u[4], u[5], u[6], u[7],
		u[8], u[9], u[10], u[11], u[12], u[13], u[14], u[15])
}

func (u UUID) IsZero() bool { return u == UUID{} }

func ParseUUID(s string) (UUID, error) {
	var u UUID
	if err := parseUUIDHex(s, u[:]); err != nil {
		return UUID{}, err
	}
	return u, nil
}

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

func unmarshalUUID(data []byte, offset int) UUID {
	var u UUID
	copy(u[:], data[offset:offset+16])
	return u
}

func marshalUUID(data []byte, offset int, u UUID) {
	copy(data[offset:offset+16], u[:])
}

// nativeEndian is the host byte order (little-endian on all supported targets).
var nativeEndian = binary.LittleEndian

// CBTInfo holds change block tracking metadata for a block device.
type CBTInfo struct {
	DeviceCapacity uint64
	BlockSize      uint32
	BlockCount     uint32
	GenerationID   UUID
	ChangesNumber  uint8
}

// SectorRange describes a region on a block device in sectors.
type SectorRange struct {
	Offset uint64
	Count  uint64
}

// SnapshotEventCode identifies the type of snapshot event.
type SnapshotEventCode int

const (
	// EventLowFreeSpace (v1) / EventNoSpace (v2) — difference storage limit reached.
	EventLowFreeSpace SnapshotEventCode = 0
	// EventCorrupted — snapshot image corrupted.
	EventCorrupted SnapshotEventCode = 1
)

// SnapshotEvent represents an event received from a held snapshot.
type SnapshotEvent struct {
	Code      SnapshotEventCode
	TimeLabel int64
	Corrupted *SnapshotEventCorrupted
	NoSpace   *SnapshotEventLowFreeSpace
}

// SnapshotEventCorrupted provides details for EventCorrupted.
type SnapshotEventCorrupted struct {
	OrigDevIDMajor uint32
	OrigDevIDMinor uint32
	ErrorCode      int32
}

// SnapshotEventLowFreeSpace provides details when the diff storage is full.
type SnapshotEventLowFreeSpace struct {
	RequestedSectors uint64
}

// SnapshotImageInfo describes the snapshot image for a device (v2 only).
type SnapshotImageInfo struct {
	ErrorCode int32
	Image     string
}

// DevID identifies a block device by major:minor (used in v1 API).
type DevID struct {
	Major uint32
	Minor uint32
}
