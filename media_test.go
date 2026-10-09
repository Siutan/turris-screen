package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMediaBinImportServeRemove(t *testing.T) {
	bin := &MediaBin{dir: t.TempDir()}
	src := filepath.Join(t.TempDir(), "Sunset Photo.JPEG")
	if err := os.WriteFile(src, []byte("0123456789abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	asset, err := bin.Import(src)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Kind != "image" || asset.Name != "Sunset Photo" || !strings.HasSuffix(asset.URL, ".jpg") || asset.Size != 16 {
		t.Fatalf("unexpected asset %+v", asset)
	}

	handler := bin.Middleware(http.NotFoundHandler())
	req := httptest.NewRequest(http.MethodGet, asset.URL, nil)
	req.Header.Set("Range", "bytes=4-7")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "4567" {
		t.Fatalf("range request = %d %q", rec.Code, rec.Body.String())
	}

	for _, bad := range []string{"/media/../index.json", "/media/index.json", "/media/zz.jpg"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, bad, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s served %d", bad, rec.Code)
		}
	}

	if err := bin.Remove(asset.ID); err != nil {
		t.Fatal(err)
	}
	list, err := bin.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("list after remove = %d, %v", len(list), err)
	}
}

func TestMediaBinRefusesUnknownFiles(t *testing.T) {
	bin := &MediaBin{dir: t.TempDir()}
	src := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := bin.Import(src); err == nil {
		t.Fatal("expected a text file to be refused")
	}
}

func TestMediaBinTranscodesVideo(t *testing.T) {
	ffmpeg, err := findFFmpeg()
	if err != nil {
		t.Skip(err)
	}
	src := filepath.Join(t.TempDir(), "clip.mov")
	if err := runQuiet(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=30:duration=1", "-c:v", "mpeg4", src); err != nil {
		t.Skip(err)
	}
	bin := &MediaBin{dir: t.TempDir()}
	asset, err := bin.Import(src)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Kind != "video" || !strings.HasSuffix(asset.URL, ".mp4") || asset.Size == 0 {
		t.Fatalf("unexpected asset %+v", asset)
	}
}

func runQuiet(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}
