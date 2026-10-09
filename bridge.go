package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/natefinch/npipe"
)

const pipePath = `\\.\pipe\TryxBridge`

// Bridge talks to UDB.exe, the helper KANALI uses for the Turris display.
type Bridge struct {
	mu        sync.Mutex
	connectMu sync.Mutex
	conn      *pipeClient
	serial    string
	detail    string
	inbox     []packet
	notify    chan struct{}
	done      chan struct{}
	ownsUDB   bool
	udb       *exec.Cmd
	writeMu   sync.Mutex
}

type packet struct {
	Header struct {
		Cmd     string     `json:"cmd"`
		TrackID flexString `json:"trackId"`
		SN      string     `json:"sn"`
	} `json:"header"`
	FileTransmitStatus *struct {
		Status     string `json:"status"`
		DetailInfo string `json:"detailInfo"`
		FileName   string `json:"fileName"`
		FileSize   int64  `json:"fileSize"`
		TransSize  int64  `json:"transSize"`
	} `json:"fileTransmitStatus"`
	FileList       json.RawMessage `json:"fileList"`
	UserConfig     json.RawMessage `json:"userConfig"`
	SysCtlExecResp *struct {
		ExecStatus string `json:"execStatus"`
		Log        string `json:"log"`
	} `json:"sysCtlExecResp"`
}

type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	if string(b) == "null" || len(b) == 0 {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	*f = flexString(bytes.TrimSpace(b))
	return nil
}

func NewBridge() *Bridge {
	return &Bridge{notify: make(chan struct{}, 1)}
}

func (b *Bridge) Snapshot() (serial, detail string, connected bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.serial, b.detail, b.conn != nil && b.serial != ""
}

func (b *Bridge) Connected() bool {
	_, _, ok := b.Snapshot()
	return ok
}

// Connect attaches to the bridge, starting UDB when KANALI has not.
func (b *Bridge) Connect() error {
	b.connectMu.Lock()
	defer b.connectMu.Unlock()

	if err := b.attach(); err == nil {
		return nil
	} else if strings.Contains(err.Error(), "KANALI") {
		b.setDetail(err.Error())
		return err
	}
	// A bridge that has been up for a while can open USB and then never
	// announce the cooler. Restarting it is what makes the Turris appear.
	bridgeLog("cooler did not announce, restarting bridge")
	b.closePipe()
	if err := b.restartUDB(); err != nil {
		b.setDetail(err.Error())
		return err
	}
	if err := b.attach(); err != nil {
		b.setDetail(err.Error())
		return err
	}
	return nil
}

