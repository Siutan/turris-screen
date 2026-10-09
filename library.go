package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MediaItem is one cropped image the cooler has shown.
type MediaItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	RemoteName string `json:"remoteName"`
	Created    string `json:"created"`
	Thumb      string `json:"thumb"`
}

type Library struct {
	mu  sync.Mutex
	dir string
}

type indexFile struct {
	Items []storedItem `json:"items"`
}

type storedItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	RemoteName string    `json:"remoteName"`
	Created    time.Time `json:"created"`
}

func OpenLibrary() (*Library, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "turris-screen", "library")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Library{dir: dir}, nil
}

func (l *Library) List() ([]MediaItem, error) {
	if l.dir == "" {
		return nil, fmt.Errorf("media library is unavailable")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	idx, err := l.readIndex()
	if err != nil {
		return nil, err
	}
	out := make([]MediaItem, 0, len(idx.Items))
	for i := len(idx.Items) - 1; i >= 0; i-- {
		item := idx.Items[i]
		out = append(out, l.publicItem(item))
	}
	return out, nil
}

// Create stores a cropped PNG, encodes it, and returns the library entry.
// The H.264 path is the file to upload. Discard removes it if upload fails.
func (l *Library) Create(name, pngBase64 string) (MediaItem, string, error) {
	data, err := decodePNGPayload(pngBase64)
	if err != nil {
		return MediaItem{}, "", err
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return MediaItem{}, "", fmt.Errorf("that file is not a PNG the cropper can save")
	}
	ffmpeg, err := findFFmpeg()
	if err != nil {
		return MediaItem{}, "", err
	}

	id, err := newID()
	if err != nil {
		return MediaItem{}, "", err
	}
	created := time.Now()
	remote := remoteName(created)

	l.mu.Lock()
	defer l.mu.Unlock()

	pngPath := filepath.Join(l.dir, id+".png")
	h264Path := filepath.Join(l.dir, remote)
	if err := os.WriteFile(pngPath, data, 0o644); err != nil {
		return MediaItem{}, "", err
	}
	if err := writeThumb(data, filepath.Join(l.dir, id+".jpg")); err != nil {
		os.Remove(pngPath)
		return MediaItem{}, "", err
	}
	if err := encodeStill(ffmpeg, pngPath, h264Path); err != nil {
		os.Remove(pngPath)
		os.Remove(filepath.Join(l.dir, id+".jpg"))
		return MediaItem{}, "", err
	}

	idx, err := l.readIndex()
	if err != nil {
		os.Remove(pngPath)
		os.Remove(h264Path)
		os.Remove(filepath.Join(l.dir, id+".jpg"))
		return MediaItem{}, "", err
	}
	stored := storedItem{ID: id, Name: cleanName(name), RemoteName: remote, Created: created}
	idx.Items = append(idx.Items, stored)
	if err := l.writeIndex(idx); err != nil {
		os.Remove(pngPath)
		os.Remove(h264Path)
		os.Remove(filepath.Join(l.dir, id+".jpg"))
		return MediaItem{}, "", err
	}
	return l.publicItem(stored), h264Path, nil
}

func (l *Library) File(id string) (MediaItem, string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	idx, err := l.readIndex()
	if err != nil {
		return MediaItem{}, "", err
	}
	for _, item := range idx.Items {
		if item.ID == id {
			return l.publicItem(item), filepath.Join(l.dir, item.RemoteName), nil
		}
	}
	return MediaItem{}, "", fmt.Errorf("that image is no longer in the library")
}

// Renew encodes a library image under a new cooler file name and returns the name it replaced.
// Re-sending under the old name overwrites the clip the panel may be playing, and setting
// an unchanged name does not make the panel reload.
func (l *Library) Renew(id string) (item MediaItem, path, previous string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	idx, err := l.readIndex()
	if err != nil {
		return MediaItem{}, "", "", err
	}
	for i := range idx.Items {
		stored := &idx.Items[i]
		if stored.ID != id {
			continue
		}
		pngPath := filepath.Join(l.dir, stored.ID+".png")
		if _, err := os.Stat(pngPath); err != nil {
			return MediaItem{}, "", "", fmt.Errorf("the saved image file is missing")
		}
		ffmpeg, err := findFFmpeg()
		if err != nil {
			return MediaItem{}, "", "", err
		}
		previous = stored.RemoteName
		next := remoteName(time.Now())
		path = filepath.Join(l.dir, next)
		if err := encodeStill(ffmpeg, pngPath, path); err != nil {
			return MediaItem{}, "", "", err
		}
		os.Remove(filepath.Join(l.dir, previous))
		stored.RemoteName = next
		if err := l.writeIndex(idx); err != nil {
			return MediaItem{}, "", "", err
		}
		return l.publicItem(*stored), path, previous, nil
	}
	return MediaItem{}, "", "", fmt.Errorf("that image is no longer in the library")
}

// remoteName is the basename the cooler stores.
func remoteName(t time.Time) string {
	return strings.ReplaceAll(t.Format("2006-01-02_15-04-05.000"), ".", "-") + ".mp4.h264_1280x720"
}

func (l *Library) Discard(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	idx, err := l.readIndex()
	if err != nil {
		return err
	}
	next := idx.Items[:0]
	var removed *storedItem
	for _, item := range idx.Items {
		if item.ID == id {
			copyItem := item
			removed = &copyItem
			continue
		}
		next = append(next, item)
	}
	if removed == nil {
		return fmt.Errorf("that image is no longer in the library")
	}
	idx.Items = next
	if err := l.writeIndex(idx); err != nil {
		return err
	}
	os.Remove(filepath.Join(l.dir, removed.ID+".png"))
	os.Remove(filepath.Join(l.dir, removed.ID+".jpg"))
	os.Remove(filepath.Join(l.dir, removed.RemoteName))
	return nil
}

func (l *Library) publicItem(item storedItem) MediaItem {
	thumb := ""
	if data, err := os.ReadFile(filepath.Join(l.dir, item.ID+".jpg")); err == nil {
		thumb = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data)
	}
	return MediaItem{
		ID:         item.ID,
		Name:       item.Name,
		RemoteName: item.RemoteName,
		Created:    item.Created.Format("2 Jan 2006, 15:04"),
		Thumb:      thumb,
	}
}

