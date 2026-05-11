package blksnap

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Session manages a blksnap snapshot session across one or more block devices.
type Session struct {
	snapshot *Snapshot
	devices  []string
	state    *sessionState
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

type sessionState struct {
	mu     sync.Mutex
	errors []string
}

type SessionOption func(*sessionConfig)

type sessionConfig struct {
	logger       *slog.Logger
	eventTimeout time.Duration
}

func WithLogger(logger *slog.Logger) SessionOption {
	return func(c *sessionConfig) { c.logger = logger }
}

func WithEventTimeout(d time.Duration) SessionOption {
	return func(c *sessionConfig) { c.eventTimeout = d }
}

// CreateSession creates a new blksnap session.
//
// For v2 (VAL-13.x): attaches bdevfilter, creates snapshot with diff storage,
// adds devices, starts event monitoring, takes snapshot.
//
// For v1 (VAL-6.x): creates snapshot with device IDs, appends diff storage
// via low_free_space events, starts event monitoring, takes snapshot.
func CreateSession(devices []string, diffStoragePath string, limitBytes uint64, opts ...SessionOption) (*Session, error) {
	if len(devices) == 0 {
		return nil, fmt.Errorf("blksnap: at least one device is required")
	}
	if _, err := Detect(); err != nil {
		return nil, err
	}

	cfg := sessionConfig{
		logger:       slog.Default(),
		eventTimeout: 100 * time.Millisecond,
	}
	for _, o := range opts {
		o(&cfg)
	}

	if detected == APIV1 {
		return createSessionV1(devices, diffStoragePath, limitBytes, cfg)
	}
	return createSessionV2(devices, diffStoragePath, limitBytes, cfg)
}

// --- v2 session ---

func snapCleanupV2(snap *Snapshot) {
	_ = snap.Destroy()
	_ = snap.Close()
}

func createSessionV2(devices []string, diffStoragePath string, limitBytes uint64, cfg sessionConfig) (*Session, error) {
	if diffStoragePath == "" {
		return nil, fmt.Errorf("blksnap: diffStoragePath must not be empty")
	}

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

	snap, err := CreateSnapshot(diffStoragePath, limitBytes)
	if err != nil {
		return nil, fmt.Errorf("blksnap: session setup: %w", err)
	}
	cfg.logger.Info("blksnap snapshot created", "id", snap.ID().String())

	for _, dev := range devices {
		t, err := OpenTracker(dev)
		if err != nil {
			snapCleanupV2(snap)
			return nil, fmt.Errorf("blksnap: session setup: %w", err)
		}
		if err := t.SnapshotAdd(snap.ID()); err != nil {
			_ = t.Close()
			snapCleanupV2(snap)
			return nil, fmt.Errorf("blksnap: add %s to snapshot: %w", dev, err)
		}
		_ = t.Close()
		cfg.logger.Info("device added to snapshot", "device", dev, "id", snap.ID().String())
	}

	state := &sessionState{}

	ev, ok, err := snap.WaitEvent(uint32(cfg.eventTimeout.Milliseconds()))
	if err != nil {
		snapCleanupV2(snap)
		return nil, fmt.Errorf("blksnap: wait for initial allocation: %w", err)
	}
	if ok {
		switch ev.Code {
		case EventCorrupted:
			snapCleanupV2(snap)
			return nil, fmt.Errorf("blksnap: snapshot corrupted for device %d:%d, code %d",
				ev.Corrupted.OrigDevIDMajor, ev.Corrupted.OrigDevIDMinor, ev.Corrupted.ErrorCode)
		case EventLowFreeSpace:
			cfg.logger.Warn("difference storage limit reached during initial allocation")
			state.mu.Lock()
			state.errors = append(state.errors, "difference storage limit reached")
			state.mu.Unlock()
		default:
			snapCleanupV2(snap)
			return nil, fmt.Errorf("blksnap: unexpected event code %d during init", ev.Code)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	session := &Session{snapshot: snap, devices: devices, state: state, cancel: cancel}
	session.wg.Add(1)
	go session.monitorEventsV2(ctx, cfg)

	time.Sleep(time.Microsecond)

	if err := snap.Take(); err != nil {
		cancel()
		session.wg.Wait()
		snapCleanupV2(snap)
		return nil, fmt.Errorf("blksnap: take snapshot: %w", err)
	}
	cfg.logger.Info("blksnap snapshot taken", "id", snap.ID().String())
	return session, nil
}

func (s *Session) monitorEventsV2(ctx context.Context, cfg sessionConfig) {
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
				ev.Corrupted.OrigDevIDMajor, ev.Corrupted.OrigDevIDMinor, ev.Corrupted.ErrorCode)
			cfg.logger.Error(msg, "id", s.snapshot.ID().String())
			s.state.mu.Lock()
			s.state.errors = append(s.state.errors, msg)
			s.state.mu.Unlock()
		case EventLowFreeSpace:
			msg := fmt.Sprintf("difference storage limit reached (requested %d sectors)", ev.NoSpace.RequestedSectors)
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

// --- v1 session ---

func createSessionV1(devices []string, diffStoragePath string, limitBytes uint64, cfg sessionConfig) (*Session, error) {
	// Collect device IDs.
	devIDs := make([]DevID, len(devices))
	for i, dev := range devices {
		t, err := OpenTracker(dev)
		if err != nil {
			return nil, fmt.Errorf("blksnap: session setup: %w", err)
		}
		devIDs[i] = t.V1DevID()
		_ = t.Close()
	}

	// Create snapshot with devices (v1 creates and adds devices in one call).
	snap, _, err := CreateSnapshotV1(devIDs)
	if err != nil {
		return nil, fmt.Errorf("blksnap: session setup: %w", err)
	}
	cfg.logger.Info("blksnap snapshot created (v1)", "id", snap.ID().String())

	state := &sessionState{}

	// Wait for first allocation event.
	ev, ok, err := snap.WaitEvent(uint32(cfg.eventTimeout.Milliseconds()))
	if err != nil {
		_ = snap.Destroy()
		_ = snap.Close()
		return nil, fmt.Errorf("blksnap: wait for initial allocation: %w", err)
	}
	if ok {
		switch ev.Code {
		case EventCorrupted:
			_ = snap.Destroy()
			_ = snap.Close()
			return nil, fmt.Errorf("blksnap: snapshot corrupted for device %d:%d, code %d",
				ev.Corrupted.OrigDevIDMajor, ev.Corrupted.OrigDevIDMinor, ev.Corrupted.ErrorCode)
		case EventLowFreeSpace:
			cfg.logger.Warn("low free space during initial allocation")
			state.mu.Lock()
			state.errors = append(state.errors, "low free space in diff storage")
			state.mu.Unlock()
		default:
			_ = snap.Destroy()
			_ = snap.Close()
			return nil, fmt.Errorf("blksnap: unexpected event code %d during init", ev.Code)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	session := &Session{snapshot: snap, devices: devices, state: state, cancel: cancel}
	session.wg.Add(1)
	go session.monitorEventsV2(ctx, cfg) // same event loop logic

	time.Sleep(time.Microsecond)

	if err := snap.Take(); err != nil {
		cancel()
		session.wg.Wait()
		_ = snap.Destroy()
		_ = snap.Close()
		return nil, fmt.Errorf("blksnap: take snapshot: %w", err)
	}
	cfg.logger.Info("blksnap snapshot taken (v1)", "id", snap.ID().String())
	return session, nil
}

// --- shared ---

func (s *Session) ID() UUID { return s.snapshot.ID() }

func (s *Session) CBTHandle(devicePath string) (*CBTHandle, error) {
	return NewCBT(devicePath)
}

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
