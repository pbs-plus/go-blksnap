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
		{"valid UUID", "550e8400-e29b-41d4-a716-446655440000", false},
		{"zero UUID", "00000000-0000-0000-0000-000000000000", false},
		{"empty string", "", true},
		{"too short", "550e8400-e29b-41d4-a716-44665544000", true},
		{"too long", "550e8400-e29b-41d4-a716-4466554400000", true},
		{"no dashes", "550e8400e29b41d4a716446655440000", true},
		{"invalid hex", "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz", true},
		{"parse then string", "550e8400-e29b-41d4-a716-446655440000", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := ParseUUID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseUUID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err == nil {
				got := id.String()
				if got != tt.input {
					t.Errorf("UUID.String() = %q, want %q", got, tt.input)
				}
			}
		})
	}
}

func TestUUIDIsZero(t *testing.T) {
	var zero UUID
	if !zero.IsZero() {
		t.Error("zero UUID should report IsZero() == true")
	}

	nonZero := MustParseUUID("550e8400-e29b-41d4-a716-446655440000")
	if nonZero.IsZero() {
		t.Error("non-zero UUID should report IsZero() == false")
	}
}

func TestMustParseUUID_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseUUID should panic on invalid input")
		}
	}()
	MustParseUUID("invalid")
}

func TestVersionString(t *testing.T) {
	v := Version{Major: 1, Minor: 2, Revision: 3, Build: 4}
	if got := v.String(); got != "1.2.3.4" {
		t.Errorf("Version.String() = %q, want %q", got, "1.2.3.4")
	}
}

func TestCBTMap_Offset(t *testing.T) {
	m := CBTMap{Offset: 1024, Data: []byte{1, 0, 1}}
	if m.Offset != 1024 {
		t.Errorf("CBTMap.Offset = %d, want 1024", m.Offset)
	}
	if len(m.Data) != 3 {
		t.Errorf("len(CBTMap.Data) = %d, want 3", len(m.Data))
	}
}

func TestSectorRange(t *testing.T) {
	r := SectorRange{Offset: 100, Count: 50}
	if r.Offset != 100 || r.Count != 50 {
		t.Errorf("SectorRange = %+v", r)
	}
}

func TestSnapshotEventCode(t *testing.T) {
	if EventCorrupted != 0 {
		t.Errorf("EventCorrupted = %d, want 0", EventCorrupted)
	}
	if EventNoSpace != 1 {
		t.Errorf("EventNoSpace = %d, want 1", EventNoSpace)
	}
}

func TestIOCTLConstants(t *testing.T) {
	if IoctlBlksnapVersion == 0 {
		t.Error("IoctlBlksnapVersion should not be zero")
	}
	if IoctlBlksnapSnapshotCreate == 0 {
		t.Error("IoctlBlksnapSnapshotCreate should not be zero")
	}
	if IoctlBdevfilterAttach == 0 {
		t.Error("IoctlBdevfilterAttach should not be zero")
	}
	if IoctlBdevfilterCtl == 0 {
		t.Error("IoctlBdevfilterCtl should not be zero")
	}
}

func TestErrorSentinelValues(t *testing.T) {
	errs := []struct {
		name string
		err  error
	}{
		{"ErrNotFound", ErrNotFound},
		{"ErrAlreadyExists", ErrAlreadyExists},
		{"ErrInterrupted", ErrInterrupted},
		{"ErrNoData", ErrNoData},
		{"ErrNoSpace", ErrNoSpace},
		{"ErrCorrupted", ErrCorrupted},
	}
	for _, e := range errs {
		if e.err == nil {
			t.Errorf("%s should not be nil", e.name)
		}
	}
}

func TestBytesPtr_Nil(t *testing.T) {
	if got := bytesPtr(nil); got != 0 {
		t.Errorf("bytesPtr(nil) = %d, want 0", got)
	}
	if got := bytesPtr([]byte{}); got != 0 {
		t.Errorf("bytesPtr([]byte{}) = %d, want 0", got)
	}
}

func TestBytesPtr_NonNil(t *testing.T) {
	b := []byte{1, 2, 3}
	ptr := bytesPtr(b)
	if ptr == 0 {
		t.Error("bytesPtr should be non-zero for non-empty slice")
	}
}

func TestBdevfilterAttachBuf(t *testing.T) {
	buf := bdevfilterAttachBuf(0xDEAD)
	if len(buf) != 56 {
		t.Errorf("len(attachBuf) = %d, want 56", len(buf))
	}
	if nativeEndian.Uint64(buf[0:8]) != 0xDEAD {
		t.Errorf("devpath = 0x%x, want 0xDEAD", nativeEndian.Uint64(buf[0:8]))
	}
	if string(buf[8:15]) != filterName {
		t.Errorf("name = %q, want %q", string(buf[8:15]), filterName)
	}
}

