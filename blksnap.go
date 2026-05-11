// Package blksnap provides a pure Go interface to the veeamblksnap kernel
// module (VAL-13.0 standalone branch) for block device snapshot management.
//
// The veeamblksnap module provides Change Block Tracking (CBT) and snapshot
// capabilities for block devices. This package communicates directly with
// the kernel module via ioctl system calls.
//
// # Overview
//
// The VAL-13.0 module exposes two interfaces:
//
//  1. The control device (/dev/veeamblksnap) for snapshot lifecycle:
//     Create, Take, Destroy snapshots; Collect active snapshots; Wait for
//     snapshot events.
//
//  2. The bdevfilter device (/dev/bdevfilter) for per-block-device CBT:
//     Attach/Detach the filter; Read CBT maps; Mark dirty blocks;
//     Add devices to snapshots; Get snapshot image info.
//
// # High-Level API
//
// For most use cases, the high-level Session and CBT interfaces are
// recommended:
//
//	session, err := blksnap.CreateSession(devices, diffStoragePath, limit)
//	// ... read snapshot images via session.CBT() ...
//	session.Close()
//
// # Requirements
//
// The veeamblksnap and bdevfilter kernel modules must be loaded.
// This package targets Linux only (linux/amd64, linux/arm64).
package blksnap

import "golang.org/x/sys/unix"

// Control device path for snapshot management ioctls.
const ControlDevice = "/dev/" + ctlName

// Filter device path for bdevfilter ioctls.
const FilterDevice = "/dev/" + bdevFilterDev

const ctlName = "veeamblksnap"
const bdevFilterDev = "bdevfilter"
const filterName = "blksnap"
const imageDiskNameLen = 32
const sectorSize = 512

// BLKSNAP ioctl magic byte.
const blksnapMagic = 'V'

// BDEVFILTER ioctl magic byte.
const bdevfilterMagic = 'F'

// BLKSNAP ioctl command numbers.
const (
	ioctlVersion           = 0
	ioctlSnapshotCreate    = 1
	ioctlSnapshotDestroy   = 2
	ioctlSnapshotTake      = 3
	ioctlSnapshotCollect   = 4
	ioctlSnapshotWaitEvent = 5
)

// BDEVFILTER ioctl command numbers (VAL-13.0).
const (
	bdevfilterAttach = 140
	bdevfilterDetach = 141
	bdevfilterCtl    = 142
	bdevfilterSetlog = 143
)

// BLKFILTER_CTL subcommands for the blksnap filter.
const (
	blkfilterCtlCBTInfo      = 0
	blkfilterCtlCBTMap       = 1
	blkfilterCtlCBTDirty     = 2
	blkfilterCtlSnapshotAdd  = 3
	blkfilterCtlSnapshotInfo = 4
)

// IOC direction bits.
const (
	iocNone      = 0
	iocWrite     = 1
	iocRead      = 2
	iocReadWrite = 3
)

// IOC field shifts (Linux ABI).
const (
	iocNrShift   = 0
	iocTypeShift = 8
	iocSizeShift = 16
	iocDirShift  = 30
)

// Pre-computed ioctl request numbers.
//
//	_IOC(dir,type,nr,size) = (dir<<30)|(type<<8)|(nr<<0)|(size<<16)
const (
	// IOCTL_BLKSNAP_VERSION: _IOR(V, 0, blksnap_version={4*u16=8})
	IoctlBlksnapVersion = (iocRead << iocDirShift) | (blksnapMagic << iocTypeShift) |
		(ioctlVersion << iocNrShift) | (8 << iocSizeShift)

	// IOCTL_BLKSNAP_SNAPSHOT_CREATE: _IOWR(V, 1, blksnap_snapshot_create={u64+u64+uuid[16]=32})
	IoctlBlksnapSnapshotCreate = (iocReadWrite << iocDirShift) | (blksnapMagic << iocTypeShift) |
		(ioctlSnapshotCreate << iocNrShift) | (32 << iocSizeShift)

	// IOCTL_BLKSNAP_SNAPSHOT_DESTROY: _IOW(V, 2, uuid[16]=16)
	IoctlBlksnapSnapshotDestroy = (iocWrite << iocDirShift) | (blksnapMagic << iocTypeShift) |
		(ioctlSnapshotDestroy << iocNrShift) | (16 << iocSizeShift)

	// IOCTL_BLKSNAP_SNAPSHOT_TAKE: _IOW(V, 3, uuid[16]=16)
	IoctlBlksnapSnapshotTake = (iocWrite << iocDirShift) | (blksnapMagic << iocTypeShift) |
		(ioctlSnapshotTake << iocNrShift) | (16 << iocSizeShift)

	// IOCTL_BLKSNAP_SNAPSHOT_COLLECT: _IOR(V, 4, blksnap_snapshot_collect={u32+pad(4)+u64=16})
	IoctlBlksnapSnapshotCollect = (iocRead << iocDirShift) | (blksnapMagic << iocTypeShift) |
		(ioctlSnapshotCollect << iocNrShift) | (16 << iocSizeShift)

	// IOCTL_BLKSNAP_SNAPSHOT_WAIT_EVENT: _IOR(V, 5, snapshot_event=4096)
	IoctlBlksnapSnapshotWaitEvent = (iocRead << iocDirShift) | (blksnapMagic << iocTypeShift) |
		(ioctlSnapshotWaitEvent << iocNrShift) | (4096 << iocSizeShift)

	// BDEVFILTER_ATTACH: _IOWR(F, 140, bdevfilter_attach={u64+u8[32]+u64+u32+pad(4)=56})
	IoctlBdevfilterAttach = (iocReadWrite << iocDirShift) | (bdevfilterMagic << iocTypeShift) |
		(bdevfilterAttach << iocNrShift) | (56 << iocSizeShift)

	// BDEVFILTER_DETACH: _IOWR(F, 141, bdevfilter_name={u64+u8[32]=40})
	IoctlBdevfilterDetach = (iocReadWrite << iocDirShift) | (bdevfilterMagic << iocTypeShift) |
		(bdevfilterDetach << iocNrShift) | (40 << iocSizeShift)

	// BDEVFILTER_CTL: _IOWR(F, 142, bdevfilter_ctl={u64+u8[32]+u32+u32+u64=56})
	IoctlBdevfilterCtl = (iocReadWrite << iocDirShift) | (bdevfilterMagic << iocTypeShift) |
		(bdevfilterCtl << iocNrShift) | (56 << iocSizeShift)
)

// ioctl issues a raw ioctl syscall on fd with the given request and argument pointer.
func ioctl(fd uintptr, req uintptr, arg uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, req, arg)
	if errno != 0 {
		return errno
	}
	return nil
}
