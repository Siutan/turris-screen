// Command fetch-tools downloads the Windows programs the app packs into its
// executable. wails build runs it from build/bin; it can also be run from the
// repo root with `go run ./scripts/fetch-tools.go`.
package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ffmpegZipURL = "https://github.com/GyanD/codexffmpeg/releases/download/9.0.2/ffmpeg-9.0.2-essentials_build.zip"
	ffmpegZipSHA = "60f467265b1e312373dbcd92200c2618a74850f98d3d078e94296bb3fa2047ba"
	ffmpegSHA    = "3256173f3f8bffd7df12227c68adf68025edb1832273a9530688a7bb1ed8edec"
	ffprobeSHA   = "f0d36ecbbdd3bcfac3efa078c96c7271c2e68b3810595552ac3b7f17e9a65c52"

	ytdlpURL = "https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/download/2026.10.10.233141/yt-dlp.exe"
	ytdlpSHA = "e7cafc44db8aa433da8dd78303b9157f2c1dd509c4cbfcd8418081b7e1d26a7a"

	qjsURL = "https://github.com/quickjs-ng/quickjs/releases/download/v0.17.0/qjs-windows-x86_64.exe"
	qjsSHA = "2aeabf0092c3262d6b2609824418f7dd7ed1f1df939f73b2b15645230cac0d77"
)

func main() {
	root, err := projectRoot()
	if err != nil {
		fail(err)
	}
	if err := fetch(root); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func projectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "wails.json")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find the project root (wails.json)")
		}
		dir = parent
	}
}

func fetch(root string) error {
	bundle := filepath.Join(root, "bundle")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		return err
	}
	needFFmpeg := !current(bundle, "ffmpeg.exe", ffmpegSHA)
	needFFprobe := !current(bundle, "ffprobe.exe", ffprobeSHA)
	if needFFmpeg || needFFprobe {
		fmt.Println("downloading ffmpeg 9.0.2 essentials")
		z, err := download(ffmpegZipURL, ffmpegZipSHA)
		if err != nil {
			return err
		}
		ffmpeg, err := unzip(z, "/bin/ffmpeg.exe")
		if err != nil {
			return err
		}
		ffprobe, err := unzip(z, "/bin/ffprobe.exe")
		if err != nil {
			return err
		}
		if !bytes.Contains(ffmpeg, []byte("libx264")) {
			return fmt.Errorf("this ffmpeg was built without libx264")
		}
		if err := pack(bundle, "ffmpeg.exe", ffmpeg, ffmpegSHA); err != nil {
			return err
		}
		if err := pack(bundle, "ffprobe.exe", ffprobe, ffprobeSHA); err != nil {
			return err
		}
	}
	if !current(bundle, "yt-dlp.exe", ytdlpSHA) {
		fmt.Println("downloading yt-dlp nightly 2026.10.10.233141")
		b, err := download(ytdlpURL, ytdlpSHA)
		if err != nil {
			return err
		}
		if err := pack(bundle, "yt-dlp.exe", b, ytdlpSHA); err != nil {
			return err
		}
	}
	if !current(bundle, "qjs.exe", qjsSHA) {
		fmt.Println("downloading QuickJS-ng 0.17.0")
		b, err := download(qjsURL, qjsSHA)
		if err != nil {
			return err
		}
		if err := pack(bundle, "qjs.exe", b, qjsSHA); err != nil {
			return err
		}
	}
	return nil
}

func current(bundle, name, sha string) bool {
	b, err := os.ReadFile(filepath.Join(bundle, name+".gz"))
	if err != nil {
		return false
	}
	raw, err := gunzip(b)
	if err != nil || hexSum(raw) != sha {
		return false
	}
	fmt.Println(name, "already bundled")
	return true
}

func pack(bundle, name string, payload []byte, sha string) error {
	if hexSum(payload) != sha {
		return fmt.Errorf("%s checksum mismatch", name)
	}
	if len(payload) < 2 || payload[0] != 'M' || payload[1] != 'Z' {
		return fmt.Errorf("%s is not a Windows program", name)
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return err
	}
	zw.Header.Name = name
	zw.Header.ModTime = time.Unix(0, 0)
	if _, err := zw.Write(payload); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	dst := filepath.Join(bundle, name+".gz")
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	fmt.Printf("packed %s (%.1f MB)\n", name, float64(buf.Len())/(1<<20))
	return nil
}

func unzip(data []byte, suffix string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range r.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, suffix) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		out, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, fmt.Errorf("the ffmpeg archive has no %s", suffix)
}

func download(url, sha string) ([]byte, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		data, err := downloadOnce(url)
		if err != nil {
			last = err
			continue
		}
		if hexSum(data) != sha {
			last = fmt.Errorf("checksum mismatch for %s", url)
			continue
		}
		return data, nil
	}
	return nil, last
}

func downloadOnce(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "turris-screen-build")
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func gunzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func hexSum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
