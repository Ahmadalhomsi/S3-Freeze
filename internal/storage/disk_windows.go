//go:build windows

package storage

import "golang.org/x/sys/windows"

// DiskUsage reports the total and free bytes of the filesystem holding path.
func DiskUsage(path string) (total, free uint64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var avail, tot, totFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &tot, &totFree); err != nil {
		return 0, 0, err
	}
	return tot, avail, nil
}
