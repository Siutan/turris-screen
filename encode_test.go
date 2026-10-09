package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncodeStill(t *testing.T) {
	ffmpeg, err := findFFmpeg()
	if err != nil {
		t.Fatal(err)
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
	if st.Size() < 200000 {
		t.Fatalf("encoded file is only %d bytes", st.Size())
	}
}
