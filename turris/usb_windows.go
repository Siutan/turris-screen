package turris

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const usbPrintGUID = "{28d78fad-5a12-11d1-ae5b-0000f803a8c2}"

// usbprint SOFT_RESET is function index 16, METHOD_BUFFERED, FILE_DEVICE_UNKNOWN.
const ioctlUSBPrintSoftReset = 0x00220040

// Open claims the Turris and starts the ping that keeps the current clip looping.
func Open() (*Device, error) {
	io, hint, err := openUSB()
	if err != nil {
		return nil, err
	}
	d := claim(io, hint)
	if err := d.Handshake(); err != nil {
		if errors.Is(err, errTimeout) {
			_ = resetLink(io)
		}
		_ = io.Close()
		io, hint, err = reopenUSB(8 * time.Second)
		if err != nil {
			return nil, err
		}
		d = claim(io, hint)
		if err := d.Handshake(); err != nil {
			_ = io.Close()
			return nil, err
		}
	}
	d.startKeepAlive()
	return d, nil
}

func claim(io link, hint string) *Device {
	d := newDevice(io)
	if hint != "" {
		d.serial = hint
	}
	d.setReopen(openUSB)
	return d
}

func reopenUSB(wait time.Duration) (link, string, error) {
	deadline := time.Now().Add(wait)
	var last error
	for {
		io, hint, err := openUSB()
		if err == nil {
			return io, hint, nil
		}
		last = err
		if time.Now().After(deadline) {
			return nil, "", last
		}
		time.Sleep(400 * time.Millisecond)
	}
}

func resetLink(io link) error {
	r, ok := io.(interface{ Reset() error })
	if !ok {
		return nil
	}
	return r.Reset()
}

func openUSB() (link, string, error) {
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			time.Sleep(500 * time.Millisecond)
		}
		io, hint, err := openOnce(attempt > 0)
		if err == nil {
			return io, hint, nil
		}
		last = err
		if strings.Contains(err.Error(), "Quit KANALI") || strings.Contains(err.Error(), "not plugged in") {
			return nil, "", err
		}
	}
	return nil, "", last
}

func openOnce(retried bool) (link, string, error) {
	cands, err := findDevices()
	if err != nil {
		return nil, "", err
	}
	if len(cands) == 0 {
		return nil, "", errors.New("The cooler is not plugged in.")
	}
	var last error
	denied := false
	stalled := false
	for _, c := range cands {
		conn, err := openPath(c.path)
		if err != nil {
			if busyErr(err) {
				denied = true
			}
			last = err
			continue
		}
		if answers(conn) {
			if Trace != nil {
				Trace("open", []byte(c.path), nil)
			}
			return conn, c.hint, nil
		}
		if Trace != nil {
			Trace("silent", []byte(c.path), nil)
		}
		stalled = true
		if err := resetLink(conn); err != nil && Trace != nil {
			Trace("reset", nil, err)
		}
		_ = conn.Close()
		last = errTimeout
	}
	if stalled && !retried {
		time.Sleep(time.Second)
		return openOnce(true)
	}
	if denied {
		return nil, "", errors.New("Quit KANALI, then connect again. It is using the cooler.")
	}
	if last != nil {
		return nil, "", fmt.Errorf("the cooler could not be opened: %w", last)
	}
	return nil, "", errors.New("The cooler is not plugged in.")
}

type candidate struct {
	path string
	hint string
}