func (l *Library) readIndex() (indexFile, error) {
	path := filepath.Join(l.dir, "index.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return indexFile{}, nil
	}
	if err != nil {
		return indexFile{}, err
	}
	var idx indexFile
	if err := json.Unmarshal(data, &idx); err != nil {
		return indexFile{}, err
	}
	return idx, nil
}

func (l *Library) writeIndex(idx indexFile) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(l.dir, "index.json"), data, 0o644)
}

func decodePNGPayload(payload string) ([]byte, error) {
	payload = strings.TrimSpace(payload)
	if i := strings.Index(payload, ","); i >= 0 && strings.Contains(strings.ToLower(payload[:i]), "base64") {
		payload = payload[i+1:]
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("could not read the cropped image")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("the cropped image was empty")
	}
	return data, nil
}

func writeThumb(png []byte, path string) error {
	img, _, err := image.Decode(bytes.NewReader(png))
	if err != nil {
		return err
	}
	b := img.Bounds()
	tw := 320
	th := 180
	if b.Dx() > 0 && b.Dy() > 0 {
		th = tw * b.Dy() / b.Dx()
		if th < 1 {
			th = 1
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		sy := b.Min.Y
		if b.Dy() > 0 {
			sy = b.Min.Y + y*b.Dy()/th
		}
		for x := 0; x < tw; x++ {
			sx := b.Min.X
			if b.Dx() > 0 {
				sx = b.Min.X + x*b.Dx()/tw
			}
			dst.Set(x, y, img.At(sx, sy))
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, dst, &jpeg.Options{Quality: 82})
}

func newID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
