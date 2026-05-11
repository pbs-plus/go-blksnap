package blksnap

// v2 ioctl constants (VAL-13.x).
const (
	v2IoctlVersion           uintptr = (iocRead << iocDirShift) | (blksnapMagic << iocTypeShift) | (0 << iocNrShift) | (8 << iocSizeShift)
	v2IoctlSnapshotCreate    uintptr = (iocReadWrite << iocDirShift) | (blksnapMagic << iocTypeShift) | (1 << iocNrShift) | (32 << iocSizeShift)
	v2IoctlSnapshotDestroy   uintptr = (iocWrite << iocDirShift) | (blksnapMagic << iocTypeShift) | (2 << iocNrShift) | (16 << iocSizeShift)
	v2IoctlSnapshotTake      uintptr = (iocWrite << iocDirShift) | (blksnapMagic << iocTypeShift) | (3 << iocNrShift) | (16 << iocSizeShift)
	v2IoctlSnapshotCollect   uintptr = (iocRead << iocDirShift) | (blksnapMagic << iocTypeShift) | (4 << iocNrShift) | (16 << iocSizeShift)
	v2IoctlSnapshotWaitEvent uintptr = (iocRead << iocDirShift) | (blksnapMagic << iocTypeShift) | (5 << iocNrShift) | (4096 << iocSizeShift)
	v2IoctlBdevfilterAttach  uintptr = (iocReadWrite << iocDirShift) | (bdevfilterMagic << iocTypeShift) | (140 << iocNrShift) | (56 << iocSizeShift)
	v2IoctlBdevfilterDetach  uintptr = (iocReadWrite << iocDirShift) | (bdevfilterMagic << iocTypeShift) | (141 << iocNrShift) | (40 << iocSizeShift)
	v2IoctlBdevfilterCtl     uintptr = (iocReadWrite << iocDirShift) | (bdevfilterMagic << iocTypeShift) | (142 << iocNrShift) | (56 << iocSizeShift)
)

// BLKFILTER_CTL subcommands (same values for v1 tracker and v2 bdevfilter).
const (
	ctlCBTInfo      = 0
	ctlCBTMap       = 1
	ctlCBTDirty     = 2
	ctlSnapshotAdd  = 3
	ctlSnapshotInfo = 4
)