func TestBdevfilterNameBuf(t *testing.T) {
	buf := bdevfilterNameBuf(0xBEEF)
	if len(buf) != 40 {
		t.Errorf("len(nameBuf) = %d, want 40", len(buf))
	}
	if nativeEndian.Uint64(buf[0:8]) != 0xBEEF {
		t.Errorf("devpath = 0x%x, want 0xBEEF", nativeEndian.Uint64(buf[0:8]))
	}
	if string(buf[8:15]) != filterName {
		t.Errorf("name = %q, want %q", string(buf[8:15]), filterName)
	}
}

func TestBdevfilterCtlBuf(t *testing.T) {
	buf := bdevfilterCtlBuf(1, make([]byte, 40), 0xCAFE)
	if len(buf) != 56 {
		t.Errorf("len(ctlBuf) = %d, want 56", len(buf))
	}
	if nativeEndian.Uint64(buf[0:8]) != 0xCAFE {
		t.Errorf("devpath = 0x%x, want 0xCAFE", nativeEndian.Uint64(buf[0:8]))
	}
	if string(buf[8:15]) != filterName {
		t.Errorf("name = %q, want %q", string(buf[8:15]), filterName)
	}
	if nativeEndian.Uint32(buf[40:44]) != 1 {
		t.Errorf("cmd = %d, want 1", nativeEndian.Uint32(buf[40:44]))
	}
	if nativeEndian.Uint32(buf[44:48]) != 40 {
		t.Errorf("optlen = %d, want 40", nativeEndian.Uint32(buf[44:48]))
	}
}

func TestSetOptPtr(t *testing.T) {
	buf := make([]byte, 56)
	setOptPtr(buf, 0xDEADBEEF)
	got := nativeEndian.Uint64(buf[48:56])
	if got != 0xDEADBEEF {
		t.Errorf("opt ptr = 0x%x, want 0xDEADBEEF", got)
	}
}

func TestMarshalUnmarshalUUID(t *testing.T) {
	original := MustParseUUID("550e8400-e29b-41d4-a716-446655440000")
	buf := make([]byte, 32)
	marshalUUID(buf, 8, original)
	got := unmarshalUUID(buf, 8)
	if got != original {
		t.Errorf("UUID round-trip failed: got %s, want %s", got, original)
	}
}

