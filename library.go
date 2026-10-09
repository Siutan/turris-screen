package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MediaItem is one project in the library. RemoteName is empty until it has been exported.
type MediaItem struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	RemoteName string  `json:"remoteName"`
	Created    string  `json:"created"`
	Thumb      string  `json:"thumb"`
	Kind       string  `json:"kind"`
	Draft      bool    `json:"draft"`
	Editable   bool    `json:"editable"`
	Duration   float64 `json:"duration"`
	Size       int64   `json:"size"`
}

// ProjectSave is what the editor hands over when it saves.
// Data is the editor's project JSON. Thumb is a JPEG data URL.
type ProjectSave struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	Duration float64 `json:"duration"`
	Data     string  `json:"data"`
	Thumb    string  `json:"thumb"`
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
	Kind       string    `json:"kind,omitempty"`
	Draft      bool      `json:"draft,omitempty"`
	Duration   float64   `json:"duration,omitempty"`
	Size       int64     `json:"size,omitempty"`
}

var projectKinds = map[string]bool{"image": true, "video": true, "gif": true, "widget": true}

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

// Renew moves a library clip to a new cooler file name and returns the name it replaced.
// Re-sending under the old name overwrites the clip the panel may be playing, and setting
// an unchanged name does not make the panel reload. Older items without a stored clip
// are encoded again from their PNG.
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
		previous = stored.RemoteName
		next := remoteName(time.Now())
		path = filepath.Join(l.dir, next)
		clip := ""
		if previous != "" {
			clip = filepath.Join(l.dir, previous)
		}
		if _, statErr := os.Stat(clip); clip != "" && statErr == nil {
			if err := os.Rename(clip, path); err != nil {
				return MediaItem{}, "", "", err
			}
		} else {
			pngPath := filepath.Join(l.dir, stored.ID+".png")
			if _, err := os.Stat(pngPath); err != nil {
				return MediaItem{}, "", "", fmt.Errorf("this project has not been saved to the library yet. Open it and choose Save to Library")
			}
			ffmpeg, err := findFFmpeg()
			if err != nil {
				return MediaItem{}, "", "", err
			}
			if err := encodeStill(ffmpeg, pngPath, path); err != nil {
				return MediaItem{}, "", "", err
			}
		}
		stored.RemoteName = next
		if err := l.writeIndex(idx); err != nil {
			return MediaItem{}, "", "", err
		}
		return l.publicItem(*stored), path, previous, nil
	}
	return MediaItem{}, "", "", fmt.Errorf("that project is no longer in the library")
}

// SaveProject stores an editor project and its thumbnail. With clipPath set, that encoded
// clip becomes the project's cooler file under a new name, and previous is the name it replaced.
// Without it the project is a draft and keeps whatever clip it had.
func (l *Library) SaveProject(p ProjectSave, clipPath string, clipSize int64) (item MediaItem, path, previous string, err error) {
	if l.dir == "" {
		return MediaItem{}, "", "", fmt.Errorf("media library is unavailable")
	}
	if strings.TrimSpace(p.Data) == "" || !json.Valid([]byte(p.Data)) {
		return MediaItem{}, "", "", fmt.Errorf("the project could not be read")
	}
	thumb, err := decodeImagePayload(p.Thumb)
	if err != nil {
		return MediaItem{}, "", "", err
	}
	kind := p.Kind
	if !projectKinds[kind] {
		kind = "image"
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	idx, err := l.readIndex()
	if err != nil {
		return MediaItem{}, "", "", err
	}
	stored := storedItem{}
	found := -1
	if p.ID != "" {
		for i, it := range idx.Items {
			if it.ID == p.ID {
				stored, found = it, i
				break
			}
		}
	}
	if found < 0 {
		id, err := newID()
		if err != nil {
			return MediaItem{}, "", "", err
		}
		stored.ID = id
	} else {
		idx.Items = append(idx.Items[:found], idx.Items[found+1:]...)
	}
	stored.Name = cleanName(p.Name)
	stored.Kind = kind
	stored.Duration = p.Duration
	stored.Created = time.Now()

	if err := os.WriteFile(filepath.Join(l.dir, stored.ID+".project.json"), []byte(p.Data), 0o644); err != nil {
		return MediaItem{}, "", "", err
	}
	if err := os.WriteFile(filepath.Join(l.dir, stored.ID+".jpg"), thumb, 0o644); err != nil {
		return MediaItem{}, "", "", err
	}
	if clipPath == "" {
		stored.Draft = true
	} else {
		previous = stored.RemoteName
		next := remoteName(stored.Created)
		path = filepath.Join(l.dir, next)
		if err := moveFile(clipPath, path); err != nil {
			return MediaItem{}, "", "", err
		}
		if previous != "" {
			os.Remove(filepath.Join(l.dir, previous))
		}
		stored.RemoteName = next
		stored.Size = clipSize
		stored.Draft = false
	}
	idx.Items = append(idx.Items, stored)
	if err := l.writeIndex(idx); err != nil {
		return MediaItem{}, "", "", err
	}
	return l.publicItem(stored), path, previous, nil
}

// Project returns the editor JSON saved for a library item.
func (l *Library) Project(id string) (string, error) {
	if !assetID(id) {
		return "", fmt.Errorf("that project is no longer in the library")
	}
	data, err := os.ReadFile(filepath.Join(l.dir, id+".project.json"))
	if err != nil {
		return "", fmt.Errorf("this item was made before the editor and cannot be opened")
	}
	return string(data), nil
}

func assetID(id string) bool {
	if len(id) != 16 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
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
	os.Remove(filepath.Join(l.dir, removed.ID+".project.json"))
	if removed.RemoteName != "" {
		os.Remove(filepath.Join(l.dir, removed.RemoteName))
	}
	return nil
}

func (l *Library) publicItem(item storedItem) MediaItem {
	thumb := ""
	if data, err := os.ReadFile(filepath.Join(l.dir, item.ID+".jpg")); err == nil {
		thumb = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data)
	}
	kind := item.Kind
	if kind == "" {
		kind = "image"
	}
	_, err := os.Stat(filepath.Join(l.dir, item.ID+".project.json"))
	return MediaItem{
		ID:         item.ID,
		Name:       item.Name,
		RemoteName: item.RemoteName,
		Created:    item.Created.Format("2 Jan 2006, 15:04"),
		Thumb:      thumb,
		Kind:       kind,
		Draft:      item.Draft,
		Editable:   err == nil,
		Duration:   item.Duration,
		Size:       item.Size,
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

// decodeImagePayload reads a base64 or data-URL image the editor sent.
func decodeImagePayload(payload string) ([]byte, error) {
	payload = strings.TrimSpace(payload)
	if i := strings.Index(payload, ","); i >= 0 && strings.Contains(strings.ToLower(payload[:i]), "base64") {
		payload = payload[i+1:]
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("could not read the project thumbnail")
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("could not read the project thumbnail")
	}
	return data, nil
}

func newID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