func (b *Bridge) attach() error {
	b.mu.Lock()
	if b.conn != nil && b.serial != "" {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	f, err := openPipe()
	if err != nil && (errors.Is(err, errPipeMissing) || isNotFound(err)) {
		if startErr := b.startUDB(); startErr != nil {
			return startErr
		}
		f, err = waitPipe(8 * time.Second)
	}
	if err != nil {
		if isBusy(err) {
			return errors.New("KANALI is using the cooler. Quit KANALI, then connect again.")
		}
		return err
	}

	b.mu.Lock()
	if b.conn != nil {
		b.conn.Close()
	}
	b.conn = f
	b.serial = ""
	b.inbox = nil
	b.done = make(chan struct{})
	done := b.done
	b.mu.Unlock()

	go b.readLoop(f, done)

	serial, err := b.waitSerial(12 * time.Second)
	if err != nil {
		b.closePipe()
		return err
	}
	b.mu.Lock()
	b.serial = serial
	b.detail = "Connected to " + serial
	b.mu.Unlock()
	bridgeLog("connected %s", serial)
	return nil
}

func (b *Bridge) Close() {
	b.closePipe()
	b.mu.Lock()
	udb := b.udb
	owns := b.ownsUDB
	b.udb = nil
	b.ownsUDB = false
	b.mu.Unlock()
	if owns && udb != nil && udb.Process != nil {
		_ = udb.Process.Kill()
		_, _ = udb.Process.Wait()
	}
}

func (b *Bridge) closePipe() {
	b.mu.Lock()
	conn := b.conn
	done := b.done
	b.conn = nil
	b.serial = ""
	b.done = nil
	b.mu.Unlock()

	if conn != nil {
		conn.Close()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

// TransmitMedia uploads an H.264 file. The file name is the name stored on the cooler.
// onProgress receives bytes sent and the total size while the transfer is running.
func (b *Bridge) TransmitMedia(path string, onProgress func(sent, total int64)) error {
	if !b.Connected() {
		return errors.New("cooler is not connected")
	}
	track := fmt.Sprintf("%d", time.Now().UnixMilli()%1_000_000_000)
	body := map[string]any{
		"header": map[string]any{
			"cmd":     "CMD_File_Transmit",
			"trackId": track,
			"sn":      b.currentSerial(),
		},
		"fileTransmit": map[string]any{
			"fileName": path,
			"fileType": "media",
		},
	}
	bridgeLog("transmit start %s", path)
	if err := b.writeJSON(body); err != nil {
		bridgeLog("transmit write failed: %v", err)
		return err
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			bridgeLog("transmit timed out %s", path)
			return errors.New("upload did not finish: timed out")
		}
		pkt, err := b.wait(remain, func(p packet) bool {
			return p.FileTransmitStatus != nil && string(p.Header.TrackID) == track
		})
		if err != nil {
			bridgeLog("transmit wait failed: %v", err)
			return fmt.Errorf("upload did not finish: %w", err)
		}
		status := pkt.FileTransmitStatus
		bridgeLog("transmit %s %d/%d", status.Status, status.TransSize, status.FileSize)
		if strings.EqualFold(status.Status, "Success") {
			if onProgress != nil && status.FileSize > 0 {
				onProgress(status.FileSize, status.FileSize)
			}
			return nil
		}
		if failedStatus(status.Status) {
			detail := status.DetailInfo
			if detail == "" {
				detail = status.Status
			}
			return fmt.Errorf("cooler rejected the image: %s", detail)
		}
		if onProgress != nil {
			onProgress(status.TransSize, status.FileSize)
		}
	}
}

// ShowMedia makes an uploaded file the image on screen.
func (b *Bridge) ShowMedia(remoteName string) error {
	if !b.Connected() {
		return errors.New("cooler is not connected")
	}
	if err := b.waitUntilListed(remoteName); err != nil {
		return err
	}
	brightness := 0
	if cfg, err := b.readUserConfig(); err == nil {
		if display, ok := configMap(cfg, "displayConfig"); ok {
			if n, ok := display["backlightBrightness"].(float64); ok {
				brightness = int(n)
			}
		}
	}
	body := map[string]any{
		"header": map[string]any{
			"cmd": "CMD_Set_UserConfig",
			"sn":  b.currentSerial(),
		},
		"userConfig": playbackConfig(brightness, remoteName),
	}
	if err := b.writeJSON(body); err != nil {
		return err
	}
	// KANALI sends the run command as its own message a moment later.
	// Glued onto the config message, the cooler saves the name and leaves the old picture up.
	time.Sleep(40 * time.Millisecond)
	run := map[string]any{
		"header": map[string]any{
			"cmd": "CMD_Set_RunConfig",
			"sn":  b.currentSerial(),
		},
		"runConfig": map[string]any{
			"labelGroups": []any{},
		},
	}
	if err := b.writeJSON(run); err != nil {
		return err
	}
	bridgeLog("show %s", remoteName)
	time.Sleep(250 * time.Millisecond)
	return nil
}

// waitUntilListed blocks until the cooler’s media list includes this file.
// Setting the picture before that open fails, and the panel keeps the previous clip.
func (b *Bridge) waitUntilListed(name string) error {
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		body := map[string]any{
			"header": map[string]any{
				"cmd": "CMD_Get_FileList",
				"sn":  b.currentSerial(),
			},
		}
		if err := b.writeJSON(body); err != nil {
			return err
		}
		if _, err := b.wait(800*time.Millisecond, func(p packet) bool {
			return bytes.Contains(p.FileList, []byte(name))
		}); err == nil {
			bridgeLog("listed %s", name)
			return nil
		}
	}
	bridgeLog("not listed %s", name)
	return errors.New("the cooler has not stored the image yet")
}

// RemoveMedia deletes a file from the cooler. Missing files are ignored.
func (b *Bridge) RemoveMedia(remoteName string) error {
	if !b.Connected() {
		return errors.New("cooler is not connected")
	}
	body := map[string]any{
		"header": map[string]any{
			"cmd": "CMD_File_Remove",
			"sn":  b.currentSerial(),
		},
		"fileRemove": map[string]any{
			"fileName": remoteName,
			"fileType": "media",
		},
	}
	return b.writeJSON(body)
}

// ActiveMedia returns the file the cooler is set to play. ok is false when the cooler did not answer.
func (b *Bridge) ActiveMedia() (name string, ok bool) {
	cfg, err := b.readUserConfig()
	if err != nil {
		return "", false
	}
	work, found := configMap(cfg, "workConfig")
	if !found {
		return "", false
	}
	name, _ = work["singleModeMediaFile"].(string)
	return name, true
}

// RestartCooler reboots the display board and waits for it to come back.
// A frozen media player ignores every config change until the board restarts.
func (b *Bridge) RestartCooler() error {
	if !b.Connected() {
		return errors.New("cooler is not connected")
	}
	body := map[string]any{
		"header": map[string]any{
			"cmd": "CMD_SysCtl_Exec",
			"sn":  b.currentSerial(),
		},
		"sys_ctl_exec": map[string]any{
			"cmd":    "Reboot",
			"params": "",
		},
	}
	b.mu.Lock()
	b.inbox = nil
	b.mu.Unlock()
	bridgeLog("restart requested")
	if err := b.writeJSON(body); err != nil {
		return err
	}
	pkt, err := b.wait(15*time.Second, func(p packet) bool {
		return p.Header.Cmd == "CMD_SysCtl_Exec_Resp" || p.Header.Cmd == "CMD_Ntf_DevOffline"
	})
	if err != nil {
		bridgeLog("restart not acknowledged")
		return errors.New("the cooler did not accept the restart. Shut the PC down fully, wait 10 seconds, then start it again")
	}
	if pkt.SysCtlExecResp != nil {
		bridgeLog("restart response %s %q", pkt.SysCtlExecResp.ExecStatus, pkt.SysCtlExecResp.Log)
		if !strings.EqualFold(pkt.SysCtlExecResp.ExecStatus, "Success") {
			detail := strings.TrimSpace(pkt.SysCtlExecResp.Log)
			if detail == "" {
				detail = pkt.SysCtlExecResp.ExecStatus
			}
			return fmt.Errorf("the cooler refused the restart: %s", detail)
		}
		if _, err := b.wait(20*time.Second, func(p packet) bool {
			return p.Header.Cmd == "CMD_Ntf_DevOffline"
		}); err != nil {
			bridgeLog("restart accepted but the cooler stayed online")
			return errors.New("the cooler accepted the restart but did not go offline")
		}
	}
	bridgeLog("cooler offline")
	b.mu.Lock()
	b.serial = ""
	b.detail = "Cooler restarting…"
	b.mu.Unlock()

	pkt, err = b.wait(90*time.Second, func(p packet) bool {
		return p.Header.Cmd == "CMD_Ntf_DevOnline" && p.Header.SN != ""
	})
	if err != nil {
		b.setDetail("The cooler did not come back after restarting.")
		return errors.New("the cooler did not come back after restarting. Connect again in a minute")
	}
	b.mu.Lock()
	b.serial = pkt.Header.SN
	b.detail = "Connected to " + pkt.Header.SN
	b.mu.Unlock()
	bridgeLog("cooler back online %s", pkt.Header.SN)
	return nil
}

func (b *Bridge) readUserConfig() (map[string]any, error) {
	track := fmt.Sprintf("%d", time.Now().UnixMilli()%1_000_000_000)
	body := map[string]any{
		"header": map[string]any{
			"cmd":     "CMD_Get_UserConfig",
			"trackId": track,
			"sn":      b.currentSerial(),
		},
	}
	b.discard(func(p packet) bool { return len(p.UserConfig) > 0 })
	if err := b.writeJSON(body); err != nil {
		return nil, err
	}
	pkt, err := b.wait(900*time.Millisecond, func(p packet) bool {
		return len(p.UserConfig) > 0
	})
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(pkt.UserConfig, &cfg); err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, errors.New("empty user config")
	}
	return cfg, nil
}

