//go:build windows

package main

import (
	"fmt"
	"os"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

const (
	wmNull          = 0x0000
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmUser          = 0x0400
	wmTrayCallback  = wmUser + 1
	wmTrayStop      = wmUser + 2

	nimAdd     = 0x00000000
	nimDelete  = 0x00000002
	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString    = 0x00000000
	mfSeparator = 0x00000800

	tpmReturnCmd   = 0x0100
	tpmRightAlign  = 0x0008
	tpmBottomAlign = 0x0020
	tpmNoNotify    = 0x0080

	idiApplication     = 32512
	wsExToolWindow     = 0x00000080
	classAlreadyExists = syscall.Errno(1410)
	trayIconID         = 1
	trayShowID         = 1
	trayExitID         = 2
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	trayKernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procSetForegroundWnd = user32.NewProc("SetForegroundWindow")
	procLoadIconW        = user32.NewProc("LoadIconW")
	procDestroyIcon      = user32.NewProc("DestroyIcon")
	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procExtractIconExW   = shell32.NewProc("ExtractIconExW")
	procGetModuleHandleW = trayKernel32.NewProc("GetModuleHandleW")

	trayCallback = windows.NewCallback(trayProc)

	trayMu    sync.Mutex
	trayApp   *App
	trayHWND  uintptr
	trayUp    atomic.Bool
	trayDone  chan struct{}
	trayIcon  uintptr
	trayOwned bool
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type winMsg struct {
	Hwnd    uintptr
	Message uint32
	_       uint32
	Wparam  uintptr
	Lparam  uintptr
	Time    uint32
	X       int32
	Y       int32
}

type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        windows.GUID
	BalloonIcon     uintptr
}

// prepareTrayRuntime keeps the live clock ticking after the window is hidden.
// WebView2 otherwise slows timers once the page is in the background.
func prepareTrayRuntime() {
	const extra = "--disable-background-timer-throttling --disable-renderer-backgrounding --disable-backgrounding-occluded-windows --disable-features=CalculateNativeWinOcclusion,IntensiveWakeUpThrottling"
	existing := os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS")
	if existing == "" {
		_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", extra)
		return
	}
	_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", existing+" "+extra)
}

func trayActive() bool {
	return trayUp.Load()
}

func startTray(a *App) {
	trayMu.Lock()
	if trayHWND != 0 || trayDone != nil {
		trayMu.Unlock()
		return
	}
	trayApp = a
	done := make(chan struct{})
	trayDone = done
	ready := make(chan struct{})
	trayMu.Unlock()

	go func() {
		defer close(done)
		runTray(ready)
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
	}
}

func stopTray() {
	trayMu.Lock()
	hwnd := trayHWND
	done := trayDone
	trayMu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmTrayStop, 0, 0)
	}
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

