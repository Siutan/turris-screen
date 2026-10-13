package tools

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestGunzipRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write([]byte("MZ-tool")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := Gunzip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "MZ-tool" {
		t.Fatalf("gunzip = %q", got)
	}
}

func TestGunzipRejectsTrash(t *testing.T) {
	if _, err := Gunzip([]byte("not gzip")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestMaterializeWritesAndSkips(t *testing.T) {
	dir := t.TempDir()
	path, err := Materialize(dir, "ffmpeg.exe", []byte("MZ-v1"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Materialize(dir, "ffmpeg.exe", []byte("MZ-v1"))
	if err != nil {
		t.Fatal(err)
	}
	st2, err := os.Stat(again)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(st2.ModTime()) || st.Size() != st2.Size() {
		t.Fatal("the same payload was written twice")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file was left behind")
	}
}

func TestMaterializeLeavesUpdatedFile(t *testing.T) {
	dir := t.TempDir()
	path, err := Materialize(dir, "yt-dlp.exe", []byte("shipped"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("updated-by-yt-dlp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Materialize(dir, "yt-dlp.exe", []byte("shipped")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "updated-by-yt-dlp" {
		t.Fatalf("update was replaced with %q", got)
	}
}

func TestMaterializeReplacesNewBundle(t *testing.T) {
	dir := t.TempDir()
	path, err := Materialize(dir, "ffmpeg.exe", []byte("v1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("local-edit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Materialize(dir, "ffmpeg.exe", []byte("v2")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2" {
		t.Fatalf("new bundle = %q", got)
	}
	stamp, err := os.ReadFile(filepath.Join(dir, "ffmpeg.exe.stamp"))
	if err != nil || len(stamp) != 64 {
		t.Fatalf("stamp = %q, %v", stamp, err)
	}
}

func TestMaterializeEmpty(t *testing.T) {
	dir := t.TempDir()
	path, err := Materialize(dir, "qjs.exe", nil)
	if err != nil || path != "" {
		t.Fatalf("empty payload = %q, %v", path, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty payload created %d files", len(entries))
	}
}
