package blksnap

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Session manages a blksnap snapshot session across one or more block devices.
// It handles the full lifecycle: attaching trackers, creating the snapshot,
// adding devices, starting the event monitor, taking the snapshot, and
// cleanup.
//
// A typical usage:
//
//	session, err := blksnap.CreateSession(
//	    []string{"/dev/sda1", "/dev/sda2"},
//	    "/var/lib/blksnap/diff_storage",
//	    1024*1024*1024, // 1 GB limit
//	)
//	if err != nil { ... }
//	defer session.Close()
//
//	// Read snapshot images via CBT handles...
type Session struct {
	snapshot *Snapshot
	devices  []string
	state    *sessionState
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// sessionState holds mutable session state shared with the event monitor goroutine.
type sessionState struct {
	mu     sync.Mutex
	errors []string
}

// SessionOption configures a Session.
type SessionOption func(*sessionConfig)

type sessionConfig struct {
	logger       *slog.Logger
	eventTimeout time.Duration
}

// WithLogger sets the logger for session event messages.
func WithLogger(logger *slog.Logger) SessionOption {
	return func(c *sessionConfig) {
		c.logger = logger
	}
}

// WithEventTimeout sets the timeout for event polling. Default is 100ms.
func WithEventTimeout(d time.Duration) SessionOption {
	return func(c *sessionConfig) {
		c.eventTimeout = d
	}
}

// CreateSession creates a new blksnap session for the given block devices.
//
// Parameters:
//   - devices: paths to block devices (e.g., "/dev/sda1")
//   - diffStoragePath: filesystem path for the difference storage file
//   - limitBytes: maximum size of the difference storage in bytes
//   - opts: optional configuration
func CreateSession(devices []string, diffStoragePath string, limitBytes uint64, opts ...SessionOption) (*Session, error) {
	if len(devices) == 0 {
		return nil, fmt.Errorf("blksnap: at least one device is required")
	}
	if diffStoragePath == "" {
		return nil, fmt.Errorf("blksnap: diffStoragePath must not be empty")
	}

	cfg := sessionConfig{
		logger:       slog.Default(),
		eventTimeout: 100 * time.Millisecond,
	}
	for _, o := range opts {
		o(&cfg)
	}

	// Attach blksnap tracker to each device.
	for _, dev := range devices {
		t, err := OpenTracker(dev)
		if err != nil {
			return nil, fmt.Errorf("blksnap: session setup: %w", err)
		}
		if _, err := t.Attach(); err != nil {
			_ = t.Close()
			return nil, fmt.Errorf("blksnap: attach to %s: %w", dev, err)
		}
		_ = t.Close()
	}

	// Create the snapshot.
	snap, err := CreateSnapshot(diffStoragePath, limitBytes)
	if err != nil {
		return nil, fmt.Errorf("blksnap: session setup: %w", err)
	}
	cfg.logger.Info("blksnap snapshot created", "id", snap.ID().String())

	// Cleanup helper for error returns: destroy and close the snapshot.
	snapCleanup := func() {
		_ = snap.Destroy()
		_ = snap.Close()
	}

	// Add each device to the snapshot.
	for _, dev := range devices {
		t, err := OpenTracker(dev)
		if err != nil {
			snapCleanup()
			return nil, fmt.Errorf("blksnap: session setup: %w", err)
		}
		if err := t.SnapshotAdd(snap.ID()); err != nil {
			_ = t.Close()
			snapCleanup()
			return nil, fmt.Errorf("blksnap: add %s to snapshot: %w", dev, err)
		}
		_ = t.Close()
		cfg.logger.Info("device added to snapshot", "device", dev, "id", snap.ID().String())
	}

	state := &sessionState{}

	// Wait for the initial portion of difference storage to be allocated.
	// The kernel sends an event when the first allocation is done.
	ev, ok, err := snap.WaitEvent(uint32(cfg.eventTimeout.Milliseconds()))
	if err != nil {
		snapCleanup()
		return nil, fmt.Errorf("blksnap: wait for initial allocation: %w", err)
	}
	if ok {
		switch ev.Code {
		case EventCorrupted:
			snapCleanup()
			return nil, fmt.Errorf("blksnap: snapshot corrupted for device %d:%d, code %d",
				ev.Corrupted.OrigDevIDMajor, ev.Corrupted.OrigDevIDMinor, ev.Corrupted.ErrorCode)
		case EventNoSpace:
			cfg.logger.Warn("difference storage limit reached during initial allocation")
			state.mu.Lock()
			state.errors = append(state.errors, "difference storage limit reached")
			state.mu.Unlock()
		default:
			snapCleanup()
			return nil, fmt.Errorf("blksnap: unexpected event code %d during init", ev.Code)
		}
	}

	// Start the event monitor goroutine.
	ctx, cancel := context.WithCancel(context.Background())

	session := &Session{
		snapshot: snap,
		devices:  devices,
		state:    state,
		cancel:   cancel,
	}

	session.wg.Add(1)
	go session.monitorEvents(ctx, cfg)

	// Small yield to let the goroutine start.
	time.Sleep(time.Microsecond)

	// Take the snapshot.
	if err := snap.Take(); err != nil {
		cancel()
		session.wg.Wait()
		snapCleanup()
		return nil, fmt.Errorf("blksnap: take snapshot: %w", err)
	}
	cfg.logger.Info("blksnap snapshot taken", "id", snap.ID().String())

	return session, nil
}

// ID returns the snapshot UUID.
func (s *Session) ID() UUID {
	return s.snapshot.ID()
}

// CBTHandle returns a CBT handle for a device in this session.
func (s *Session) CBTHandle(devicePath string) (*CBTHandle, error) {
	return NewCBT(devicePath)
}

// Errors returns any errors that occurred during snapshot holding.
// Returns false if no errors are pending.
func (s *Session) Errors() ([]string, bool) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if len(s.state.errors) == 0 {
		return nil, false
	}
	errs := make([]string, len(s.state.errors))
	copy(errs, s.state.errors)
	s.state.errors = s.state.errors[:0]
	return errs, true
}

