package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Asset is one imported file the editor can place on the timeline.
type Asset struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	URL     string `json:"url"`
	Size    int64  `json:"size"`
	Created string `json:"created"`
}

// ImportResult lists what was added and the files that could not be.
type ImportResult struct {
	Assets []Asset  `json:"assets"`
	Errors []string `json:"errors"`
}

// MediaBin keeps imported pictures, GIFs, and videos for every project.
type MediaBin struct {
	mu  sync.Mutex
	dir string
}

type binIndex struct {
	Assets []storedAsset `json:"assets"`
}

type storedAsset struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Kind    string    `json:"kind"`
	File    string    `json:"file"`
	Size    int64     `json:"size"`
	Created time.Time `json:"created"`
}

var mediaKinds = map[string]string{
	".png":  "image",
	".jpg":  "image",
	".jpeg": "image",
	".webp": "image",
	".bmp":  "image",
	".gif":  "gif",
	".mp4":  "video",
	".mov":  "video",
	".m4v":  "video",
	".webm": "video",
	".mkv":  "video",
	".avi":  "video",
	".wmv":  "video",
}

var assetFile = regexp.MustCompile(`^[0-9a-f]{16}\.[a-z0-9]{3,4}$`)

// MediaPatterns is the open-dialog filter for files the bin accepts.
func MediaPatterns() string {
	var out []string
	for ext := range mediaKinds {
		out = append(out, "*"+ext)
	}
	return strings.Join(out, ";")
}

func mediaKind(name string) string {
	return mediaKinds[strings.ToLower(filepath.Ext(name))]
}

func OpenMediaBin() (*MediaBin, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "turris-screen", "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &MediaBin{dir: dir}, nil
}

func (b *MediaBin) List() ([]Asset, error) {
	if b.dir == "" {
		return nil, fmt.Errorf("the media bin is unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	idx, err := b.readIndex()
	if err != nil {
		return nil, err
	}
	out := make([]Asset, 0, len(idx.Assets))
	for i := len(idx.Assets) - 1; i >= 0; i-- {
		out = append(out, publicAsset(idx.Assets[i]))
	}
	return out, nil
}

// Import copies a file into the bin. Videos are re-encoded to H.264 with short GOPs
// so the editor can seek frame by frame while it renders.
func (b *MediaBin) Import(src string) (Asset, error) {
	name := filepath.Base(src)
	return b.ImportAs(src, strings.TrimSuffix(name, filepath.Ext(name)))
}

// ImportAs is Import with the name the bin shows instead of the file's own.
func (b *MediaBin) ImportAs(src, title string) (Asset, error) {
	if b.dir == "" {
		return Asset{}, fmt.Errorf("the media bin is unavailable")
	}
	name := filepath.Base(src)
	kind := mediaKind(src)
	if kind == "" {
		return Asset{}, fmt.Errorf("%s is not a picture, GIF, or video", name)
	}
	st, err := os.Stat(src)
	if err != nil {
		return Asset{}, fmt.Errorf("could not open %s", name)
	}
	if st.IsDir() {
		return Asset{}, fmt.Errorf("%s is a folder", name)
	}
	id, err := newID()
	if err != nil {
		return Asset{}, err
	}
	ext := strings.ToLower(filepath.Ext(src))
	if ext == ".jpeg" {
		ext = ".jpg"
	}
	if kind == "video" {
		ext = ".mp4"
	}
	file := id + ext
	dst := filepath.Join(b.dir, file)
	if kind == "video" {
		ffmpeg, err := findFFmpeg()
		if err != nil {
			return Asset{}, err
		}
		if err := transcodeVideo(ffmpeg, src, dst); err != nil {
			os.Remove(dst)
			return Asset{}, fmt.Errorf("%s: %w", name, err)
		}
	} else if err := copyFile(src, dst); err != nil {
		os.Remove(dst)
		return Asset{}, err
	}
	out, err := os.Stat(dst)
	if err != nil {
		return Asset{}, err
	}
	stored := storedAsset{
		ID:      id,
		Name:    title,
		Kind:    kind,
		File:    file,
		Size:    out.Size(),
		Created: time.Now(),
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	idx, err := b.readIndex()
	if err != nil {
		os.Remove(dst)
		return Asset{}, err
	}
	idx.Assets = append(idx.Assets, stored)
	if err := b.writeIndex(idx); err != nil {
		os.Remove(dst)
		return Asset{}, err
	}
	return publicAsset(stored), nil
}

func (b *MediaBin) Remove(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	idx, err := b.readIndex()
	if err != nil {
		return err
	}
	next := idx.Assets[:0]
	var removed *storedAsset
	for _, a := range idx.Assets {
		if a.ID == id {
			copyAsset := a
			removed = &copyAsset
			continue
		}
		next = append(next, a)
	}
	if removed == nil {
		return fmt.Errorf("that file is no longer in the media bin")
	}
	idx.Assets = next
	if err := b.writeIndex(idx); err != nil {
		return err
	}
	os.Remove(filepath.Join(b.dir, removed.File))
	return nil
}

// Middleware serves bin files at /media/<file> so the webview can load and seek them.
func (b *MediaBin) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/media/") {
			next.ServeHTTP(w, r)
			return
		}
		name := path.Base(r.URL.Path)
		if b.dir == "" || !assetFile.MatchString(name) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		http.ServeFile(w, r, filepath.Join(b.dir, name))
	})
}

func publicAsset(a storedAsset) Asset {
	return Asset{
		ID:      a.ID,
		Name:    a.Name,
		Kind:    a.Kind,
		URL:     "/media/" + a.File,
		Size:    a.Size,
		Created: a.Created.Format("2 Jan 2006, 15:04"),
	}
}

func (b *MediaBin) readIndex() (binIndex, error) {
	data, err := os.ReadFile(filepath.Join(b.dir, "index.json"))
	if os.IsNotExist(err) {
		return binIndex{}, nil
	}
	if err != nil {
		return binIndex{}, err
	}
	var idx binIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return binIndex{}, err
	}
	return idx, nil
}

func (b *MediaBin) writeIndex(idx binIndex) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(b.dir, "index.json"), data, 0o644)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// moveFile renames, falling back to a copy when src and dst are on different volumes.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}
