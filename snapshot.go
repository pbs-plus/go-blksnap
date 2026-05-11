package blksnap

import (
	"fmt"
	"os"
	"unsafe"
)

// Service provides low-level access to the blksnap control device
// (/dev/blksnap-control). It can query the module version and list
// active snapshots.
type Service struct {
	ctl *os.File
}

// OpenService opens a connection to the blksnap control device.
func OpenService() (*Service, error) {
	f, err := os.OpenFile(ControlDevice, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("blksnap: failed to open %s: %w", ControlDevice, err)
	}
	return &Service{ctl: f}, nil
}

// Close closes the connection to the control device.
func (s *Service) Close() error {
	return s.ctl.Close()
}

// Version returns the blksnap kernel module version.
func (s *Service) Version() (Version, error) {
	buf := make([]byte, 8)
	if err := ioctl(s.ctl.Fd(), IoctlBlksnapVersion, bytesPtr(buf)); err != nil {
		return Version{}, fmt.Errorf("blksnap: get version: %w", errnoToError(err))
	}
	return unmarshalVersion(buf), nil
}

// Collect returns the UUIDs of all active snapshots.
func (s *Service) Collect() ([]UUID, error) {
	// First call: get the count.
	param := make([]byte, 16)
	if err := ioctl(s.ctl.Fd(), IoctlBlksnapSnapshotCollect, bytesPtr(param)); err != nil {
		return nil, fmt.Errorf("blksnap: collect active snapshots: %w", errnoToError(err))
	}
	count := nativeEndian.Uint32(param[0:4])
	if count == 0 {
		return nil, nil
	}

	// Second call: get the UUIDs.
	idsBuf := make([]byte, count*16)
	nativeEndian.PutUint32(param[0:4], count)
	setOptPtr(param, bytesPtr(idsBuf))

	if err := ioctl(s.ctl.Fd(), IoctlBlksnapSnapshotCollect, bytesPtr(param)); err != nil {
		return nil, fmt.Errorf("blksnap: collect active snapshots: %w", errnoToError(err))
	}

	ids := make([]UUID, count)
	for i := range ids {
		ids[i] = unmarshalUUID(idsBuf, i*16)
	}
	return ids, nil
}

// FD returns the file descriptor for use with Snapshot methods.
func (s *Service) FD() uintptr {
	return s.ctl.Fd()
}

// Snapshot manages a single blksnap snapshot via the control device.
// It provides methods to Create, Take, Destroy snapshots and wait for
// snapshot events.
type Snapshot struct {
	id UUID
	fd uintptr
}

// CreateSnapshot creates a new snapshot with the given difference storage
// file and size limit. The limit is in bytes and is converted to sectors
// internally.
func CreateSnapshot(diffStorageFile string, limitBytes uint64) (*Snapshot, error) {
	if diffStorageFile == "" {
		return nil, fmt.Errorf("blksnap: diffStorageFile must not be empty")
	}

	// Open the control device.
	f, err := os.OpenFile(ControlDevice, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("blksnap: open %s: %w", ControlDevice, err)
	}

	limitSectors := limitBytes / sectorSize

	// Build the ioctl argument: we need to pass a string pointer for the
	// diff_storage_filename field. The kernel uses strncpy_from_user on this
	// address, so we marshal separately.
	filenameBytes := append([]byte(diffStorageFile), 0)
	filenamePtr := uintptr(unsafe.Pointer(&filenameBytes[0]))

	paramBuf := make([]byte, 32)
	nativeEndian.PutUint64(paramBuf[0:8], limitSectors)
	nativeEndian.PutUint64(paramBuf[8:16], uint64(filenamePtr))

	if err := ioctl(f.Fd(), IoctlBlksnapSnapshotCreate, bytesPtr(paramBuf)); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("blksnap: create snapshot: %w", errnoToError(err))
	}

	id := unmarshalUUID(paramBuf, 16)
	return &Snapshot{id: id, fd: f.Fd()}, nil
}

// OpenSnapshot opens an existing snapshot by its UUID.
func OpenSnapshot(id UUID) (*Snapshot, error) {
	f, err := os.OpenFile(ControlDevice, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("blksnap: open %s: %w", ControlDevice, err)
	}
	return &Snapshot{id: id, fd: f.Fd()}, nil
}

// ID returns the snapshot UUID.
func (s *Snapshot) ID() UUID {
	return s.id
}

// Take takes the snapshot, creating snapshot images of all attached devices
// and switching the CBT tables.
func (s *Snapshot) Take() error {
	buf := make([]byte, 16)
	marshalUUID(buf, 0, s.id)
	if err := ioctl(s.fd, IoctlBlksnapSnapshotTake, bytesPtr(buf)); err != nil {
		return fmt.Errorf("blksnap: take snapshot %s: %w", s.id, errnoToError(err))
	}
	return nil
}

// Destroy releases and destroys the snapshot, deleting all snapshot images
// and freeing the difference storage. CBT tracking continues.
func (s *Snapshot) Destroy() error {
	buf := make([]byte, 16)
	marshalUUID(buf, 0, s.id)
	if err := ioctl(s.fd, IoctlBlksnapSnapshotDestroy, bytesPtr(buf)); err != nil {
		return fmt.Errorf("blksnap: destroy snapshot %s: %w", s.id, errnoToError(err))
	}
	return nil
}

// WaitEvent waits for an event from the held snapshot with the given timeout
// in milliseconds. It returns false if no event arrived within the timeout or
// if interrupted, true with the event otherwise.
func (s *Snapshot) WaitEvent(timeoutMs uint32) (SnapshotEvent, bool, error) {
	buf := snapshotEventBuf(s.id, timeoutMs)
	if err := ioctl(s.fd, IoctlBlksnapSnapshotWaitEvent, bytesPtr(buf)); err != nil {
		se := errnoToError(err)
		if se == ErrNotFound || se == ErrInterrupted {
			return SnapshotEvent{}, false, nil
		}
		return SnapshotEvent{}, false, fmt.Errorf("blksnap: wait event for %s: %w", s.id, err)
	}
	return unmarshalSnapshotEvent(buf), true, nil
}