func runTray(ready chan struct{}) {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()

	var signal sync.Once
	notify := func() { signal.Do(func() { close(ready) }) }
	defer notify()
	defer func() {
		trayMu.Lock()
		trayHWND = 0
		trayDone = nil
		trayMu.Unlock()
		trayUp.Store(false)
	}()

	className, err := windows.UTF16PtrFromString("TurrisStudioTray")
	if err != nil {
		return
	}
	title, err := windows.UTF16PtrFromString("Turris Studio")
	if err != nil {
		return
	}
	hinst, _, _ := procGetModuleHandleW.Call(0)
	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   trayCallback,
		Instance:  hinst,
		ClassName: className,
	}
	atom, _, regErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 && regErr != classAlreadyExists {
		fmt.Fprintf(os.Stderr, "tray: register window: %v\n", regErr)
		return
	}
	hwnd, _, createErr := procCreateWindowExW.Call(
		wsExToolWindow,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		0,
		0, 0, 0, 0,
		0, 0,
		hinst,
		0,
	)
	if hwnd == 0 {
		fmt.Fprintf(os.Stderr, "tray: create window: %v\n", createErr)
		return
	}
	trayMu.Lock()
	trayHWND = hwnd
	trayMu.Unlock()
	defer func() {
		removeTrayIcon()
		procDestroyWindow.Call(hwnd)
	}()

	icon, owned := loadTrayIcon()
	if icon == 0 {
		fmt.Fprintf(os.Stderr, "tray: no icon\n")
		return
	}
	trayMu.Lock()
	trayIcon = icon
	trayOwned = owned
	trayMu.Unlock()
	if err := addTrayIcon(hwnd, icon); err != nil {
		fmt.Fprintf(os.Stderr, "tray: %v\n", err)
		return
	}
	trayUp.Store(true)
	notify()

	for {
		var msg winMsg
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func trayProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmTrayCallback:
		switch lparam {
		case wmLButtonUp, wmLButtonDblClk:
			showFromTray()
		case wmRButtonUp:
			showTrayMenu(hwnd)
		}
		return 0
	case wmTrayStop:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func showFromTray() {
	app := trayApp
	if app == nil || app.ctx == nil {
		return
	}
	wailsrt.WindowShow(app.ctx)
}

func quitFromTray() {
	app := trayApp
	if app == nil || app.ctx == nil {
		return
	}
	app.quitting.Store(true)
	wailsrt.Quit(app.ctx)
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	appendItem(menu, mfString, trayShowID, "Show Turris Studio")
	appendItem(menu, mfSeparator, 0, "")
	appendItem(menu, mfString, trayExitID, "Exit")

	var pt struct{ X, Y int32 }
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWnd.Call(hwnd)
	flags := uintptr(tpmReturnCmd | tpmRightAlign | tpmBottomAlign | tpmNoNotify)
	cmd, _, _ := procTrackPopupMenu.Call(menu, flags, uintptr(int(pt.X)), uintptr(int(pt.Y)), 0, hwnd, 0)
	procPostMessageW.Call(hwnd, wmNull, 0, 0)
	switch cmd {
	case trayShowID:
		showFromTray()
	case trayExitID:
		quitFromTray()
	}
}

func appendItem(menu uintptr, flags uintptr, id uintptr, label string) {
	var text *uint16
	if label != "" {
		text, _ = windows.UTF16PtrFromString(label)
	}
	procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(text)))
}

func addTrayIcon(hwnd, icon uintptr) error {
	nid := notifyIconData{
		Size:            uint32(unsafe.Sizeof(notifyIconData{})),
		Wnd:             hwnd,
		ID:              trayIconID,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: wmTrayCallback,
		Icon:            icon,
	}
	setUTF16(nid.Tip[:], "Turris Studio")
	r, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	if r == 0 {
		if err == nil {
			return fmt.Errorf("could not add the tray icon")
		}
		return err
	}
	return nil
}

func removeTrayIcon() {
	trayMu.Lock()
	hwnd := trayHWND
	icon := trayIcon
	owned := trayOwned
	trayIcon = 0
	trayOwned = false
	trayMu.Unlock()
	trayUp.Store(false)
	if hwnd != 0 {
		nid := notifyIconData{
			Size: uint32(unsafe.Sizeof(notifyIconData{})),
			Wnd:  hwnd,
			ID:   trayIconID,
		}
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	}
	if owned && icon != 0 {
		procDestroyIcon.Call(icon)
	}
}

func loadTrayIcon() (uintptr, bool) {
	exe, err := os.Executable()
	if err == nil {
		path, err := windows.UTF16PtrFromString(exe)
		if err == nil {
			var large, small uintptr
			n, _, _ := procExtractIconExW.Call(
				uintptr(unsafe.Pointer(path)),
				0,
				uintptr(unsafe.Pointer(&large)),
				uintptr(unsafe.Pointer(&small)),
				1,
			)
			if n > 0 {
				chosen := large
				extra := small
				if chosen == 0 {
					chosen = small
					extra = 0
				}
				if extra != 0 && extra != chosen {
					procDestroyIcon.Call(extra)
				}
				if chosen != 0 {
					return chosen, true
				}
			}
		}
	}
	icon, _, _ := procLoadIconW.Call(0, idiApplication)
	return icon, false
}

func setUTF16(dst []uint16, s string) {
	encoded := utf16.Encode([]rune(s))
	if len(encoded) >= len(dst) {
		encoded = encoded[:len(dst)-1]
	}
	copy(dst, encoded)
}
