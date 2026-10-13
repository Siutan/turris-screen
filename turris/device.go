package turris

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const maxMessage = 8 << 20

var errTimeout = errors.New("the cooler did not answer in time")

// Trace, when set, sees each frame written and each body read.
var Trace func(dir string, data []byte, err error)

// link is one bulk pipe to the panel.
type link interface {
	Write(p []byte, timeout time.Duration) error
	Read(timeout time.Duration) ([]byte, error)
	Close() error
}

// candidate is one plugged-in cooler port.
type candidate struct {
	path string
	hint string
}

// Device is a claimed Turris. Playback keeps looping only while this stays open and pinging.
type Device struct {
	mu          sync.Mutex
	io          link
	pending     []byte
	track       uint64
	brightness  int
	poweron     string
	standby     string
	serial      string
	alive       bool
	stop        chan struct{}
	pingStopped chan struct{}
	keeping     bool
	closeOnce   sync.Once
	lastPing    time.Time
	reopen      func() (link, string, error)
	recoverMu   sync.Mutex
}

func newDevice(io link) *Device {
	return &Device{
		io:          io,
		brightness:  80,
		stop:        make(chan struct{}),
		pingStopped: make(chan struct{}),
	}
}

// Serial is the chip id or the serial string the panel reported.
func (d *Device) Serial() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.serial
}

// Healthy is true after a session has been accepted and the pipe is still up.
func (d *Device) Healthy() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.alive && d.io != nil
}

// Close releases the panel. The clip stops looping once the pings stop.
func (d *Device) Close() {
	d.closeOnce.Do(func() { close(d.stop) })
	if d.keeping {
		select {
		case <-d.pingStopped:
		case <-time.After(2 * time.Second):
		}
	}
	d.mu.Lock()
	if d.io != nil {
		_ = d.io.Close()
		d.io = nil
	}
	d.alive = false
	d.mu.Unlock()
}

func (d *Device) startKeepAlive() {
	d.keeping = true
	go d.keepAlive()
}

func (d *Device) keepAlive() {
	defer close(d.pingStopped)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	misses := 0
	for {
		select {
		case <-d.stop:
			return
		case <-ticker.C:
			d.mu.Lock()
			err := d.pingLocked()
			d.mu.Unlock()
			if err != nil {
				misses++
				if misses >= 3 {
					_ = d.recover()
					misses = 0
				}
				continue
			}
			misses = 0
		}
	}
}

// Handshake is the connect sequence: read state, then apply, which does not change the clip.
func (d *Device) Handshake() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	body, err := d.transact(BuildDeviceInfo(d.nextTrack()), 1500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("the cooler did not answer: %w", err)
	}
	d.noteInfo(Strings(body))
	if err := d.optional(BuildAuth(d.nextTrack())); err != nil {
		return err
	}
	if err := d.optional(BuildIdentity(d.nextTrack())); err != nil {
		return err
	}
	if err := d.optional(BuildFileListQuery(d.nextTrack())); err != nil {
		return err
	}
	if body, err := d.transact(BuildUserConfigQuery(d.nextTrack()), 1500*time.Millisecond); err == nil {
		if cfg, err := ParseUserConfig(body); err == nil {
			d.noteConfig(cfg)
		}
	}
	// Apply commits the session. The panel often stays quiet here; a missing reply is still a success.
	if _, err := d.exchange(BuildApply(d.nextTrack()), 400*time.Millisecond, false); err != nil {
		return fmt.Errorf("the cooler did not accept the session: %w", err)
	}
	d.alive = true
	d.lastPing = time.Now()
	return nil
}

func (d *Device) optional(frame []byte) error {
	if err := d.write(frame); err != nil {
		return err
	}
	_, _ = d.recv(800 * time.Millisecond)
	return nil
}

// Info returns the text fields from get_device_info.
func (d *Device) Info() ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	body, err := d.transact(BuildDeviceInfo(d.nextTrack()), 1500*time.Millisecond)
	if err != nil {
		return nil, err
	}
	lines := Strings(body)
	d.noteInfo(lines)
	return lines, nil
}

// Files returns user clips and built-in presets.
func (d *Device) Files() ([]File, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	body, err := d.transact(BuildFileListQuery(d.nextTrack()), 1500*time.Millisecond)
	if err != nil {
		return nil, err
	}
	return ParseFileList(body)
}

// Config returns the active clip and backlight.
func (d *Device) Config() (UserConfig, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.configLocked()
}

