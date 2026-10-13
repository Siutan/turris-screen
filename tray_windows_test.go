//go:build windows

package main

import (
	"testing"
	"unsafe"
)

func TestTrayStructLayout(t *testing.T) {
	var class wndClassEx
	if unsafe.Sizeof(class) != 80 {
		t.Fatalf("WNDCLASSEX size = %d, want 80", unsafe.Sizeof(class))
	}
	if unsafe.Offsetof(class.WndProc) != 8 {
		t.Fatalf("wndproc offset = %d, want 8", unsafe.Offsetof(class.WndProc))
	}

	var msg winMsg
	if unsafe.Offsetof(msg.Wparam) != 16 || unsafe.Offsetof(msg.Time) != 32 {
		t.Fatalf("MSG layout wparam=%d time=%d", unsafe.Offsetof(msg.Wparam), unsafe.Offsetof(msg.Time))
	}

	var nid notifyIconData
	if unsafe.Sizeof(nid) != 976 {
		t.Fatalf("NOTIFYICONDATA size = %d, want 976", unsafe.Sizeof(nid))
	}
	if unsafe.Offsetof(nid.Icon) != 32 || unsafe.Offsetof(nid.Tip) != 40 {
		t.Fatalf("NOTIFYICONDATA icon=%d tip=%d", unsafe.Offsetof(nid.Icon), unsafe.Offsetof(nid.Tip))
	}
}

func TestTrayIconAddsAndRemoves(t *testing.T) {
	startTray(&App{})
	if !trayActive() {
		t.Fatal("tray icon was not created")
	}
	stopTray()
	if trayActive() {
		t.Fatal("tray icon was still registered after stop")
	}
}
