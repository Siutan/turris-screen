// Package tools unpacks programs that ship inside the app.
package tools

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// maxToolBytes stops a damaged bundle from expanding without limit.
const maxToolBytes = 256 << 20

// BinDir is where unpacked programs live, next to the app's other data.
func BinDir() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("could not find a folder for the bundled tools")
	}
	return filepath.Join(root, "turris-screen", "bin"), nil
}

// Gunzip expands a bundled tool. The result is at most maxToolBytes.
func Gunzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, maxToolBytes+1))
	if err != nil {
		return nil, err
	}
	if len(out) > maxToolBytes {
		return nil, fmt.Errorf("tool is larger than %d MB", maxToolBytes>>20)
	}
	return out, nil
}

// Materialize writes payload to dir/name.
//
// The stamp records the payload that was installed. When it matches, the file
// on disk is left alone: yt-dlp --update may have replaced the shipped copy,
// and the next launch should keep that newer program.
func Materialize(dir, name string, payload []byte) (string, error) {
	if len(payload) == 0 {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, name)
	sum := sha256.Sum256(payload)
	want := hex.EncodeToString(sum[:])
	stampPath := dst + ".stamp"
	if got, err := os.ReadFile(stampPath); err == nil && string(got) == want {
		if st, err := os.Stat(dst); err == nil && !st.IsDir() && st.Size() > 0 {
			return dst, nil
		}
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o755); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := moveIntoPlace(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.WriteFile(stampPath, []byte(want), 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

// moveIntoPlace replaces dst. Windows will not rename over a file that is
// still open, and a scanner may briefly lock a new .exe, so try a few times.
func moveIntoPlace(tmp, dst string) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*40) * time.Millisecond)
		}
		_ = os.Remove(dst)
		if err = os.Rename(tmp, dst); err == nil {
			return nil
		}
	}
	return err
}