func (d *Device) configLocked() (UserConfig, error) {
	body, err := d.transact(BuildUserConfigQuery(d.nextTrack()), 1500*time.Millisecond)
	if err != nil {
		return UserConfig{}, err
	}
	cfg, err := ParseUserConfig(body)
	if err != nil {
		return UserConfig{}, err
	}
	d.noteConfig(cfg)
	return cfg, nil
}

func (d *Device) noteConfig(cfg UserConfig) {
	if cfg.HasBrightness {
		d.brightness = cfg.Brightness
	}
	if cfg.Poweron != "" {
		d.poweron = cfg.Poweron
	}
	if cfg.Standby != "" {
		d.standby = cfg.Standby
	}
}

// Active is the basename of the clip the panel is set to play.
func (d *Device) Active() (string, bool) {
	cfg, err := d.Config()
	if err != nil || cfg.Active == "" {
		return "", false
	}
	return cfg.Active, true
}

// Upload sends a media blob and stores it under name.
func (d *Device) Upload(name string, blob []byte, onProgress func(sent, total int64)) error {
	if !IsMediaBlob(blob) {
		blob = WrapMedia(blob, 60, CountFrames(blob))
	}
	name = Basename(name)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.io == nil {
		return errors.New("cooler is not connected")
	}
	if _, err := d.transact(BuildFileBegin(name, len(blob), d.nextTrack()), 2*time.Second); err != nil {
		return err
	}
	parts := Chunks(blob)
	var sent int
	for _, part := range parts {
		if err := d.keepPingLocked(); err != nil {
			return err
		}
		// A missing chunk reply is normal: the panel often stays quiet until the transfer ends.
		if _, err := d.exchange(BuildFileData(part, d.nextTrack()), 8*time.Second, false); err != nil {
			return err
		}
		sent += len(part)
		if onProgress != nil {
			onProgress(int64(sent), int64(len(blob)))
		}
	}
	if _, err := d.transact(BuildFileEnd("media", d.nextTrack()), 2*time.Second); err != nil {
		return err
	}
	return nil
}

// Play selects a stored clip and commits it. The panel may reconnect afterwards.
func (d *Device) Play(name string) error {
	name = Basename(name)
	d.mu.Lock()
	brightness := d.brightness
	poweron, standby := d.poweron, d.standby
	if cfg, err := d.configLocked(); err == nil {
		if cfg.HasBrightness {
			brightness = cfg.Brightness
		}
		if cfg.Poweron != "" {
			poweron = cfg.Poweron
		}
		if cfg.Standby != "" {
			standby = cfg.Standby
		}
	}
	// The panel often stays quiet after run_config. Apply is what commits the clip.
	frame := BuildRunConfig(name, poweron, standby, brightness, 1, d.nextTrack())
	if _, err := d.exchange(frame, 500*time.Millisecond, false); err != nil {
		d.mu.Unlock()
		return err
	}
	_, err := d.exchange(BuildApply(d.nextTrack()), 1500*time.Millisecond, false)
	d.mu.Unlock()
	// Apply often drops the USB handle as the panel starts the clip.
	if err != nil && !errors.Is(err, errTimeout) {
		return err
	}
	return d.ensureAlive()
}

// Brightness changes the backlight without touching the current clip.
func (d *Device) Brightness(value int) error {
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.exchange(BuildRunConfig("", d.poweron, d.standby, value, 1, d.nextTrack()), 500*time.Millisecond, false); err != nil {
		return err
	}
	if _, err := d.exchange(BuildApply(d.nextTrack()), 1500*time.Millisecond, false); err != nil {
		return err
	}
	d.brightness = value
	return nil
}

// Remove deletes a user file. Presets and the clip that is playing are left alone by the caller.
func (d *Device) Remove(name, category string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Delete often draws no reply. A real error still comes back as a failure.
	_, err := d.exchange(BuildFileRemove(Basename(name), category, d.nextTrack()), 600*time.Millisecond, false)
	return err
}

// Reboot asks the board to restart. It succeeds only after the cooler leaves USB and comes back.
func (d *Device) Reboot() error {
	d.recoverMu.Lock()
	defer d.recoverMu.Unlock()

	d.mu.Lock()
	_, err := d.exchange(BuildSysCtl(SysReboot, "", d.nextTrack()), 2*time.Second, false)
	io := d.io
	d.io = nil
	d.alive = false
	d.pending = nil
	d.mu.Unlock()
	if io != nil {
		_ = io.Close()
	}
	if err != nil && !errors.Is(err, errTimeout) {
		return err
	}
	if !awaitPresent(false, 8*time.Second) {
		_ = d.reopenUntil(8 * time.Second)
		return errors.New("reboot did not restart the cooler")
	}
	if !awaitPresent(true, 20*time.Second) {
		return errors.New("the cooler did not come back")
	}
	return d.reopenUntil(12 * time.Second)
}