// Close terminates the session: stops the event monitor, destroys the
// snapshot, and releases resources. Close is safe to call multiple times.
func (s *Session) Close() error {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.wg.Wait()
	if s.snapshot != nil {
		err := s.snapshot.Destroy()
		closeErr := s.snapshot.Close()
		s.snapshot = nil
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// monitorEvents runs in a goroutine, polling for snapshot events.
func (s *Session) monitorEvents(ctx context.Context, cfg sessionConfig) {
	defer s.wg.Done()

	ticker := time.NewTicker(cfg.eventTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		ev, ok, err := s.snapshot.WaitEvent(uint32(cfg.eventTimeout.Milliseconds()))
		if err != nil {
			cfg.logger.Error("blksnap event monitor error", "id", s.snapshot.ID().String(), "error", err)
			s.state.mu.Lock()
			s.state.errors = append(s.state.errors, err.Error())
			s.state.mu.Unlock()
			return
		}
		if !ok {
			continue
		}

		switch ev.Code {
		case EventCorrupted:
			msg := fmt.Sprintf("snapshot corrupted: device %d:%d, error %d",
				ev.Corrupted.OrigDevIDMajor,
				ev.Corrupted.OrigDevIDMinor,
				ev.Corrupted.ErrorCode)
			cfg.logger.Error(msg, "id", s.snapshot.ID().String())
			s.state.mu.Lock()
			s.state.errors = append(s.state.errors, msg)
			s.state.mu.Unlock()
		case EventNoSpace:
			msg := fmt.Sprintf("difference storage limit reached (requested %d sectors)",
				ev.NoSpace.RequestedSectors)
			cfg.logger.Warn(msg, "id", s.snapshot.ID().String())
			s.state.mu.Lock()
			s.state.errors = append(s.state.errors, msg)
			s.state.mu.Unlock()
		default:
			msg := fmt.Sprintf("unknown blksnap event code %d", ev.Code)
			cfg.logger.Error(msg, "id", s.snapshot.ID().String())
			s.state.mu.Lock()
			s.state.errors = append(s.state.errors, msg)
			s.state.mu.Unlock()
		}
	}
}
