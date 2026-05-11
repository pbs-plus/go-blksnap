package blksnap

import (
	"testing"
)

func TestParseUUID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", "550e8400-e29b-41d4-a716-446655440000", false},
		{"zero", "00000000-0000-0000-0000-000000000000", false},
		{"empty", "", true},
		{"too short", "550e8400-e29b-41d4-a716-44665544000", true},
		{"too long", "550e8400-e29b-41d4-a716-4466554400000", true},
		{"no dashes", "550e8400e29b41d4a716446655440000", true},
		{"invalid hex", "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := ParseUUID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseUUID(%q) error=%v wantErr=%v", tt.input, err, tt.wantErr)
			}
			if err == nil && id.String() != tt.input {
				t.Errorf("round-trip: got %q want %q", id.String(), tt.input)
			}
		})
	}
}

func TestUUIDIsZero(t *testing.T) {
	var zero UUID
	if !zero.IsZero() {
		t.Error("zero UUID should be IsZero")
	}
	if MustParseUUID("550e8400-e29b-41d4-a716-446655440000").IsZero() {
		t.Error("non-zero UUID should not be IsZero")
	}
}

func TestMustParseUUID_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	MustParseUUID("invalid")
}

func TestModuleVersionString(t *testing.T) {
	v := ModuleVersion{1, 2, 3, 4}
	if v.String() != "1.2.3.4" {
		t.Errorf("got %q want %q", v.String(), "1.2.3.4")
	}
}

func TestSnapshotEventCode(t *testing.T) {
	if EventLowFreeSpace != 0 {
		t.Errorf("EventLowFreeSpace=%d want 0", EventLowFreeSpace)
	}
	if EventCorrupted != 1 {
		t.Errorf("EventCorrupted=%d want 1", EventCorrupted)
	}
}

func TestErrorSentinels(t *testing.T) {
	for _, e := range []error{ErrNotFound, ErrAlreadyExists, ErrInterrupted, ErrNoData, ErrNoSpace, ErrCorrupted} {
		if e == nil {
			t.Error("sentinel should not be nil")
		}
	}
}

func TestMarshalUnmarshalUUID(t *testing.T) {
	orig := MustParseUUID("550e8400-e29b-41d4-a716-446655440000")
	buf := make([]byte, 32)
	marshalUUID(buf, 8, orig)
	got := unmarshalUUID(buf, 8)
	if got != orig {
		t.Errorf("round-trip failed: %s vs %s", got, orig)
	}
}

func TestSnapshotEventBuf(t *testing.T) {
	id := MustParseUUID("550e8400-e29b-41d4-a716-446655440000")
	buf := snapshotEventBuf(id, 5000)
	if len(buf) != 4096 {
		t.Fatalf("len=%d want 4096", len(buf))
	}
	if unmarshalUUID(buf, 0) != id {
		t.Error("id mismatch")
	}
	if nativeEndian.Uint32(buf[16:20]) != 5000 {
		t.Errorf("timeout=%d want 5000", nativeEndian.Uint32(buf[16:20]))
	}
}

func TestUnmarshalVersion(t *testing.T) {
	buf := make([]byte, 8)
	nativeEndian.PutUint16(buf[0:2], 1)
	nativeEndian.PutUint16(buf[2:4], 2)
	nativeEndian.PutUint16(buf[4:6], 3)
	nativeEndian.PutUint16(buf[6:8], 4)
	v := unmarshalVersion(buf)
	if v.Major != 1 || v.Minor != 2 || v.Revision != 3 || v.Build != 4 {
		t.Errorf("got %+v", v)
	}
}

func TestUnmarshalCBTInfo(t *testing.T) {
	buf := make([]byte, 40)
	nativeEndian.PutUint64(buf[0:8], 1073741824)
	nativeEndian.PutUint32(buf[8:12], 4096)
	nativeEndian.PutUint32(buf[12:16], 262144)
	id := MustParseUUID("550e8400-e29b-41d4-a716-446655440000")
	marshalUUID(buf, 16, id)
	buf[32] = 3
	info := unmarshalCBTInfo(buf)
	if info.DeviceCapacity != 1073741824 || info.BlockSize != 4096 || info.BlockCount != 262144 ||
		info.GenerationID != id || info.ChangesNumber != 3 {
		t.Errorf("got %+v", info)
	}
}

func TestUnmarshalCBTInfoV1(t *testing.T) {
	buf := make([]byte, 56)
	nativeEndian.PutUint32(buf[0:4], 8)     // dev_id.mj
	nativeEndian.PutUint32(buf[4:8], 1)     // dev_id.mn
	nativeEndian.PutUint32(buf[8:12], 4096) // blk_size
	// pad 4 bytes at 12
	nativeEndian.PutUint64(buf[16:24], 1073741824) // device_capacity
	nativeEndian.PutUint32(buf[24:28], 262144)     // blk_count
	id := MustParseUUID("550e8400-e29b-41d4-a716-446655440000")
	marshalUUID(buf, 28, id)
	buf[44] = 3 // snap_number
	info := unmarshalCBTInfoV1(buf)
	if info.DeviceCapacity != 1073741824 || info.BlockSize != 4096 || info.BlockCount != 262144 ||
		info.GenerationID != id || info.ChangesNumber != 3 {
		t.Errorf("got %+v", info)
	}
}

func TestUnmarshalSnapshotInfo(t *testing.T) {
	buf := make([]byte, 36)
	copy(buf[4:], []byte("vbsnap-image0\x00"))
	info := unmarshalSnapshotInfo(buf)
	if info.ErrorCode != 0 || info.Image != "vbsnap-image0" {
		t.Errorf("got %+v", info)
	}
}