func findDevices() ([]candidate, error) {
	list, err := windows.SetupDiGetClassDevsEx(nil, "USB", 0, windows.DIGCF_PRESENT|windows.DIGCF_ALLCLASSES, 0, "")
	if err != nil {
		return nil, err
	}
	defer windows.SetupDiDestroyDeviceInfoList(list)

	var out []candidate
	for i := 0; ; i++ {
		info, err := windows.SetupDiEnumDeviceInfo(list, i)
		if err != nil {
			break
		}
		id, err := windows.SetupDiGetDeviceInstanceId(list, info)
		if err != nil || !strings.Contains(strings.ToUpper(id), "VID_391A&PID_2011") {
			continue
		}
		hint := id
		if n := strings.LastIndex(id, `\`); n >= 0 {
			hint = id[n+1:]
		}
		// The device interface speaks TRYX. The USB00n printer port often accepts
		// writes and never returns a reply, so try the interface first.
		out = append(out, candidate{path: interfacePath(id), hint: hint})
		if port := portName(list, info); port != "" {
			out = append(out, candidate{path: `\\.\` + port, hint: hint})
		}
	}
	return out, nil
}

func portName(list windows.DevInfo, info *windows.DevInfoData) string {
	key, err := windows.SetupDiOpenDevRegKey(list, info, windows.DICS_FLAG_GLOBAL, 0, windows.DIREG_DEV, windows.KEY_READ)
	if err != nil {
		return ""
	}
	k := registry.Key(key)
	defer k.Close()
	port, _, err := k.GetStringValue("PortName")
	if err != nil || !strings.HasPrefix(strings.ToUpper(port), "USB") {
		return ""
	}
	return port
}

func interfacePath(instanceID string) string {
	return `\\?\` + strings.ReplaceAll(instanceID, `\`, `#`) + `#` + usbPrintGUID
}

func answers(io link) bool {
	if err := io.Write(BuildPing(0), 2*time.Second); err != nil {
		if Trace != nil {
			Trace("ping-write", nil, err)
		}
		return false
	}
	deadline := time.Now().Add(time.Second)
	var buf []byte
	for time.Now().Before(deadline) {
		chunk, err := io.Read(time.Until(deadline))
		if len(chunk) > 0 {
			buf = append(buf, chunk...)
		}
		if i := bytes.Index(buf, []byte(Magic)); i >= 0 && len(buf[i:]) >= 8 {
			n := binary.LittleEndian.Uint32(buf[i+4 : i+8])
			if n <= maxMessage && len(buf[i:]) >= 8+int(n) {
				return true
			}
		}
		if bytes.Contains(buf, []byte("Hey!")) {
			return true
		}
		if err != nil {
			if Trace != nil {
				Trace("ping-read", buf, err)
			}
			return false
		}
	}
	if Trace != nil {
		Trace("ping-timeout", buf, errTimeout)
	}
	return false
}

func openPath(path string) (link, error) {
	// A shared handle splits bulk reads with KANALI, so a header and its body
	// land in different processes and every later command desynchronizes.
	h, err := createFile(path, 0)
	if err != nil {
		return nil, err
	}
	c := &usbConn{
		h:    h,
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	go c.loop()
	return c, nil
}

func createFile(path string, share uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, share, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
}

func busyErr(err error) bool {
	return errnoIs(err, windows.ERROR_ACCESS_DENIED) || sharing(err)
}

func sharing(err error) bool {
	return errnoIs(err, windows.ERROR_SHARING_VIOLATION)
}

func errnoIs(err error, code windows.Errno) bool {
	return errors.Is(err, code)
}

type usbConn struct {
	h       windows.Handle
	mu      sync.Mutex
	parts   [][]byte
	readErr error
	wake    chan struct{}
	done    chan struct{}
	stop    sync.Once
	stopped bool
}

// loop owns the only bulk read. Cancelling that read on a short timeout wedges
// usbprint, so callers wait on the buffer instead of cancelling the transfer.
func (c *usbConn) loop() {
	defer close(c.done)
	for {
		c.mu.Lock()
		stopped := c.stopped
		h := c.h
		c.mu.Unlock()
		if stopped || h == 0 {
			return
		}
		buf := make([]byte, 64*1024)
		n, err := readHandle(h, buf)
		if n > 0 && Trace != nil {
			Trace("chunk", buf[:n], err)
		}
		c.mu.Lock()
		if n > 0 {
			c.parts = append(c.parts, append([]byte(nil), buf[:n]...))
			c.signal()
		}
		stopped = c.stopped
		if err != nil && (stopped || !errors.Is(err, windows.ERROR_OPERATION_ABORTED)) {
			c.readErr = err
			c.signal()
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
		if stopped {
			return
		}
	}
}

func (c *usbConn) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *usbConn) halt() {
	c.stop.Do(func() {
		c.mu.Lock()
		c.stopped = true
		h := c.h
		c.mu.Unlock()
		if h != 0 {
			_ = windows.CancelIoEx(h, nil)
		}
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-c.done:
				return
			case <-ticker.C:
				if h != 0 {
					_ = windows.CancelIoEx(h, nil)
				}
			}
		}
	})
}

func (c *usbConn) Reset() error {
	c.halt()
	c.mu.Lock()
	h := c.h
	c.mu.Unlock()
	if h == 0 {
		return errors.New("cooler is not connected")
	}
	return softReset(h)
}

func softReset(h windows.Handle) error {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)
	var ov windows.Overlapped
	ov.HEvent = event
	var done uint32
	err = windows.DeviceIoControl(h, ioctlUSBPrintSoftReset, nil, 0, nil, 0, &done, &ov)
	if err == windows.ERROR_IO_PENDING {
		wait, werr := windows.WaitForSingleObject(event, 2000)
		if werr != nil || wait != windows.WAIT_OBJECT_0 {
			_ = windows.CancelIoEx(h, &ov)
			_, _ = windows.WaitForSingleObject(event, 1000)
			if werr != nil {
				return werr
			}
			return errTimeout
		}
		err = windows.GetOverlappedResult(h, &ov, &done, false)
	}
	return err
}

