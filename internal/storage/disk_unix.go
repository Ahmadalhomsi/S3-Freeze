//go:build !windows

package storage

import "golang.org/x/sys/unix"

// DiskUsage reports the total and free bytes of the filesystem holding path.
func DiskUsage(path string) (total, free uint64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return st.Blocks * uint64(st.Bsize), st.Bavail * uint64(st.Bsize), nil
}
