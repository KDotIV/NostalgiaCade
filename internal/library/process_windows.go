//go:build windows

package library

import (
	"context"
	"errors"
	"fmt"
	"github.com/KdotIV/NostalgiaCade/internal/processenv"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

type nativeProcess struct {
	pid      int
	identity string
	handle   windows.Handle
	cmd      *exec.Cmd
}

func (p *nativeProcess) PID() int         { return p.pid }
func (p *nativeProcess) Identity() string { return p.identity }
func processIdentity(h windows.Handle) (string, error) {
	var c, e, k, u windows.Filetime
	if err := windows.GetProcessTimes(h, &c, &e, &k, &u); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", c.HighDateTime, c.LowDateTime), nil
}
func startNative(exe string, args []string, dir string) (runningProcess, error) {
	cmd := exec.Command(exe, args...)
	cmd.Env = processenv.Cabinet(os.Environ())
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	identity, err := processIdentity(h)
	if err != nil {
		windows.CloseHandle(h)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	return &nativeProcess{cmd.Process.Pid, identity, h, cmd}, nil
}
func recoverNative(pid int, expected string) (runningProcess, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	identity, err := processIdentity(h)
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	status, err := windows.WaitForSingleObject(h, 0)
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	if identity != expected || status == windows.WAIT_OBJECT_0 {
		windows.CloseHandle(h)
		return nil, nil
	}
	return &nativeProcess{pid, identity, h, nil}, nil
}
func (p *nativeProcess) Wait(ctx context.Context) (int, error) {
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		state, err := windows.WaitForSingleObject(p.handle, 200)
		if err != nil {
			return 0, err
		}
		if state == windows.WAIT_OBJECT_0 {
			var code uint32
			err = windows.GetExitCodeProcess(p.handle, &code)
			if p.cmd != nil {
				_ = p.cmd.Wait()
			}
			return int(code), err
		}
	}
}

var postMessage = windows.NewLazySystemDLL("user32.dll").NewProc("PostMessageW")
var closeWindowCallback = syscall.NewCallback(func(hwnd uintptr, param unsafe.Pointer) uintptr {
	var pid uint32
	_, _ = windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
	if pid == *(*uint32)(param) {
		postMessage.Call(hwnd, 0x0010, 0, 0)
	}
	return 1
})

func (p *nativeProcess) Stop(force bool) error {
	if force {
		return windows.TerminateProcess(p.handle, 1)
	}
	// Hold a verified process handle; never select or terminate by executable name.
	var pid = uint32(p.pid)
	return windows.EnumWindows(closeWindowCallback, unsafe.Pointer(&pid))
}
func (p *nativeProcess) Release() {
	if p.handle != 0 {
		windows.CloseHandle(p.handle)
		p.handle = 0
	}
	if p.cmd != nil && p.cmd.ProcessState == nil {
		_ = p.cmd.Process.Release()
	}
}
