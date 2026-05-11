package blksnap

import (
	"errors"

	"golang.org/x/sys/unix"
)

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
func errnoToError(err error) error {
	if err == nil {
		return nil
	}
	e, ok := err.(unix.Errno)
	if !ok {
		return err
	}
	switch e {
	case unix.ENOENT:
		return ErrNotFound
	case unix.EINTR:
		return ErrInterrupted
	case unix.EEXIST:
		return ErrAlreadyExists
	case unix.ENOSPC:
		return ErrNoSpace
	case unix.ENODATA:
		return ErrNoData
	case unix.EALREADY:
		return ErrAlreadyExists
	default:
		return err
	}
}
