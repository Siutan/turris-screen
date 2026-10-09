package turris

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

type scriptLink struct {
	reads  [][]byte
	writes [][]byte
}

func (s *scriptLink) Write(p []byte, _ time.Duration) error {
	s.writes = append(s.writes, append([]byte(nil), p...))
	return nil
}

func (s *scriptLink) Read(_ time.Duration) ([]byte, error) {
	if len(s.reads) == 0 || s.reads[0] == nil {
		if len(s.reads) > 0 {
			s.reads = s.reads[1:]
		}
		return nil, errTimeout
	}
	next := s.reads[0]
	s.reads = s.reads[1:]
	return next, nil
}

func (s *scriptLink) Close() error { return nil }

func ack() []byte {
	return Frame(fieldBytes(1, nil))
}

func TestUploadChunksAndPlay(t *testing.T) {
	blob := WrapMedia(bytes.Repeat([]byte{0, 0, 0, 1, 0x09}, 1), 60, 1)
	blob = append(blob, bytes.Repeat([]byte{7}, ChunkSize)...)
	link := &scriptLink{reads: [][]byte{ack(), ack(), ack(), ack()}}
	dev := newDevice(link)
	dev.lastPing = time.Now()
	name := "clip.mp4.h264_1280x720"
	if err := dev.Upload(name, blob, nil); err != nil {
		t.Fatal(err)
	}
	if len(link.writes) != 4 {
		t.Fatalf("writes = %d", len(link.writes))
	}
	ops := []int{OpFileBegin, OpFileData, OpFileData, OpFileEnd}
	for i, op := range ops {
		fields, err := Parse(link.writes[i][8:])
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := fields[op]; !ok {
			t.Fatalf("write %d missing opcode %d", i, op)
		}
	}
	begin, _ := Parse(link.writes[0][8:])
	inner, _ := Parse(begin[OpFileBegin][0].Bytes)
	if string(inner[1][0].Bytes) != name {
		t.Fatalf("begin name %q", inner[1][0].Bytes)
	}
	data, _ := Parse(link.writes[1][8:])
	chunk, _ := Parse(data[OpFileData][0].Bytes)
	if len(chunk[1][0].Bytes) != ChunkSize {
		t.Fatalf("first chunk %d", len(chunk[1][0].Bytes))
	}

	work := fieldBytes(3, []byte(name))
	display := append(fieldVarint(1, 1), fieldVarint(2, 55)...)
	cfg := Frame(fieldBytes(504, append(fieldBytes(3, work), fieldBytes(5, display)...)))
	link.reads = [][]byte{cfg, ack(), ack(), ack()}
	link.writes = nil
	if err := dev.Play(name); err != nil {
		t.Fatal(err)
	}
	if len(link.writes) < 3 {
		t.Fatalf("play writes %d", len(link.writes))
	}
	run, _ := Parse(link.writes[1][8:])
	if !bytes.Contains(run[OpRunConfig][0].Bytes, []byte(name)) {
		t.Fatal("play did not send the clip name")
	}
	if !bytes.Contains(run[OpRunConfig][0].Bytes, []byte("default_poweron.mp4.h264_1280x720")) {
		t.Fatal("play used the wrong power-on clip")
	}
}

func TestRemoveIgnoresAQuietPanel(t *testing.T) {
	link := &scriptLink{reads: [][]byte{nil}}
	dev := newDevice(link)
	dev.alive = true
	if err := dev.Remove("clip.mp4.h264_1280x720", "media"); err != nil {
		t.Fatal(err)
	}
	if len(link.writes) != 1 {
		t.Fatalf("writes %d", len(link.writes))
	}
	fields, err := Parse(link.writes[0][8:])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fields[OpFileRemove]; !ok {
		t.Fatal("missing file_remove")
	}
}

func TestPlayCommitsWhenRunConfigIsQuiet(t *testing.T) {
	name := "clip.mp4.h264_1280x720"
	work := fieldBytes(3, []byte(name))
	display := append(fieldVarint(1, 1), fieldVarint(2, 40)...)
	cfg := Frame(fieldBytes(504, append(fieldBytes(3, work), fieldBytes(5, display)...)))
	link := &scriptLink{reads: [][]byte{cfg, nil, nil, ack()}}
	dev := newDevice(link)
	dev.alive = true
	if err := dev.Play(name); err != nil {
		t.Fatal(err)
	}
	if len(link.writes) < 3 {
		t.Fatalf("writes %d", len(link.writes))
	}
	run, err := Parse(link.writes[1][8:])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := run[OpRunConfig]; !ok {
		t.Fatal("missing run_config")
	}
	apply, err := Parse(link.writes[2][8:])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := apply[OpApply]; !ok {
		t.Fatal("missing apply")
	}
}