func TestSnapshotEventBuf(t *testing.T) {
	id := MustParseUUID("550e8400-e29b-41d4-a716-446655440000")
	buf := snapshotEventBuf(id, 5000)
	if len(buf) != 4096 {
		t.Errorf("len(eventBuf) = %d, want 4096", len(buf))
	}
	gotID := unmarshalUUID(buf, 0)
	if gotID != id {
		t.Errorf("id = %s, want %s", gotID, id)
	}
	if nativeEndian.Uint32(buf[16:20]) != 5000 {
		t.Errorf("timeout = %d, want 5000", nativeEndian.Uint32(buf[16:20]))
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
	if info.DeviceCapacity != 1073741824 {
		t.Errorf("DeviceCapacity = %d, want 1073741824", info.DeviceCapacity)
	}
	if info.BlockSize != 4096 {
		t.Errorf("BlockSize = %d, want 4096", info.BlockSize)
	}
	if info.BlockCount != 262144 {
		t.Errorf("BlockCount = %d, want 262144", info.BlockCount)
	}
	if info.GenerationID != id {
		t.Errorf("GenerationID = %s, want %s", info.GenerationID, id)
	}
	if info.ChangesNumber != 3 {
		t.Errorf("ChangesNumber = %d, want 3", info.ChangesNumber)
	}
}

func TestUnmarshalSnapshotInfo(t *testing.T) {
	buf := make([]byte, 36)
	copy(buf[4:], []byte("vbsnap-image0\x00"))

	info := unmarshalSnapshotInfo(buf)
	if info.ErrorCode != 0 {
		t.Errorf("ErrorCode = %d, want 0", info.ErrorCode)
	}
	if info.Image != "vbsnap-image0" {
		t.Errorf("Image = %q, want \"vbsnap-image0\"", info.Image)
	}
}

func TestUnmarshalSnapshotInfo_Error(t *testing.T) {
	buf := make([]byte, 36)
	binaryEncodeInt32(buf[0:4], -28)

	info := unmarshalSnapshotInfo(buf)
	if info.ErrorCode != -28 {
		t.Errorf("ErrorCode = %d, want -28", info.ErrorCode)
	}
	if info.Image != "" {
		t.Errorf("Image = %q, want empty", info.Image)
	}
}

func TestUnmarshalSnapshotEvent_Corrupted(t *testing.T) {
	buf := make([]byte, 4096)
	id := MustParseUUID("550e8400-e29b-41d4-a716-446655440000")
	marshalUUID(buf, 0, id)
	nativeEndian.PutUint32(buf[16:20], 100)
	nativeEndian.PutUint32(buf[20:24], 0) // EventCorrupted
	nativeEndian.PutUint64(buf[24:32], 1234567890)
	nativeEndian.PutUint32(buf[32:36], 8) // dev_id_mj
	nativeEndian.PutUint32(buf[36:40], 1) // dev_id_mn
	buf[40] = 0xe4                        // err_code: int32 -28 = 0xFFFFFFE4 (LE: e4 ff ff ff)
	buf[41] = 0xff
	buf[42] = 0xff
	buf[43] = 0xff

	ev := unmarshalSnapshotEvent(buf)
	if ev.Code != EventCorrupted {
		t.Errorf("Code = %d, want EventCorrupted", ev.Code)
	}
	if ev.TimeLabel != 1234567890 {
		t.Errorf("TimeLabel = %d, want 1234567890", ev.TimeLabel)
	}
	if ev.Corrupted == nil {
		t.Fatal("Corrupted should not be nil")
	}
	if ev.Corrupted.OrigDevIDMajor != 8 {
		t.Errorf("OrigDevIDMajor = %d, want 8", ev.Corrupted.OrigDevIDMajor)
	}
	if ev.Corrupted.OrigDevIDMinor != 1 {
		t.Errorf("OrigDevIDMinor = %d, want 1", ev.Corrupted.OrigDevIDMinor)
	}
	if ev.Corrupted.ErrorCode != -28 {
		t.Errorf("ErrorCode = %d, want -28", ev.Corrupted.ErrorCode)
	}
}

func TestUnmarshalSnapshotEvent_NoSpace(t *testing.T) {
	buf := make([]byte, 4096)
	id := MustParseUUID("550e8400-e29b-41d4-a716-446655440000")
	marshalUUID(buf, 0, id)
	nativeEndian.PutUint32(buf[16:20], 100)
	nativeEndian.PutUint32(buf[20:24], 1) // EventNoSpace
	nativeEndian.PutUint64(buf[24:32], 1234567890)
	nativeEndian.PutUint64(buf[32:40], 10000) // requested_nr_sect

	ev := unmarshalSnapshotEvent(buf)
	if ev.Code != EventNoSpace {
		t.Errorf("Code = %d, want EventNoSpace", ev.Code)
	}
	if ev.NoSpace == nil {
		t.Fatal("NoSpace should not be nil")
	}
	if ev.NoSpace.RequestedSectors != 10000 {
		t.Errorf("RequestedSectors = %d, want 10000", ev.NoSpace.RequestedSectors)
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
		t.Errorf("Version = %+v, want {1 2 3 4}", v)
	}
}

func TestIoctlConstants_Values(t *testing.T) {
	tests := []struct {
		name string
		got  uintptr
		dir  uintptr
		typ  uintptr
		nr   uintptr
		size uintptr
	}{
		{"Version", IoctlBlksnapVersion, iocRead, blksnapMagic, ioctlVersion, 8},
		{"SnapshotCreate", IoctlBlksnapSnapshotCreate, iocReadWrite, blksnapMagic, ioctlSnapshotCreate, 32},
		{"SnapshotDestroy", IoctlBlksnapSnapshotDestroy, iocWrite, blksnapMagic, ioctlSnapshotDestroy, 16},
		{"SnapshotTake", IoctlBlksnapSnapshotTake, iocWrite, blksnapMagic, ioctlSnapshotTake, 16},
		{"SnapshotCollect", IoctlBlksnapSnapshotCollect, iocRead, blksnapMagic, ioctlSnapshotCollect, 16},
		{"SnapshotWaitEvent", IoctlBlksnapSnapshotWaitEvent, iocRead, blksnapMagic, ioctlSnapshotWaitEvent, 4096},
		{"BdevfilterAttach", IoctlBdevfilterAttach, iocReadWrite, bdevfilterMagic, bdevfilterAttach, 56},
		{"BdevfilterDetach", IoctlBdevfilterDetach, iocReadWrite, bdevfilterMagic, bdevfilterDetach, 40},
		{"BdevfilterCtl", IoctlBdevfilterCtl, iocReadWrite, bdevfilterMagic, bdevfilterCtl, 56},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := (tt.dir << iocDirShift) | (tt.typ << iocTypeShift) |
				(tt.nr << iocNrShift) | (tt.size << iocSizeShift)
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
