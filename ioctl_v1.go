package blksnap

// v1 ioctl constants (VAL-6.x).
const (
	v1IoctlVersion               uintptr = (iocWrite << iocDirShift) | (blksnapMagic << iocTypeShift) | (0 << iocNrShift) | (8 << iocSizeShift)
	v1IoctlTrackerRemove         uintptr = (iocWrite << iocDirShift) | (blksnapMagic << iocTypeShift) | (1 << iocNrShift) | (8 << iocSizeShift)
	v1IoctlTrackerCollect        uintptr = (iocWrite << iocDirShift) | (blksnapMagic << iocTypeShift) | (2 << iocNrShift) | (16 << iocSizeShift)
	v1IoctlTrackerReadCBTMap     uintptr = (iocRead << iocDirShift) | (blksnapMagic << iocTypeShift) | (3 << iocNrShift) | (24 << iocSizeShift)
	v1IoctlTrackerMarkDirty      uintptr = (iocRead << iocDirShift) | (blksnapMagic << iocTypeShift) | (4 << iocNrShift) | (24 << iocSizeShift)
	v1IoctlSnapshotCreate        uintptr = (iocWrite << iocDirShift) | (blksnapMagic << iocTypeShift) | (5 << iocNrShift) | (32 << iocSizeShift)
	v1IoctlSnapshotDestroy       uintptr = (iocRead << iocDirShift) | (blksnapMagic << iocTypeShift) | (6 << iocNrShift) | (16 << iocSizeShift)
	v1IoctlSnapshotTake          uintptr = (iocRead << iocDirShift) | (blksnapMagic << iocTypeShift) | (8 << iocNrShift) | (16 << iocSizeShift)
	v1IoctlSnapshotCollect       uintptr = (iocWrite << iocDirShift) | (blksnapMagic << iocTypeShift) | (9 << iocNrShift) | (16 << iocSizeShift)
	v1IoctlSnapshotCollectImages uintptr = (iocWrite << iocDirShift) | (blksnapMagic << iocTypeShift) | (10 << iocNrShift) | (32 << iocSizeShift)
	v1IoctlSnapshotWaitEvent     uintptr = (iocWrite << iocDirShift) | (blksnapMagic << iocTypeShift) | (11 << iocNrShift) | (4096 << iocSizeShift)
)

// v1 snapshot image info struct (for collect_images).
// struct blk_snap_image_info { dev_t orig_dev_id; dev_t image_dev_id; };
// dev_t = { u32 mj; u32 mn; } = 8 bytes.
// Total: 16 bytes.
const v1ImageInfoSize = 16
