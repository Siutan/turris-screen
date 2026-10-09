package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails binding surface for the cooler display.
type App struct {
	ctx context.Context

	mu      sync.Mutex
	opMu    sync.Mutex
	bridge  *Bridge
	library *Library
	job     JobProgress
}

// BridgeStatus is what the window shows about the cooler connection.
type BridgeStatus struct {
	Connected bool   `json:"connected"`
	Serial    string `json:"serial"`
	Detail    string `json:"detail"`
}

func NewApp() *App {
	lib, err := OpenLibrary()
	if err != nil {
		lib = &Library{}
	}
	return &App{
		bridge:  NewBridge(),
		library: lib,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.Connect()
}

func (a *App) shutdown(context.Context) {
	a.bridge.Close()
}

// Connect opens the KANALI bridge. Quit KANALI first; it holds the same pipe.
func (a *App) Connect() BridgeStatus {
	_ = a.bridge.Connect()
	return a.snapshot()
}

// GetLibrary returns previously shown images, newest first.
func (a *App) GetLibrary() ([]MediaItem, error) {
	if a.library == nil {
		return nil, fmt.Errorf("media library is unavailable")
	}
	return a.library.List()
}

// ActiveName is the file the cooler is set to play. Empty when that is unknown.
func (a *App) ActiveName() string {
	if !a.bridge.Connected() {
		return ""
	}
	name, ok := a.bridge.ActiveMedia()
	if !ok {
		return ""
	}
	return name
}

// ShowImage sends a cropped 1280×720 PNG to the cooler and stores it in the library.
func (a *App) ShowImage(name, pngBase64 string) (MediaItem, error) {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if err := a.bridge.Connect(); err != nil {
		return MediaItem{}, err
	}
	a.progress("Encoding image", 6)
	item, h264Path, err := a.library.Create(name, pngBase64)
	if err != nil {
		a.clearProgress()
		return MediaItem{}, err
	}
	if err := a.publish(item.RemoteName, h264Path); err != nil {
		a.library.Discard(item.ID)
		a.clearProgress()
		return MediaItem{}, err
	}
	a.progress("On the cooler", 100)
	return item, nil
}

// Apply sends a library image to the cooler again.
func (a *App) Apply(id string) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if err := a.bridge.Connect(); err != nil {
		return err
	}
	a.progress("Encoding image", 6)
	item, path, previous, err := a.library.Renew(id)
	if err != nil {
		a.clearProgress()
		return err
	}
	if err := a.publish(item.RemoteName, path); err != nil {
		a.clearProgress()
		return err
	}
	a.removeIfInactive(previous)
	a.progress("On the cooler", 100)
	return nil
}

// Remove drops an image from the library and from the cooler storage.
func (a *App) Remove(id string) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	item, _, err := a.library.File(id)
	if err != nil {
		return err
	}
	a.removeIfInactive(item.RemoteName)
	return a.library.Discard(id)
}

// RestartCooler reboots the display board. Use it when the background stops changing.
func (a *App) RestartCooler() (BridgeStatus, error) {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.progress("Restarting the cooler", 30)
	err := a.bridge.RestartCooler()
	a.clearProgress()
	return a.snapshot(), err
}

// removeIfInactive deletes a file from the cooler unless the panel is set to play it.
// Deleting the playing clip freezes the panel on its last frame.
func (a *App) removeIfInactive(remoteName string) {
	if remoteName == "" || !a.bridge.Connected() {
		return
	}
	active, ok := a.bridge.ActiveMedia()
	if !ok || active == remoteName {
		return
	}
	_ = a.bridge.RemoveMedia(remoteName)
}

func (a *App) publish(remoteName, h264Path string) error {
	a.progress("Sending to the cooler", 12)
	err := a.bridge.TransmitMedia(h264Path, func(sent, total int64) {
		percent := 12.0
		if total > 0 {
			percent = 12 + 80*float64(sent)/float64(total)
		}
		if percent > 92 {
			percent = 92
		}
		a.progress("Sending to the cooler", percent)
	})
	if err != nil {
		return err
	}
	a.progress("Showing on the cooler", 96)
	return a.bridge.ShowMedia(remoteName)
}

// JobProgress is the title-bar transfer state.
type JobProgress struct {
	Active  bool    `json:"active"`
	Label   string  `json:"label"`
	Percent float64 `json:"percent"`
}

// Job returns the transfer in progress. The window polls this while a send is running.
func (a *App) Job() JobProgress {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.job
}

func (a *App) progress(label string, percent float64) {
	job := JobProgress{Active: true, Label: label, Percent: percent}
	a.mu.Lock()
	a.job = job
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return
	}
	runtime.EventsEmit(ctx, "progress", job)
}

func (a *App) clearProgress() {
	a.mu.Lock()
	a.job = JobProgress{}
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return
	}
	runtime.EventsEmit(ctx, "progress", JobProgress{Active: false})
}

func (a *App) snapshot() BridgeStatus {
	serial, detail, ok := a.bridge.Snapshot()
	return BridgeStatus{
		Connected: ok,
		Serial:    serial,
		Detail:    detail,
	}
}

func cleanName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Image"
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '\\' || r == '/' {
			return -1
		}
		return r
	}, name)
	if len(name) > 80 {
		name = name[:80]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "Image " + time.Now().Format("15:04")
	}
	return name
}
