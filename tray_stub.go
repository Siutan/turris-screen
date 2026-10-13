//go:build !windows

package main

func prepareTrayRuntime() {}

func trayActive() bool { return false }

func startTray(*App) {}

func stopTray() {}
