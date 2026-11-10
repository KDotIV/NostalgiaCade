//go:build windows

package library

import (
	"context"
	"errors"
	"fmt"
	"github.com/KdotIV/NostalgiaCade/internal/processenv"
	"golang.org/x/sys/windows"
	"os"
	"sort"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

type nativeProcess struct {
	pid      int
	identity string
	handle   windows.Handle
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
func startNative(exe string, args []string, dir string, overrides ...string) (runningProcess, error) {
	application, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return nil, err
	}
	command, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{exe}, args...)))
	if err != nil {
		return nil, err
	}
	directory, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, err
	}
	environment, err := nativeEnvironment(processenv.Cabinet(append(os.Environ(), overrides...)))
	if err != nil {
		return nil, err
	}
	// A background core may inherit a noninteractive desktop from its host.
	// The Windows cabinet prototype runs on the user's normal interactive
	// desktop; explicitly select it instead of silently creating hidden GUIs.
	desktop, _ := windows.UTF16PtrFromString(`winsta0\default`)
	si := windows.StartupInfo{Desktop: desktop, Flags: windows.STARTF_USESHOWWINDOW, ShowWindow: windows.SW_SHOWNORMAL}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	if err = windows.CreateProcess(application, command, nil, nil, false, windows.CREATE_UNICODE_ENVIRONMENT, &environment[0], directory, &si, &pi); err != nil {
		return nil, err
	}
	windows.CloseHandle(pi.Thread)
	identity, err := processIdentity(pi.Process)
	if err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		windows.CloseHandle(pi.Process)
		return nil, err
	}
	return &nativeProcess{int(pi.ProcessId), identity, pi.Process}, nil
}

func nativeEnvironment(entries []string) ([]uint16, error) {
	// Windows names are case-insensitive; explicit adapter overrides win.
	values := map[string]string{}
	for _, entry := range entries {
		if strings.ContainsRune(entry, 0) {
			return nil, fail("invalid_environment", "environment contains a null character")
		}
		separator := strings.IndexByte(entry, '=')
		if separator == 0 {
			if i := strings.IndexByte(entry[1:], '='); i >= 0 {
				separator = i + 1
			}
		}
		if separator <= 0 {
			continue
		}
		values[strings.ToUpper(entry[:separator])] = entry
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	block := []uint16{}
	for _, name := range names {
		block = append(block, utf16.Encode([]rune(values[name]))...)
		block = append(block, 0)
	}
	if len(block) == 0 {
		block = append(block, 0)
	}
	return append(block, 0), nil
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
	return &nativeProcess{pid, identity, h}, nil
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
			return int(code), err
		}
	}
}

var postMessage = windows.NewLazySystemDLL("user32.dll").NewProc("PostMessageW")
var openDesktop = windows.NewLazySystemDLL("user32.dll").NewProc("OpenDesktopW")
var closeDesktop = windows.NewLazySystemDLL("user32.dll").NewProc("CloseDesktop")
var enumDesktopWindows = windows.NewLazySystemDLL("user32.dll").NewProc("EnumDesktopWindows")

type closeRequest struct {
	pid   uint32
	count int
}

var closeWindowCallback = syscall.NewCallback(func(hwnd uintptr, param unsafe.Pointer) uintptr {
	request := (*closeRequest)(param)
	var pid uint32
	_, _ = windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
	if pid == request.pid {
		postMessage.Call(hwnd, 0x0010, 0, 0)
		request.count++
	}
	return 1
})

func (p *nativeProcess) Stop(force bool) error {
	if force {
		return windows.TerminateProcess(p.handle, 1)
	}
	// Hold a verified process handle; never select or terminate by executable name.
	request := closeRequest{pid: uint32(p.pid)}
	name, _ := windows.UTF16PtrFromString("default")
	desktop, _, err := openDesktop.Call(uintptr(unsafe.Pointer(name)), 0, 0, 0x0040) // DESKTOP_ENUMERATE
	if desktop == 0 {
		return fmt.Errorf("open interactive desktop for graceful exit: %w", err)
	}
	defer closeDesktop.Call(desktop)
	ok, _, err := enumDesktopWindows.Call(desktop, closeWindowCallback, uintptr(unsafe.Pointer(&request)))
	if ok == 0 {
		return fmt.Errorf("enumerate emulator windows: %w", err)
	}
	if request.count == 0 {
		// Recovery compatibility for sessions started by older cores on their
		// inherited desktop. PID filtering still excludes unrelated processes.
		if err := windows.EnumWindows(closeWindowCallback, unsafe.Pointer(&request)); err != nil {
			return err
		}
	}
	if request.count == 0 {
		return fail("no_emulator_window", "the tracked emulator has no window to close yet; wait for startup or use an explicit forced stop")
	}
	return nil
}
func (p *nativeProcess) Release() {
	if p.handle != 0 {
		windows.CloseHandle(p.handle)
		p.handle = 0
	}
}
