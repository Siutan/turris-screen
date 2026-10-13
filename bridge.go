package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"turris-screen/turris"
)

// Bridge owns the USB session to the Turris panel.
type Bridge struct {
	mu     sync.Mutex
	dev    *turris.Device
	serial string
	detail string
}

func NewBridge() *Bridge {
	return &Bridge{}
}

func (b *Bridge) Snapshot() (serial, detail string, connected bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ok := b.dev != nil && b.dev.Healthy()
	return b.serial, b.detail, ok
}

func (b *Bridge) Connected() bool {
	_, _, ok := b.Snapshot()
	return ok
}

// Connect claims the cooler and starts the ping that keeps the picture looping.
func (b *Bridge) Connect() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dev != nil && b.dev.Healthy() {
		return nil
	}
	if b.dev != nil {
		b.dev.Close()
		b.dev = nil
	}
	dev, err := turris.Open()
	if err != nil {
		b.detail = err.Error()
		b.serial = ""
		bridgeLog("connect failed: %v", err)
		return err
	}
	b.dev = dev
	b.serial = dev.Serial()
	if b.serial == "" {
		b.serial = "TRYX TURRIS 620"
	}
	b.detail = "Connected"
	bridgeLog("connected %s", b.serial)
	return nil
}

func (b *Bridge) Close() {
	b.mu.Lock()
	dev := b.dev
	b.dev = nil
	b.serial = ""
	b.mu.Unlock()
	if dev != nil {
		dev.Close()
	}
}

// TransmitMedia uploads a media blob. The basename of path is the name stored on the cooler.
func (b *Bridge) TransmitMedia(path string, onProgress func(sent, total int64)) error {
	dev, err := b.device()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	name := filepath.Base(path)
	bridgeLog("transmit start %s (%d bytes)", name, len(data))
	if err := dev.Upload(name, data, onProgress); err != nil {
		bridgeLog("transmit failed: %v", err)
		return err
	}
	bridgeLog("transmit done %s", name)
	return nil
}

// ShowMedia makes an uploaded file the clip on screen.
func (b *Bridge) ShowMedia(remoteName string) error {
	dev, err := b.device()
	if err != nil {
		return err
	}
	if err := dev.Listed(remoteName, 2*time.Second); err != nil {
		bridgeLog("not listed yet %s", remoteName)
	}
	if err := dev.Play(remoteName); err != nil {
		bridgeLog("show failed: %v", err)
		return err
	}
	bridgeLog("show %s", remoteName)
	if err := dev.LabelError(); err != nil {
		bridgeLog("labels failed: %v", err)
	} else if groups := dev.Labels(); len(groups) > 0 {
		lines := 0
		sample := ""
		for _, group := range groups {
			lines += len(group.Labels)
			if sample == "" && len(group.Labels) > 0 {
				sample = group.Labels[0].Text
			}
		}
		bridgeLog("labels %d groups, %d lines, %q", len(groups), lines, sample)
	}
	return nil
}

// StageLabels keeps the clock and field text for the next show. Play sends it after
// the commit, because that commit clears the text layer.
func (b *Bridge) StageLabels(groups []turris.OverlayGroup) error {
	dev, err := b.device()
	if err != nil {
		return err
	}
	dev.StageLabels(groups)
	return nil
}

// SetLabels replaces the text layer on the clip that is already playing.
func (b *Bridge) SetLabels(groups []turris.OverlayGroup) error {
	dev, err := b.device()
	if err != nil {
		return err
	}
	if err := dev.SetLabels(groups); err != nil {
		bridgeLog("labels failed: %v", err)
		return err
	}
	bridgeLog("labels %d groups", len(groups))
	return nil
}

// UpdateLabelText pushes new strings for labels already on screen.
func (b *Bridge) UpdateLabelText(items []turris.OverlayText) error {
	dev, err := b.device()
	if err != nil {
		return err
	}
	if err := dev.UpdateLabelText(items); err != nil {
		bridgeLog("label text failed: %v", err)
		return err
	}
	return nil
}

// RemoveMedia deletes a user file from the cooler.
func (b *Bridge) RemoveMedia(remoteName string) error {
	dev, err := b.device()
	if err != nil {
		return err
	}
	if err := dev.Remove(remoteName, "media"); err != nil {
		bridgeLog("remove failed %s: %v", remoteName, err)
		return err
	}
	bridgeLog("removed %s", remoteName)
	return nil
}

// ActiveMedia returns the clip the cooler is set to play.
func (b *Bridge) ActiveMedia() (name string, ok bool) {
	dev, err := b.device()
	if err != nil {
		return "", false
	}
	return dev.Active()
}

// RestartCooler reboots the display board and waits for it to come back.
func (b *Bridge) RestartCooler() error {
	dev, err := b.device()
	if err != nil {
		return err
	}
	bridgeLog("restart requested")
	b.mu.Lock()
	b.detail = "Cooler restarting…"
	b.mu.Unlock()
	if err := dev.Reboot(); err != nil {
		bridgeLog("restart failed: %v", err)
		b.setDetail("The cooler did not come back after restarting.")
		return errors.New("the cooler did not come back after restarting. Connect again in a minute")
	}
	b.mu.Lock()
	b.serial = dev.Serial()
	if b.serial == "" {
		b.serial = "TRYX TURRIS 620"
	}
	b.detail = "Connected"
	b.mu.Unlock()
	bridgeLog("cooler back online %s", b.serial)
	return nil
}

func (b *Bridge) device() (*turris.Device, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dev == nil || !b.dev.Healthy() {
		return nil, errors.New("cooler is not connected")
	}
	return b.dev, nil
}

func (b *Bridge) setDetail(msg string) {
	b.mu.Lock()
	b.detail = msg
	b.mu.Unlock()
}

func bridgeLog(format string, args ...any) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, "turris-screen", "bridge.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format("15:04:05.000 ")+format+"\n", args...)
}
