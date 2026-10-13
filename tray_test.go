package main

import "testing"

func TestHideOnClose(t *testing.T) {
	if !hideOnClose(false, true) {
		t.Fatal("closing the window should leave the app running once the tray icon is up")
	}
	if hideOnClose(true, true) {
		t.Fatal("exit from the tray should quit")
	}
	if hideOnClose(false, false) {
		t.Fatal("without a tray icon, close should quit so the window cannot get stuck")
	}
}
