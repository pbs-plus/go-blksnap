# go-blksnap — Pure Go Block Device Snapshot Library

[![Go Reference](https://pkg.go.dev/badge/github.com/pbs-plus/go-blksnap.svg)](https://pkg.go.dev/github.com/pbs-plus/go-blksnap)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/dl/)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

Pure Go [ioctl](https://man7.org/linux/man-pages/man2/ioctl.2.html)-based client for the
[blksnap](https://github.com/veeam/blksnap) Linux kernel module. No cgo, no C
bindings — communicates directly with the kernel via `golang.org/x/sys/unix`.

The upstream blksnap module provides non-persistent block device snapshots with
Change Block Tracking (CBT), enabling incremental and differential backup
workflows.

> **Important**: This library targets the [upstream kernel integration](https://github.com/veeam/blksnap/blob/master/doc/README-upstream-kernel.md)
> branch of blksnap. For standalone kernel module branches (Veeam Agent for Linux),
> see the [upstream repository branches](https://github.com/veeam/blksnap/branches).

- [Features](#features)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Architecture](#architecture)
  - [Low-level API](#low-level-api)
  - [High-level API](#high-level-api)
- [How it works](#how-it-works)
- [Compatibility](#compatibility)
- [License](#license)

## Features

- **Zero dependencies beyond the Go standard library + `golang.org/x/sys`**
- **Snapshot lifecycle**: Create, Take, Destroy, Collect via `/dev/blksnap-control`
- **Change Block Tracking (CBT)**: Attach filter, read CBT bitmap, mark dirty blocks
- **Snapshot events**: Wait for corrupted/no-space events with timeout
- **Session management**: goroutine-based event monitor, automatic cleanup
- **Comprehensive unit tests**: 27 tests covering marshal/unmarshal, ioctl constants,
  UUID parsing, event decoding, buffer layouts

## Requirements

- **Linux** (amd64 or arm64)
- **Go 1.26+**
- **blksnap kernel module** loaded (`modprobe blksnap`)
- Root privileges (or `CAP_SYS_ADMIN`) for most operations

## Installation

```bash
go get github.com/pbs-plus/go-blksnap@latest
```

## Quick start

```go
package main

import (
	"log"

	"github.com/pbs-plus/go-blksnap"
)

func main() {
	// Create a snapshot session for /dev/sda1 and /dev/sda2
	session, err := blksnap.CreateSession(
		[]string{"/dev/sda1", "/dev/sda2"},
		"/var/lib/blksnap/diff_storage",
		1024*1024*1024, // 1 GB diff storage limit
	)
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()

	// Read the CBT bitmap for /dev/sda1
	cbt, _ := session.CBTHandle("/dev/sda1")
	info, _ := cbt.Info()
	log.Printf("device=%d blocks, block_size=%d", info.BlockCount, info.BlockSize)

	data, _ := cbt.Data()
	log.Printf("changed blocks: %d", countChanged(data))

	// Get the snapshot image device name
	img, _ := cbt.Image()
	log.Printf("snapshot image: %s", img)

	// Check for runtime errors
	if errs, ok := session.Errors(); ok {
		for _, e := range errs {
			log.Printf("snapshot error: %s", e)
		}
	}
}
```

## Architecture

The library mirrors the two kernel interfaces exposed by blksnap:

| Kernel interface | Go type | Purpose |
|------------------|---------|---------|
| `/dev/blksnap-control` | `Service`, `Snapshot` | Snapshot lifecycle (create, take, destroy, collect, events) |
| Block device filter | `Tracker` | Per-device CBT, attach/detach, snapshot participation |

### Low-level API

Direct ioctl access for applications needing fine-grained control.

```go
// Query module version
svc, _ := blksnap.OpenService()
v, _ := svc.Version()
fmt.Println(v) // "1.0.0.0"

// List active snapshots
ids, _ := svc.Collect()

// Per-device CBT
t, _ := blksnap.OpenTracker("/dev/sda1")
t.Attach()
info, _ := t.CBTInfo()
map := make([]byte, info.BlockCount)
t.ReadCBTMap(0, info.BlockCount, map)
t.Detach()
t.Close()

// Create and take a snapshot
snap, _ := blksnap.CreateSnapshot("/tmp/diff_storage", 1<<30)
t.SnapshotAdd(snap.ID())
snap.Take()

// Wait for events
for {
    ev, ok, _ := snap.WaitEvent(100)
    if !ok { continue }
    switch ev.Code {
    case blksnap.EventCorrupted:
        log.Printf("corrupted: dev=%d:%d code=%d",
            ev.Corrupted.OrigDevIDMajor,
            ev.Corrupted.OrigDevIDMinor,
            ev.Corrupted.ErrorCode)
    case blksnap.EventNoSpace:
        log.Printf("no space: requested=%d sectors", ev.NoSpace.RequestedSectors)
    }
}

snap.Destroy()
snap.Close()
```

### High-level API

The `Session` type encapsulates the full snapshot workflow:

```go
session, _ := blksnap.CreateSession(
    []string{"/dev/sda1"},
    "/tmp/diff_storage",
    1<<30,
    blksnap.WithLogger(slog.Default()),
    blksnap.WithEventTimeout(50*time.Millisecond),
)
defer session.Close()

// session handles:
//  1. Attaching trackers to each device
//  2. Creating the snapshot with diff storage
//  3. Adding devices to the snapshot
//  4. Starting an event monitor goroutine
//  5. Taking the snapshot
//  6. On Close(): stopping the monitor, destroying the snapshot, releasing FDs
```

### Type mapping

| C struct | Go type |
|----------|---------|
| `struct blksnap_version` | `Version` |
| `struct blksnap_uuid` | `UUID` |
| `struct blksnap_cbtinfo` | `CBTInfo` |
| `struct blksnap_cbtmap` | `CBTMap` |
| `struct blksnap_sectors` | `SectorRange` |
| `struct blksnap_cbtdirty` | (internal) |
| `struct blksnap_snapshotadd` | (internal) |
| `struct blksnap_snapshotinfo` | `SnapshotImageInfo` |
| `struct blksnap_snapshot_create` | `SnapshotCreateParams` |
| `struct blksnap_snapshot_collect` | (internal) |
| `struct blksnap_snapshot_event` | `SnapshotEvent` |
| `struct blksnap_event_corrupted` | `SnapshotEventCorrupted` |
| `struct blksnap_event_no_space` | `SnapshotEventNoSpace` |

## How it works

### Change Block Tracking

The CBT filter tracks which blocks have been modified between snapshots. Each
byte in the CBT map corresponds to one block and stores the snapshot sequence
number when that block was last changed. Comparing two snapshots' CBT data
yields the delta for incremental/differential backups.

### Copy-on-write

When a write occurs to an original device under snapshot, the module reads the
affected chunks and stores them in the difference storage before allowing the
write. Read requests to the snapshot image are served from either the original
device (unchanged data) or the difference storage (overwritten data).

### Difference storage

A single difference storage backs all devices in a snapshot. The library
supports files on regular file systems. The kernel dynamically grows the file
as needed, up to the configured limit.

## Compatibility

| Component | Version / Arch |
|-----------|---------------|
| Go | 1.26+ |
| Linux kernel | Requires blksnap module (upstream integration branch) |
| Architectures | amd64, arm64 |
| C library | None required (pure Go) |

## License

MIT — see [LICENSE](LICENSE).

---

Based on the [blksnap](https://github.com/veeam/blksnap) kernel module UAPI
by Veeam Software Group GmbH. The ioctl protocol and constant values are derived
from Linux kernel UAPI headers (`include/linux/blksnap.h`, `include/linux/blk-filter.h`)
which are licensed under GPL-2.0 WITH Linux-syscall-note, permitting independent
userspace implementations under any license.
