package blksnap

import "fmt"

// CBTHandle provides a high-level interface to Change Block Tracking on a
// block device that has the blksnap filter attached (v2) or is tracked (v1).
type CBTHandle struct {
	tracker *Tracker
}

// NewCBT creates a CBT handle for the given block device.
func NewCBT(devicePath string) (*CBTHandle, error) {
	t, err := OpenTracker(devicePath)
	if err != nil {
		return nil, err
	}
	return &CBTHandle{tracker: t}, nil
}

func (c *CBTHandle) Close() error { return c.tracker.Close() }

func (c *CBTHandle) Info() (CBTInfo, error) { return c.tracker.CBTInfo() }

// Data reads the full CBT bitmap.
func (c *CBTHandle) Data() ([]byte, error) {
	info, err := c.tracker.CBTInfo()
	if err != nil {
		return nil, err
	}
	buf := make([]byte, info.BlockCount)
	if err := c.tracker.ReadCBTMap(0, info.BlockCount, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// DataChunked reads the full CBT bitmap in chunks.
func (c *CBTHandle) DataChunked(chunkSize uint32, fn func(offset uint32, data []byte) error) error {
	info, err := c.tracker.CBTInfo()
	if err != nil {
		return err
	}
	remaining := info.BlockCount
	var offset uint32
	buf := make([]byte, chunkSize)
	for remaining > 0 {
		readLen := min(remaining, chunkSize)
		if err := c.tracker.ReadCBTMap(offset, readLen, buf[:readLen]); err != nil {
			return fmt.Errorf("blksnap: read CBT chunk at offset %d: %w", offset, err)
		}
		if err := fn(offset, buf[:readLen]); err != nil {
			return err
		}
		offset += readLen
		remaining -= readLen
	}
	return nil
}

// Image returns the block device name of the snapshot image (v2 only).
func (c *CBTHandle) Image() (string, error) {
	info, err := c.tracker.SnapshotInfo()
	if err != nil {
		return "", err
	}
	if info.Image == "" {
		return "", fmt.Errorf("blksnap: no snapshot image available")
	}
	return "/dev/" + info.Image, nil
}

// ErrorCode returns the snapshot error code (v2 only).
func (c *CBTHandle) ErrorCode() (int32, error) {
	info, err := c.tracker.SnapshotInfo()
	if err != nil {
		return 0, err
	}
	return info.ErrorCode, nil
}
