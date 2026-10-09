package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"turris-screen/turris"
)

func TestEncodeStill(t *testing.T) {
	ffmpeg, err := findFFmpeg()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "frame.png")
	h264Path := filepath.Join(dir, remoteName(time.Now()))

	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := 0; y < 720; y++ {
		for x := 0; x < 1280; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 40, A: 255})
		}
	}
	f, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	if err := encodeStill(ffmpeg, pngPath, h264Path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(h264Path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() < 4096 {
		t.Fatalf("encoded file is only %d bytes", st.Size())
	}
	f, err = os.Open(h264Path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	head := make([]byte, 80)
	n, _ := f.Read(head)
	if !bytes.Contains(head[:n], []byte("Tryx media header")) {
		t.Fatal("encoded file is missing the Tryx media header")
	}
}

func TestClipKbpsFitsTheCap(t *testing.T) {
	for _, seconds := range []float64{1, 5, 10, 30, 45, 60} {
		for _, motion := range []bool{false, true} {
			kbps := clipKbps(seconds, motion)
			bytes := float64(kbps) * 1000 / 8 * seconds
			if bytes > maxClipBytes {
				t.Fatalf("%v s motion=%v: %d kbps is %.0f bytes, over the cap", seconds, motion, kbps, bytes)
			}
			if motion && kbps > motionKbps || !motion && kbps > stillKbps {
				t.Fatalf("%v s motion=%v: %d kbps is above the base rate", seconds, motion, kbps)
			}
		}
	}
	if got := clipKbps(5, true); got != motionKbps {
		t.Fatalf("short clips should use the full video rate, got %d", got)
	}
	if got := clipKbps(60, true); got >= motionKbps {
		t.Fatalf("a minute of video should be capped below %d kbps, got %d", motionKbps, got)
	}
}

func TestEncodeClip(t *testing.T) {
	ffmpeg, err := findFFmpeg()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.mjpeg")
	var stream bytes.Buffer
	const frames = 15
	for i := 0; i < frames; i++ {
		img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
		for y := 0; y < 720; y++ {
			for x := 0; x < 1280; x++ {
				img.Set(x, y, color.RGBA{R: uint8((x + i*20) % 256), G: uint8(y % 256), B: 90, A: 255})
			}
		}
		if err := jpeg.Encode(&stream, img, &jpeg.Options{Quality: 85}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(framesPath, stream.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "clip.h264_1280x720")
	var last float64
	size, err := encodeClip(ffmpeg, framesPath, renderFPS, frames, true, out, func(p float64) { last = p })
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) != size {
		t.Fatalf("reported %d bytes, wrote %d", size, len(data))
	}
	if !turris.IsMediaBlob(data) {
		t.Fatal("clip is missing the Tryx media header")
	}
	// Half a second at 30 fps becomes 30 frames at 60 fps.
	if n := turris.CountFrames(data); n < 28 || n > 32 {
		t.Fatalf("expected about 30 frames at 60 fps, got %d", n)
	}
	if last < 0.9 {
		t.Fatalf("progress stopped at %.2f", last)
	}
}

func TestEncodeClipRejectsLongClips(t *testing.T) {
	_, err := encodeClip("ffmpeg", "unused", renderFPS, (maxClipSeconds+2)*renderFPS, true, "unused", nil)
	if err == nil {
		t.Fatal("expected a clip over the time limit to be refused")
	}
}
