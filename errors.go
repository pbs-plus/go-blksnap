package blksnap

import "errors"

// Sentinel errors returned by this package.
var (
	ErrNotFound      = errors.New("blksnap: not found")
	ErrAlreadyExists = errors.New("blksnap: already exists")
	ErrInterrupted   = errors.New("blksnap: interrupted")
	ErrNoData        = errors.New("blksnap: no data available")
	ErrNoSpace       = errors.New("blksnap: no space left on device")
	ErrCorrupted     = errors.New("blksnap: snapshot corrupted")
)

// errnoToError maps common Linux errno values to sentinel errors.
func errnoToError(errno error) error {
	if errno == nil {
		return nil
	}
	var errnum uintptr
	switch e := errno.(type) {
	case interface{ Temporary() bool }:
		// os.SyscallError or similar
		return errno
	case interface{ Errno() uintptr }:
		errnum = e.Errno()
	default:
		return errno
	}
	switch errnum {
	case 2: // ENOENT
		return ErrNotFound
	case 4: // EINTR
		return ErrInterrupted
	case 3: // ESRCH
		return ErrNotFound
	case 17: // EEXIST
		return ErrAlreadyExists
	case 28: // ENOSPC
		return ErrNoSpace
	case 61: // ENODATA
		return ErrNoData
	case 114: // EALREADY
		return ErrAlreadyExists
	default:
		return errno
	}
}