func awaitPresent(want bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ok, err := Present()
		if err == nil && ok == want {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

// Listed waits until a file list contains this basename.
func (d *Device) Listed(name string, timeout time.Duration) error {
	name = Basename(name)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		files, err := d.Files()
		if err == nil {
			for _, f := range files {
				if Basename(f.Path) == name {
					return nil
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("the cooler has not stored the image yet")
}

func (d *Device) ensureAlive() error {
	d.mu.Lock()
	err := d.pingLocked()
	d.mu.Unlock()
	if err == nil {
		return nil
	}
	return d.recover()
}

func (d *Device) recover() error {
	d.recoverMu.Lock()
	defer d.recoverMu.Unlock()
	d.mu.Lock()
	if d.alive && d.io != nil {
		err := d.pingLocked()
		d.mu.Unlock()
		if err == nil {
			return nil
		}
		d.mu.Lock()
	}
	if d.io != nil {
		_ = d.io.Close()
		d.io = nil
	}
	d.pending = nil
	d.alive = false
	d.mu.Unlock()
	return d.reopenUntil(12 * time.Second)
}

func (d *Device) reopenUntil(wait time.Duration) error {
	d.mu.Lock()
	reopen := d.reopen
	d.mu.Unlock()
	if reopen == nil {
		return errors.New("the cooler disconnected")
	}
	deadline := time.Now().Add(wait)
	var last error
	for time.Now().Before(deadline) {
		select {
		case <-d.stop:
			return errors.New("the cooler disconnected")
		default:
		}
		time.Sleep(400 * time.Millisecond)
		io, hint, err := reopen()
		if err != nil {
			last = err
			continue
		}
		d.mu.Lock()
		d.io = io
		if hint != "" && d.serial == "" {
			d.serial = hint
		}
		d.pending = nil
		err = d.pingLocked()
		if err == nil {
			d.alive = true
		}
		d.mu.Unlock()
		if err == nil {
			return nil
		}
		last = err
		d.mu.Lock()
		if d.io == io {
			d.io = nil
			d.alive = false
		}
		d.mu.Unlock()
		_ = io.Close()
	}
	if last == nil {
		last = errors.New("the cooler did not come back")
	}
	return last
}

// reopen is set by the USB opener so a dropped panel can be claimed again.
func (d *Device) setReopen(fn func() (link, string, error)) {
	d.mu.Lock()
	d.reopen = fn
	d.mu.Unlock()
}

func (d *Device) pingLocked() error {
	if d.io == nil {
		return errors.New("cooler is not connected")
	}
	_, err := d.transact(BuildPing(d.nextTrack()), 800*time.Millisecond)
	if err != nil {
		d.alive = false
		return err
	}
	d.lastPing = time.Now()
	d.alive = true
	return nil
}

func (d *Device) keepPingLocked() error {
	if time.Since(d.lastPing) < 900*time.Millisecond {
		return nil
	}
	return d.pingLocked()
}

func (d *Device) nextTrack() uint64 {
	d.track++
	if d.track == 0 {
		d.track = 1
	}
	return d.track
}

func (d *Device) noteInfo(lines []string) {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.EqualFold(line, "Turris") {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(line), "TUR") && len(line) >= 8 && hasDigit(line) {
			d.serial = line
			return
		}
	}
}

func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func (d *Device) transact(frame []byte, timeout time.Duration) ([]byte, error) {
	return d.exchange(frame, timeout, true)
}

func (d *Device) exchange(frame []byte, timeout time.Duration, required bool) ([]byte, error) {
	if d.io == nil {
		return nil, errors.New("cooler is not connected")
	}
	if err := d.write(frame); err != nil {
		d.alive = false
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			if !required {
				return nil, nil
			}
			d.alive = false
			return nil, errTimeout
		}
		body, err := d.recv(remain)
		if err != nil {
			if errors.Is(err, errTimeout) && !required {
				return nil, nil
			}
			if !errors.Is(err, errTimeout) {
				d.pending = nil
			}
			d.alive = false
			return nil, err
		}
		if !replyMatches(frame, body) {
			if Trace != nil {
				Trace("skip", body, nil)
			}
			continue
		}
		if err := ErrorFrom(body); err != nil {
			return nil, err
		}
		return body, nil
	}
}

func replyMatches(frame, body []byte) bool {
	if want, ok := frameTrack(frame); ok {
		if got, ok := bodyTrack(body); ok && got != want {
			return false
		}
	}
	return opcodeCompatible(messageOp(frame), messageOp(body))
}

func opcodeCompatible(want, got int) bool {
	if want == 0 || got == 0 || want == got {
		return true
	}
	// The panel answers opcode N with field N+400: 100→500, 400→800.
	return got == want+400
}

func messageOp(frameOrBody []byte) int {
	body := frameOrBody
	if len(body) >= 8 && string(body[:4]) == Magic {
		n := binary.LittleEndian.Uint32(body[4:8])
		if int(n) <= len(body)-8 {
			body = body[8 : 8+n]
		}
	}
	fields, err := Parse(body)
	if err != nil {
		return 0
	}
	op := 0
	for k := range fields {
		if k == 1 || k == 2 || k < op {
			continue
		}
		op = k
	}
	return op
}

func frameTrack(frame []byte) (uint64, bool) {
	if len(frame) < 8 || string(frame[:4]) != Magic {
		return 0, false
	}
	return bodyTrack(frame[8:])
}

func bodyTrack(body []byte) (uint64, bool) {
	fields, err := Parse(body)
	if err != nil || len(fields[1]) == 0 || fields[1][0].Varint {
		return 0, false
	}
	inner, err := Parse(fields[1][0].Bytes)
	if err != nil || len(inner[2]) == 0 || !inner[2][0].Varint {
		return 0, false
	}
	return inner[2][0].Num, true
}

// Call sends one frame and returns the reply body. A timeout is an error.
func (d *Device) Call(frame []byte, timeout time.Duration) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.transact(frame, timeout)
}

// PortReset asks Windows to reset the printer port. The handle usually drops afterwards.
func (d *Device) PortReset() error {
	d.mu.Lock()
	io := d.io
	d.mu.Unlock()
	if io == nil {
		return errors.New("cooler is not connected")
	}
	return resetLink(io)
}

// Present reports whether a Turris is plugged in.
func Present() (bool, error) {
	cands, err := findDevices()
	if err != nil {
		return false, err
	}
	return len(cands) > 0, nil
}

func (d *Device) write(frame []byte) error {
	err := d.io.Write(frame, 8*time.Second)
	if Trace != nil {
		Trace("tx", frame, err)
	}
	return err
}

func (d *Device) recv(timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for {
		if body, ok := d.popFrame(); ok {
			if Trace != nil {
				Trace("rx", body, nil)
			}
			return body, nil
		}
		if !time.Now().Before(deadline) {
			return nil, errTimeout
		}
		chunk, err := d.io.Read(time.Until(deadline))
		if len(chunk) == 0 {
			if err != nil {
				return nil, err
			}
			return nil, errTimeout
		}
		d.absorb(chunk)
	}
}

// absorb adds one USB transfer. A header left over from a quiet command is
// dropped when the next transfer is a complete reply of a different size.
func (d *Device) absorb(chunk []byte) {
	if n, ok := pendingHeader(d.pending); ok && len(d.pending) == 8 && n != len(chunk) && standaloneReply(chunk) {
		d.pending = nil
	}
	d.pending = append(d.pending, chunk...)
}

func (d *Device) popFrame() ([]byte, bool) {
	if body, rest, ok := takeFrame(d.pending); ok {
		d.pending = rest
		return body, true
	}
	if standaloneReply(d.pending) {
		body := d.pending
		d.pending = nil
		return body, true
	}
	return nil, false
}

func pendingHeader(buf []byte) (int, bool) {
	if len(buf) < 8 || string(buf[:4]) != Magic {
		return 0, false
	}
	n := binary.LittleEndian.Uint32(buf[4:8])
	if n > maxMessage {
		return 0, false
	}
	return int(n), true
}

func takeFrame(buf []byte) ([]byte, []byte, bool) {
	i := bytes.Index(buf, []byte(Magic))
	if i < 0 || len(buf[i:]) < 8 {
		return nil, buf, false
	}
	n := binary.LittleEndian.Uint32(buf[i+4 : i+8])
	if n > maxMessage {
		return takeFrame(buf[i+1:])
	}
	total := 8 + int(n)
	if len(buf[i:]) < total {
		return nil, buf, false
	}
	body := append([]byte(nil), buf[i+8:i+total]...)
	rest := append([]byte(nil), buf[i+total:]...)
	return body, rest, true
}

func standaloneReply(buf []byte) bool {
	if len(buf) == 0 || bytes.HasPrefix(buf, []byte(Magic)) {
		return false
	}
	fields, err := Parse(buf)
	if err != nil {
		return false
	}
	_, status := fields[2]
	_, head := fields[1]
	return status || head
}