// discard drops queued packets that match, so a later wait sees only fresh replies.
func (b *Bridge) discard(match func(packet) bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	next := b.inbox[:0]
	for _, p := range b.inbox {
		if !match(p) {
			next = append(next, p)
		}
	}
	b.inbox = next
}

func (b *Bridge) waitSerial(timeout time.Duration) (string, error) {
	pkt, err := b.wait(timeout, func(p packet) bool {
		return p.Header.SN != "" && (p.Header.Cmd == "CMD_Ntf_DevOnline" || p.Header.Cmd == "CMD_Ntf_FileList")
	})
	if err != nil || pkt.Header.SN == "" {
		return "", errors.New("the Turris did not answer. Check that it is plugged in, then connect again")
	}
	return pkt.Header.SN, nil
}

func (b *Bridge) restartUDB() error {
	b.mu.Lock()
	old := b.udb
	b.udb = nil
	b.ownsUDB = false
	b.mu.Unlock()
	if old != nil && old.Process != nil {
		_ = old.Process.Kill()
		_, _ = old.Process.Wait()
	}
	kill := exec.Command("taskkill", "/IM", "UDB.exe", "/F")
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = kill.Run()
	time.Sleep(400 * time.Millisecond)
	return b.startUDB()
}

func (b *Bridge) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()
	if conn == nil {
		return errors.New("cooler bridge is closed")
	}
	_, err = conn.Write(data)
	return err
}

