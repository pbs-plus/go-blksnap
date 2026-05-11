# go-blksnap — Pure Go Block Device Snapshot Library

[![Go Reference](https://pkg.go.dev/badge/github.com/pbs-plus/go-blksnap.svg)](https://pkg.go.dev/github.com/pbs-plus/go-blksnap)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/dl/)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

Pure Go [ioctl](https://man7.org/linux/man-pages/man2/ioctl.2.html)-based client for the
[veeamblksnap](https://github.com/veeam/blksnap) standalone kernel module.
Supports both API generations (VAL-6.x and VAL-13.x) with automatic
version detection. No cgo, no C bindings.

> **Important**: This library auto-detects the loaded kernel module version:
> - **v2** (VAL-13.0 / VAL-13.0.1): `/dev/veeamblksnap` + `/dev/bdevfilter`
> - **v1** (VAL-6.0 through VAL-6.3.2): `/dev/veeamblksnap` only

- [Features](#features)
- [Requirements](#requirements)
- [Kernel module installation](#kernel-module-installation)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Architecture](#architecture)
- [Type reference](#type-reference)
- [License](#license)

## Features

- **Multi-version**: Auto-detects v1 (VAL-6.x) or v2 (VAL-13.x) API
- **Snapshot lifecycle**: Create, Take, Destroy, Collect via `/dev/veeamblksnap`
- **Change Block Tracking**: Attach filter, read CBT bitmap, mark dirty blocks
- **Snapshot events**: Wait for corrupted / low-free-space events with timeout
- **Session management**: goroutine-based event monitor, automatic cleanup
- **Zero cgo**: Only depends on `golang.org/x/sys/unix`

## Requirements

- **Linux** (amd64 or arm64)
- **Go 1.26+**
- **veeamblksnap** kernel module loaded (and **bdevfilter** for v2)
- Root privileges (or `CAP_SYS_ADMIN`) for most operations

## Kernel module installation

The veeamblksnap module must be installed and loaded before this library can
be used. There are two ways to obtain it, depending on your distribution.

### Option 1: Pre-built packages (recommended)

Veeam distributes pre-built blksnap packages through the
[Veeam software repository](https://helpcenter.veeam.com/docs/agentforlinux/userguide/installation_val.html)
as part of Veeam Agent for Linux (free community edition available).

#### Add the repository

1. Download the `veeam-release` package from the
   [Veeam Agent for Linux download page](https://www.veeam.com/linux-backup-download.html)
   (requires a free veeam.com account).

2. Install the repository package:

   ```bash
   # Debian / Ubuntu
   sudo dpkg -i ./veeam-release* && sudo apt-get update

   # RHEL / Rocky / Alma
   sudo rpm -ivh ./veeam-release* && sudo dnf check-update

   # SLES / openSUSE
   sudo zypper in ./veeam-release* && sudo zypper refresh
   ```

> **Alternative**: If you can't use the release package, add the key manually:
> ```bash
> # Debian/Ubuntu
> sudo curl -fsSL https://repository.veeam.com/keys/DEB-9BB5AC67.gpg \
>   -o /usr/share/keyrings/veeam.gpg
> echo "deb [signed-by=/usr/share/keyrings/veeam.gpg] https://repository.veeam.com/backup/linux/agent/dpkg/debian/public stable veeam" | \
>   sudo tee /etc/apt/sources.list.d/veeam.list
> sudo apt-get update
> # RHEL/Rocky/Alma
> sudo rpm --import https://repository.veeam.com/keys/RPM-EFDCEA77
> ```

#### Install the module

```bash
# Debian 11–13 / Ubuntu 22.04 / 24.04
sudo apt-get install blksnap

# RHEL 9 / Rocky 9 / Alma 9 — pre-built kmod
sudo dnf install kmod-blksnap

# RHEL 9 / Rocky 9 / Alma 9 — DKMS (builds from source for your kernel)
sudo dnf install epel-release dkms
sudo dnf install blksnap

# RHEL 8 / Rocky 8 / Alma 8 — pre-built kmod (veeamsnap)
sudo dnf install kmod-veeamsnap

# SLES 15 SP3–SP7 / SLES 16
sudo zypper install blksnap-kmp-default
```

#### Verify

```bash
lsmod | grep -E 'veeamblksnap|bdevfilter'
sudo modprobe veeamblksnap     # if not auto-loaded
sudo modprobe bdevfilter       # v2 only
ls -la /dev/veeamblksnap /dev/bdevfilter
```

#### Secure Boot

If Secure Boot is enabled, enroll the Veeam key:

```bash
sudo mokutil --import /path/to/blksnap.der
# Reboot and follow the MOK Manager prompt
```

### Option 2: Build from source

For development or if pre-built packages don't support your kernel:

```bash
git clone https://github.com/veeam/blksnap.git -b VAL-13.0.1
cd blksnap/module
make -C /lib/modules/$(uname -r)/build M=$(pwd) modules
sudo mkdir -p /lib/modules/$(uname -r)/extra
sudo install -m 0644 veeamblksnap.ko bdevfilter.ko /lib/modules/$(uname -r)/extra/
sudo depmod -a
sudo modprobe veeamblksnap bdevfilter
```

> **Note**: Ensure `linux-headers-$(uname -r)` is installed. For kernel ≥ 6.17,
> use the VAL-13.0.1 branch.

### Troubleshooting

| Symptom | Cause | Solution |
|---------|-------|----------|
| `modprobe: FATAL: Module veeamblksnap not found` | Not installed | Install package or build from source |
| DKMS build fails (kernel ≥ 6.17) | Package too old | Build from VAL-13.0.1 source |
| `Failed to load module` | Kernel/headers mismatch | `kernel-devel`/`linux-headers` must match `uname -r` |
| Secure Boot blocks module | Unsigned | Enroll key via `mokutil` or disable Secure Boot |

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
	session, err := blksnap.CreateSession(
		[]string{"/dev/sda1", "/dev/sda2"},
		"/var/lib/blksnap/diff_storage",
		1024*1024*1024, // 1 GB diff storage limit
	)
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()

	cbt, _ := session.CBTHandle("/dev/sda1")
	info, _ := cbt.Info()
	log.Printf("device=%d blocks, block_size=%d", info.BlockCount, info.BlockSize)

	data, _ := cbt.Data()
	log.Printf("CBT map: %d bytes", len(data))

	img, _ := cbt.Image()
	log.Printf("snapshot image: %s", img)

	if errs, ok := session.Errors(); ok {
		for _, e := range errs {
			log.Printf("snapshot error: %s", e)
		}
	}
}
```

## Architecture

The library auto-detects the API version on first use:

```go
v, _ := blksnap.Detect() // returns APIV1 or APIV2
```

Two kernel interfaces are used depending on the version:

| Interface | API | Go types | Purpose |
|-----------|-----|----------|---------|
| `/dev/veeamblksnap` | v1 + v2 | `Service`, `Snapshot` | Snapshot lifecycle |
| `/dev/bdevfilter` | v2 only | `Tracker` | CBT via path-based device IDs |
| `/dev/veeamblksnap` | v1 only | `Tracker` | CBT via major:minor device IDs |

### Low-level API

```go
// Version query (works on both v1 and v2)
svc, _ := blksnap.OpenService()
ver, _ := svc.Version()
fmt.Println(ver) // "7.0.0.0"

// List active snapshots
ids, _ := svc.Collect()

// CBT on v2 (path-based)
t, _ := blksnap.OpenTracker("/dev/sda1")
t.Attach()
info, _ := t.CBTInfo()
buf := make([]byte, info.BlockCount)
t.ReadCBTMap(0, info.BlockCount, buf)
t.Detach()
t.Close()

// Create and take a snapshot (v2)
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
    case blksnap.EventLowFreeSpace:
        log.Printf("low space: requested=%d sectors",
            ev.NoSpace.RequestedSectors)
    }
}

snap.Destroy()
snap.Close()
```

### v1-specific API

In v1, devices are identified by major:minor numbers and added at snapshot
creation time:

```go
snap, _, _ := blksnap.CreateSnapshotV1([]blksnap.DevID{
    {Major: 8, Minor: 1},  // /dev/sda1
    {Major: 8, Minor: 2},  // /dev/sda2
})

// Get image device mappings
images, _ := snap.CollectImages()
for orig, img := range images {
    fmt.Printf("%d:%d → %d:%d\n", orig.Major, orig.Minor, img.Major, img.Minor)
}
```

### High-level Session API

```go
session, _ := blksnap.CreateSession(
    []string{"/dev/sda1"},
    "/tmp/diff_storage",
    1<<30,
    blksnap.WithLogger(slog.Default()),
    blksnap.WithEventTimeout(50*time.Millisecond),
)
defer session.Close()
```

## Type reference

| C struct (v2) | Go type |
|---------------|---------|
| `struct blksnap_version` | `ModuleVersion` |
| `struct blksnap_uuid` | `UUID` |
| `struct blksnap_cbtinfo` | `CBTInfo` |
| `struct blksnap_sectors` | `SectorRange` |
| `struct blksnap_snapshotinfo` | `SnapshotImageInfo` |
| `struct blksnap_snapshot_event` | `SnapshotEvent` |
| `struct blksnap_event_corrupted` | `SnapshotEventCorrupted` |
| `struct blksnap_event_no_space` | `SnapshotEventLowFreeSpace` |
| `enum blksnap_event_codes` | `SnapshotEventCode` |
| `struct blk_snap_dev_t` (v1) | `DevID` |
| `enum blk_snap_ioctl` (v1) | (internal) |
| `struct bdevfilter_attach/ctl` (v2) | (internal) |

## License

MIT — see [LICENSE](LICENSE).

---

Based on the [blksnap](https://github.com/veeam/blksnap) kernel module UAPI
by Veeam Software Group GmbH. The ioctl protocol derives from kernel UAPI
headers (`veeamblksnap.h`, `bdevfilter.h`, `blk_snap.h`) which are licensed
under GPL-2.0 WITH Linux-syscall-note, permitting independent userspace
implementations under any license.