func TestFileDataTimeoutStillFinishes(t *testing.T) {
	blob := WrapMedia([]byte{0, 0, 0, 1, 0x09}, 60, 1)
	link := &scriptLink{reads: [][]byte{ack(), nil, ack()}}
	dev := newDevice(link)
	dev.alive = true
	dev.lastPing = time.Now()
	if err := dev.Upload("clip.mp4.h264_1280x720", blob, nil); err != nil {
		t.Fatal(err)
	}
	if len(link.writes) != 3 {
		t.Fatalf("writes = %d", len(link.writes))
	}
	if !dev.Healthy() {
		t.Fatal("a quiet chunk should not drop the session")
	}
}

func TestHandshakeContinuesWhenApplyIsQuiet(t *testing.T) {
	status := fieldBytes(2, fieldBytes(2, []byte("NA")))
	info := fieldBytes(500, fieldBytes(5, []byte("TURJJ26010002837")))
	frame := Frame(append(append(fieldBytes(1, nil), status...), info...))
	link := &scriptLink{reads: [][]byte{frame, nil, nil, nil, nil, nil}}
	dev := newDevice(link)
	if err := dev.Handshake(); err != nil {
		t.Fatal(err)
	}
	if !dev.alive {
		t.Fatal("session should stay up when apply does not reply")
	}
}

func TestRecvAcceptsBodyWithoutHeader(t *testing.T) {
	body, err := hex.DecodeString("0a021007120412024e4152060a0448657921")
	if err != nil {
		t.Fatal(err)
	}
	link := &scriptLink{reads: [][]byte{body}}
	dev := newDevice(link)
	got, err := dev.recv(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := ErrorFrom(got); err != nil {
		t.Fatal(err)
	}
}

func TestRecvDropsStaleHeaderWhenBodySizeDiffers(t *testing.T) {
	header := Frame(nil)[:8]
	binary := append([]byte(nil), header...)
	// Length 158 must not swallow a short ping body.
	binary[4], binary[5], binary[6], binary[7] = 158, 0, 0, 0
	body, err := hex.DecodeString("0a021007120412024e4152060a0448657921")
	if err != nil {
		t.Fatal(err)
	}
	link := &scriptLink{reads: [][]byte{binary, body}}
	dev := newDevice(link)
	got, err := dev.recv(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(body) {
		t.Fatalf("got %d bytes", len(got))
	}
}

func TestRecvSkipsBytesBeforeTheHeader(t *testing.T) {
	body, err := hex.DecodeString("0a00120412024e4152060a0448657921")
	if err != nil {
		t.Fatal(err)
	}
	link := &scriptLink{reads: [][]byte{append([]byte{0xff, 0x00}, Frame(body)...)}}
	dev := newDevice(link)
	got, err := dev.recv(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := ErrorFrom(got); err != nil {
		t.Fatal(err)
	}
}

func TestNoteInfoPrefersSerial(t *testing.T) {
	dev := newDevice(&scriptLink{})
	body, err := hex.DecodeString("0a00120412024e41a21f90010a054c696e7578122723313620505245454d5054204d6f6e204a616e20352031353a34323a30372043535420323032361a1f352e31302e342d7461672d2d676231343737323034353233382d646972747922065475727269732a0f76312e302e382e323032363037313542105455524a4a32363031303030323833374801521032433838414144313641353531413135")
	if err != nil {
		t.Fatal(err)
	}
	dev.noteInfo(Strings(body))
	if dev.serial != "TURJJ26010002837" {
		t.Fatalf("serial %q from %q", dev.serial, Strings(body))
	}
}

func TestHandshakeRequiresDeviceInfo(t *testing.T) {
	link := &scriptLink{}
	dev := newDevice(link)
	err := dev.Handshake()
	if !errors.Is(err, errTimeout) && (err == nil || !bytes.Contains([]byte(err.Error()), []byte("did not answer"))) {
		t.Fatal(err)
	}
}