func (b *Bridge) wait(timeout time.Duration, match func(packet) bool) (packet, error) {
	deadline := time.Now().Add(timeout)
	for {
		if p, ok := b.take(match); ok {
			return p, nil
		}
		remain := time.Until(deadline)
		if remain <= 0 {
			return packet{}, errors.New("timed out")
		}
		if remain > 200*time.Millisecond {
			remain = 200 * time.Millisecond
		}
		select {
		case <-b.notify:
		case <-time.After(remain):
		}
	}
}

func (b *Bridge) take(match func(packet) bool) (packet, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, p := range b.inbox {
		if match(p) {
			b.inbox = append(b.inbox[:i], b.inbox[i+1:]...)
			return p, true
		}
	}
	return packet{}, false
}

func (b *Bridge) readLoop(f *pipeClient, done chan struct{}) {
	defer close(done)
	dec := json.NewDecoder(f)
	dec.UseNumber()
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if !errors.Is(err, io.EOF) {
				b.setDetail("The cooler bridge closed.")
			}
			b.mu.Lock()
			if b.conn == f {
				b.conn = nil
				b.serial = ""
			}
			b.mu.Unlock()
			return
		}
		var p packet
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		b.mu.Lock()
		b.inbox = append(b.inbox, p)
		if len(b.inbox) > 200 {
			b.inbox = b.inbox[len(b.inbox)-200:]
		}
		b.mu.Unlock()
		select {
		case b.notify <- struct{}{}:
		default:
		}
	}
}

func (b *Bridge) currentSerial() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.serial
}

func (b *Bridge) setDetail(msg string) {
	b.mu.Lock()
	b.detail = msg
	b.mu.Unlock()
}

func (b *Bridge) startUDB() error {
	b.mu.Lock()
	old := b.udb
	owns := b.ownsUDB
	b.mu.Unlock()
	if owns && old != nil && old.Process != nil {
		_ = old.Process.Kill()
		_, _ = old.Process.Wait()
	}
	path := findUDB()
	if path == "" {
		return errors.New("KANALI's bridge program was not found. Install KANALI, then connect again")
	}
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start the cooler bridge: %w", err)
	}
	b.mu.Lock()
	b.udb = cmd
	b.ownsUDB = true
	b.mu.Unlock()
	return nil
}

func findUDB() string {
	candidates := []string{
		`C:\Program Files\KANALI\resources\Connect\tm\UDB.exe`,
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// pipeClient reads and writes the bridge at the same time.
// A synchronous file handle cannot: the read blocks the write, and the upload never starts.
type pipeClient struct {
	*npipe.PipeConn
}

func (p *pipeClient) Read(b []byte) (int, error) {
	n, err := p.PipeConn.Read(b)
	// Message-mode pipes return the rest of a long message on the next read.
	if n > 0 && errnoIs(err, 234) {
		return n, nil
	}
	return n, err
}

func openPipe() (*pipeClient, error) {
	conn, err := npipe.DialTimeout(pipePath, 500*time.Millisecond)
	if err != nil {
		if isNotFound(err) || pipeTimedOut(err) {
			return nil, errPipeMissing
		}
		return nil, err
	}
	return &pipeClient{PipeConn: conn}, nil
}

var errPipeMissing = errors.New("cooler bridge is not running")

func pipeTimedOut(err error) bool {
	var timed interface{ Timeout() bool }
	return errors.As(err, &timed) && timed.Timeout()
}

func waitPipe(timeout time.Duration) (*pipeClient, error) {
	var last error
	deadline := time.Now().Add(timeout)
	for {
		f, err := openPipe()
		if err == nil {
			return f, nil
		}
		last = err
		if time.Now().After(deadline) {
			return nil, last
		}
		time.Sleep(200 * time.Millisecond)
	}
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

func isBusy(err error) bool {
	// A connected KANALI client makes CreateFile return access denied.
	// A free instance that is at its client limit returns pipe busy.
	return errnoIs(err, 231) || errnoIs(err, syscall.ERROR_ACCESS_DENIED)
}

func isNotFound(err error) bool {
	return errnoIs(err, syscall.ERROR_FILE_NOT_FOUND) || errnoIs(err, syscall.ERROR_PATH_NOT_FOUND)
}

func errnoIs(err error, code syscall.Errno) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == code
}

func failedStatus(status string) bool {
	switch strings.ToLower(status) {
	case "fail", "failed", "error", "cancel", "canceled", "cancelled":
		return true
	default:
		return false
	}
}
