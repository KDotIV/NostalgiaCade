//go:build !windows

package library

import (
	"golang.org/x/sys/unix"
	"os"
)

func isReparse(info os.FileInfo) bool { return false }
func availableBytes(path string) (int64, error) {
	var st unix.Statfs_t
	err := unix.Statfs(path, &st)
	return int64(st.Bavail) * int64(st.Bsize), err
}
