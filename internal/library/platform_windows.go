//go:build windows

package library

import (
	"golang.org/x/sys/windows"
	"os"
	"syscall"
)

func isReparse(info os.FileInfo) bool {
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && d.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
func availableBytes(path string) (int64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var n uint64
	err = windows.GetDiskFreeSpaceEx(p, &n, nil, nil)
	return int64(n), err
}
