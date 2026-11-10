//go:build windows

// A real Win32 process used only by integration tests. It accepts the adapter
// argument/data-root shapes, records them, and exits on WM_CLOSE.
package main

import (
	"encoding/json"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var user32 = windows.NewLazySystemDLL("user32.dll")
var defWindow = user32.NewProc("DefWindowProcW")
var quit = user32.NewProc("PostQuitMessage")

func main() {
	runtime.LockOSThread()
	user := ""
	save := ""
	for i, a := range os.Args {
		if (a == "--user" || a == "-datapath") && i+1 < len(os.Args) {
			user = os.Args[i+1]
			if a == "-datapath" {
				user = filepath.Join(user, "PCSX2")
			}
		}
		if strings.HasPrefix(a, "Dolphin.General.NANDRootPath=") {
			save = strings.TrimPrefix(a, "Dolphin.General.NANDRootPath=")
		}
	}
	if user == "" {
		user = os.Getenv("RPCS3_CONFIG_DIR")
	}
	if user == "" {
		exe, _ := os.Executable()
		user = filepath.Dir(exe)
	}
	if save == "" {
		save = filepath.Join(user, "fixture-saves")
	}
	if user == "" || save == "" {
		os.Exit(2)
	}
	_ = os.MkdirAll(save, 0700)
	_ = os.WriteFile(filepath.Join(save, "fixture-save.txt"), []byte("shared save survives"), 0600)
	args, _ := json.Marshal(os.Args[1:])
	_ = os.WriteFile(filepath.Join(user, "fixture-args.json"), args, 0600)
	_, hasCatalogCredential := os.LookupEnv("NCADE_POSTGRES_DSN")
	credentialState, _ := json.Marshal(hasCatalogCredential)
	_ = os.WriteFile(filepath.Join(user, "fixture-catalog-credential.json"), credentialState, 0600)
	var startup windows.StartupInfo
	_ = windows.GetStartupInfo(&startup)
	startupState, _ := json.Marshal(map[string]any{"desktop": windows.UTF16PtrToString(startup.Desktop), "showWindow": startup.ShowWindow, "explicitWindowState": startup.Flags&windows.STARTF_USESHOWWINDOW != 0})
	_ = os.WriteFile(filepath.Join(user, "fixture-startup.json"), startupState, 0600)
	type windowClass struct {
		Size, Style                        uint32
		Proc                               uintptr
		ClassExtra, WindowExtra            int32
		Instance, Icon, Cursor, Background uintptr
		MenuName, ClassName                *uint16
		IconSmall                          uintptr
	}
	name, _ := windows.UTF16PtrFromString("NostalgiaCadeTestEmulator")
	callback := syscall.NewCallback(func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
		if msg == 0x10 {
			quit.Call(0)
			return 0
		}
		r, _, _ := defWindow.Call(hwnd, uintptr(msg), wp, lp)
		return r
	})
	wc := windowClass{Proc: callback, ClassName: name}
	wc.Size = uint32(unsafe.Sizeof(wc))
	atom, _, _ := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		os.Exit(3)
	}
	hwnd, _, _ := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), 0, 0, 0, 100, 100, 0, 0, 0, 0)
	if hwnd == 0 {
		os.Exit(4)
	}
	_ = os.WriteFile(filepath.Join(user, "fixture-ready"), []byte("ready"), 0600)
	type message struct {
		Window         uintptr
		Message        uint32
		WParam, LParam uintptr
		Time           uint32
		X, Y           int32
		Private        uint32
	}
	var msg message
	for {
		r, _, _ := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
		user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
	}
	user32.NewProc("DestroyWindow").Call(hwnd)
}
