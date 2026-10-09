package turris

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestSuccessStatusIsNotAnError(t *testing.T) {
	// Captured ping reply: status field is {#2:"NA"} and the payload is "Hey!".
	body, err := hex.DecodeString("0a00120412024e4152060a0448657921")
	if err != nil {
		t.Fatal(err)
	}
	if err := ErrorFrom(body); err != nil {
		t.Fatal(err)
	}
	real := fieldBytes(2, append(fieldVarint(1, 3), fieldBytes(2, []byte("busy"))...))
	if err := ErrorFrom(real); err == nil {
		t.Fatal("expected a real error code to fail")
	}
}

func TestUserConfigBootClips(t *testing.T) {
	power := fieldBytes(1, fieldBytes(1, []byte("default_poweron.mp4.h264_1280x720")))
	standby := fieldBytes(2, fieldBytes(2, []byte("default_standby.mp4.h264_1280x720")))
	body := fieldBytes(504, append(power, standby...))
	cfg, err := ParseUserConfig(body)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Poweron != "default_poweron.mp4.h264_1280x720" || cfg.Standby != "default_standby.mp4.h264_1280x720" {
		t.Fatalf("boot clips %+v", cfg)
	}
}

func TestPingFrame(t *testing.T) {
	got := BuildPing(0)
	payload := append([]byte{0x0a, 0x06}, []byte("hello?")...)
	body := append([]byte{0x0a, 0x00, 0x52, 0x08}, payload...)
	want := append([]byte("TRYX"), []byte{byte(len(body)), 0, 0, 0}...)
	want = append(want, body...)
	if !bytes.Equal(got, want) {
		t.Fatalf("ping frame\n got %x\nwant %x", got, want)
	}
}

func TestRunConfigCarriesClipName(t *testing.T) {
	name := "2026-10-09_13-00-00-001.mp4.h264_1280x720"
	frame := BuildRunConfig(name, "", "", 40, 1, 7)
	body := frame[8:]
	fields, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fields[OpRunConfig]; !ok {
		t.Fatal("missing run_config opcode")
	}
	if !bytes.Contains(body, []byte(name)) {
		t.Fatal("clip name missing from run_config")
	}
	if bytes.Contains(BuildRunConfig("", "", "", 40, 1, 0)[8:], []byte("single")) {
		t.Fatal("empty active name should not invent a clip field")
	}
	if !bytes.Contains(body, []byte("default_poweron.mp4.h264_1280x720")) {
		t.Fatal("run_config is missing the panel's power-on clip")
	}
	if bytes.Contains(body, []byte("default_poweron_1280x720.mp4.h264")) {
		t.Fatal("run_config used a boot clip name this panel does not have")
	}
	header, err := Parse(fields[1][0].Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !header[2][0].Varint || header[2][0].Num != 7 {
		t.Fatalf("track id = %+v", header[2])
	}
}

func TestFileListAndUserConfig(t *testing.T) {
	user := fieldBytes(1, []byte("/mnt/data/app/media/clip.mp4.h264_1280x720"))
	user = append(user, fieldBytes(2, []byte(".h264_1280x720"))...)
	user = append(user, fieldVarint(3, 42)...)
	preset := fieldBytes(1, []byte("/mnt/data/app/preset/Cosmos.mp4.h264_1280x720"))
	preset = append(preset, fieldVarint(3, 9)...)
	list := fieldBytes(1, user)
	list = append(list, fieldBytes(2, preset)...)
	body := fieldBytes(503, list)

	files, err := ParseFileList(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Preset || !files[1].Preset || files[0].Size != 42 {
		t.Fatalf("files = %+v", files)
	}
	if Basename(files[0].Path) != "clip.mp4.h264_1280x720" {
		t.Fatal(Basename(files[0].Path))
	}
	if Category(files[0].Path) != "media" || Category(files[1].Path) != "preset" {
		t.Fatalf("categories %s %s", Category(files[0].Path), Category(files[1].Path))
	}

	work := fieldBytes(3, []byte("clip.mp4.h264_1280x720"))
	display := append(fieldVarint(1, 1), fieldVarint(2, 40)...)
	cfgBody := fieldBytes(504, append(fieldBytes(3, work), fieldBytes(5, display)...))
	cfg, err := ParseUserConfig(cfgBody)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Active != "clip.mp4.h264_1280x720" || !cfg.HasBrightness || cfg.Brightness != 40 || !cfg.BacklightOn {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestErrorFrom(t *testing.T) {
	why := fieldBytes(2, []byte("busy"))
	why = append(fieldVarint(1, 3), why...)
	body := fieldBytes(2, why)
	err := ErrorFrom(body)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("busy")) {
		t.Fatal(err)
	}
}

func TestMediaBlobAndChunks(t *testing.T) {
	h264 := append([]byte{0, 0, 0, 1, 0x09, 0x10}, bytes.Repeat([]byte{0x11}, 10)...)
	h264 = append(h264, 0, 0, 0, 1, 0x09, 0x10)
	blob := WrapMedia(h264, 60, 0)
	if !IsMediaBlob(blob) {
		t.Fatal("blob was not recognized")
	}
	n := binary.LittleEndian.Uint32(blob[:4])
	if !bytes.Contains(blob[4:4+n], []byte("Tryx media header v1, fps=60, size=1280x720")) {
		t.Fatalf("header = %q", blob[4:4+n])
	}
	if CountFrames(h264) != 2 {
		t.Fatal(CountFrames(h264))
	}
	big := bytes.Repeat([]byte{1}, ChunkSize+10)
	parts := Chunks(big)
	if len(parts) != 2 || len(parts[0]) != ChunkSize || len(parts[1]) != 10 {
		t.Fatalf("chunks %d %d", len(parts[0]), len(parts[1]))
	}
	framed := BuildFileData(parts[0], 1)
	body := framed[8:]
	fields, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	inner, err := Parse(fields[OpFileData][0].Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(inner[1][0].Bytes) != ChunkSize {
		t.Fatalf("file_data payload %d", len(inner[1][0].Bytes))
	}
}