func TestUnmarshalSnapshotInfo_Error(t *testing.T) {
	buf := make([]byte, 36)
	binaryEncodeInt32(buf[0:4], -28)
	info := unmarshalSnapshotInfo(buf)
	if info.ErrorCode != -28 || info.Image != "" {
		t.Errorf("got %+v", info)
	}
}

func TestUnmarshalSnapshotEvent_Corrupted(t *testing.T) {
	buf := make([]byte, 4096)
	id := MustParseUUID("550e8400-e29b-41d4-a716-446655440000")
	marshalUUID(buf, 0, id)
	nativeEndian.PutUint32(buf[16:20], 100)
	nativeEndian.PutUint32(buf[20:24], 1) // EventCorrupted
	nativeEndian.PutUint64(buf[24:32], 1234567890)
	nativeEndian.PutUint32(buf[32:36], 8) // dev_id_mj
	nativeEndian.PutUint32(buf[36:40], 1) // dev_id_mn
	buf[40] = 0xe4
	buf[41] = 0xff
	buf[42] = 0xff
	buf[43] = 0xff
	ev := unmarshalSnapshotEvent(buf)
	if ev.Code != EventCorrupted || ev.TimeLabel != 1234567890 {
		t.Errorf("code/time: %d/%d", ev.Code, ev.TimeLabel)
	}
	if ev.Corrupted == nil || ev.Corrupted.OrigDevIDMajor != 8 || ev.Corrupted.OrigDevIDMinor != 1 || ev.Corrupted.ErrorCode != -28 {
		t.Errorf("corrupted: %+v", ev.Corrupted)
	}
}

func TestUnmarshalSnapshotEvent_LowFreeSpace(t *testing.T) {
	buf := make([]byte, 4096)
	id := MustParseUUID("550e8400-e29b-41d4-a716-446655440000")
	marshalUUID(buf, 0, id)
	nativeEndian.PutUint32(buf[16:20], 100)
	nativeEndian.PutUint32(buf[20:24], 0) // EventLowFreeSpace
	nativeEndian.PutUint64(buf[24:32], 1234567890)
	nativeEndian.PutUint64(buf[32:40], 10000)
	ev := unmarshalSnapshotEvent(buf)
	if ev.Code != EventLowFreeSpace {
		t.Errorf("code=%d", ev.Code)
	}
	if ev.NoSpace == nil || ev.NoSpace.RequestedSectors != 10000 {
		t.Errorf("nospace: %+v", ev.NoSpace)
	}
}

func TestDevID(t *testing.T) {
	d := DevID{Major: 8, Minor: 1}
	if d.Major != 8 || d.Minor != 1 {
		t.Errorf("got %+v", d)
	}
}

func TestIoctlConstantsV2(t *testing.T) {
	tests := []struct {
		name string
		got  uintptr
		dir  uintptr
		nr   uintptr
		size uintptr
	}{
		{"Version", v2IoctlVersion, iocRead, 0, 8},
		{"SnapshotCreate", v2IoctlSnapshotCreate, iocReadWrite, 1, 32},
		{"SnapshotDestroy", v2IoctlSnapshotDestroy, iocWrite, 2, 16},
		{"SnapshotTake", v2IoctlSnapshotTake, iocWrite, 3, 16},
		{"SnapshotCollect", v2IoctlSnapshotCollect, iocRead, 4, 16},
		{"SnapshotWaitEvent", v2IoctlSnapshotWaitEvent, iocRead, 5, 4096},
		{"BdevfilterAttach", v2IoctlBdevfilterAttach, iocReadWrite, 140, 56},
		{"BdevfilterDetach", v2IoctlBdevfilterDetach, iocReadWrite, 141, 40},
		{"BdevfilterCtl", v2IoctlBdevfilterCtl, iocReadWrite, 142, 56},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := (tt.dir << iocDirShift) | (blksnapMagic << iocTypeShift) | (tt.nr << iocNrShift) | (tt.size << iocSizeShift)
			// bdevfilter ioctls use 'F' magic
			if tt.name == "BdevfilterAttach" || tt.name == "BdevfilterDetach" || tt.name == "BdevfilterCtl" {
				want = (tt.dir << iocDirShift) | (bdevfilterMagic << iocTypeShift) | (tt.nr << iocNrShift) | (tt.size << iocSizeShift)
			}
			if tt.got != want {
				t.Errorf("ioctl %s = 0x%x, want 0x%x", tt.name, tt.got, want)
			}
		})
	}
}

func TestIoctlConstantsV1(t *testing.T) {
	tests := []struct {
		name string
		got  uintptr
		dir  uintptr
		nr   uintptr
		size uintptr
	}{
		{"Version", v1IoctlVersion, iocWrite, 0, 8},
		{"SnapshotCreate", v1IoctlSnapshotCreate, iocWrite, 5, 32},
		{"SnapshotDestroy", v1IoctlSnapshotDestroy, iocRead, 6, 16},
		{"SnapshotTake", v1IoctlSnapshotTake, iocRead, 8, 16},
		{"SnapshotCollect", v1IoctlSnapshotCollect, iocWrite, 9, 16},
		{"SnapshotWaitEvent", v1IoctlSnapshotWaitEvent, iocWrite, 11, 4096},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := (tt.dir << iocDirShift) | (blksnapMagic << iocTypeShift) | (tt.nr << iocNrShift) | (tt.size << iocSizeShift)
			if tt.got != want {
				t.Errorf("ioctl %s = 0x%x, want 0x%x", tt.name, tt.got, want)
			}
		})
	}
}

func binaryEncodeInt32(buf []byte, v int32) {
	buf[0] = byte(v)
	buf[1] = byte(v >> 8)
	buf[2] = byte(v >> 16)
	buf[3] = byte(v >> 24)
}