func (c *usbConn) Close() error {
	c.halt()
	c.mu.Lock()
	h := c.h
	c.h = 0
	c.mu.Unlock()
	if h == 0 {
		return nil
	}
	return windows.CloseHandle(h)
}

func (c *usbConn) Write(p []byte, timeout time.Duration) error {
	for len(p) > 0 {
		n, err := c.io(p, timeout, true)
		if err != nil {
			return err
		}
		if n <= 0 {
			return errors.New("the cooler accepted none of the message")
		}
		p = p[n:]
	}
	return nil
}

func (c *usbConn) Read(timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for {
		c.mu.Lock()
		if len(c.parts) > 0 {
			out := c.parts[0]
			c.parts = c.parts[1:]
			c.mu.Unlock()
			return out, nil
		}
		err := c.readErr
		c.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if !time.Now().Before(deadline) {
			return nil, errTimeout
		}
		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-c.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func readHandle(h windows.Handle, buf []byte) (int, error) {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(event)
	var ov windows.Overlapped
	ov.HEvent = event
	var done uint32
	err = windows.ReadFile(h, buf, &done, &ov)
	if err == windows.ERROR_IO_PENDING {
		_, werr := windows.WaitForSingleObject(event, windows.INFINITE)
		if werr != nil {
			_ = windows.CancelIoEx(h, &ov)
			_, _ = windows.WaitForSingleObject(event, 1000)
			return 0, werr
		}
		err = windows.GetOverlappedResult(h, &ov, &done, false)
	}
	if err != nil {
		return int(done), err
	}
	return int(done), nil
}

func (c *usbConn) io(buf []byte, timeout time.Duration, write bool) (int, error) {
	c.mu.Lock()
	h := c.h
	c.mu.Unlock()
	if h == 0 {
		return 0, errors.New("cooler is not connected")
	}
	if !write {
		return 0, errors.New("reads go through the background transfer")
	}
	ms := uint32(timeout.Milliseconds())
	if ms == 0 {
		ms = 1
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(event)
	var ov windows.Overlapped
	ov.HEvent = event
	var done uint32
	err = windows.WriteFile(h, buf, &done, &ov)
	if err == windows.ERROR_IO_PENDING {
		wait, werr := windows.WaitForSingleObject(event, ms)
		if werr != nil || wait != windows.WAIT_OBJECT_0 {
			_ = windows.CancelIoEx(h, &ov)
			_, _ = windows.WaitForSingleObject(event, 1000)
			if werr != nil {
				return 0, werr
			}
			return 0, errTimeout
		}
		err = windows.GetOverlappedResult(h, &ov, &done, false)
	}
	if err != nil {
		return 0, err
	}
	return int(done), nil
}
