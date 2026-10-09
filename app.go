package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails binding surface for the cooler display.
type App struct {
	ctx context.Context

	mu      sync.Mutex
	opMu    sync.Mutex
	bridge  *Bridge
	library *Library
	media   *MediaBin
	exports exports
	job     JobProgress

	ytMu     sync.Mutex
	ytCancel context.CancelFunc
	stats    hostStats
}

// BridgeStatus is what the window shows about the cooler connection.
type BridgeStatus struct {
	Connected bool   `json:"connected"`
	Serial    string `json:"serial"`
	Detail    string `json:"detail"`
}

func NewApp() *App {
	lib, err := OpenLibrary()
	if err != nil {
		lib = &Library{}
	}
	bin, err := OpenMediaBin()
	if err != nil {
		bin = &MediaBin{}
	}
	return &App{
		bridge:  NewBridge(),
		library: lib,
		media:   bin,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	loadDotEnv()
	go a.Connect()
}

func (a *App) shutdown(context.Context) {
	a.bridge.Close()
}

// Connect opens the KANALI bridge. Quit KANALI first; it holds the same pipe.
func (a *App) Connect() BridgeStatus {
	_ = a.bridge.Connect()
	return a.snapshot()
}

// GetLibrary returns previously shown images, newest first.
func (a *App) GetLibrary() ([]MediaItem, error) {
	if a.library == nil {
		return nil, fmt.Errorf("media library is unavailable")
	}
	return a.library.List()
}

// ActiveName is the file the cooler is set to play. Empty when that is unknown.
func (a *App) ActiveName() string {
	if !a.bridge.Connected() {
		return ""
	}
	name, ok := a.bridge.ActiveMedia()
	if !ok {
		return ""
	}
	return name
}

// GetProject returns the editor JSON for a library item.
func (a *App) GetProject(id string) (string, error) {
	return a.library.Project(id)
}

// SaveDraft stores the project without encoding it.
func (a *App) SaveDraft(p ProjectSave) (MediaItem, error) {
	item, _, _, err := a.library.SaveProject(p, "", 0)
	return item, err
}

// Duplicate copies a library project into a new item.
func (a *App) Duplicate(id string) (MediaItem, error) {
	if a.library == nil {
		return MediaItem{}, fmt.Errorf("media library is unavailable")
	}
	return a.library.Duplicate(id)
}

// GetAssets lists the media bin, newest first.
func (a *App) GetAssets() ([]Asset, error) {
	return a.media.List()
}

// ImportMedia asks for files and adds them to the media bin.
func (a *App) ImportMedia() (ImportResult, error) {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Add media",
		Filters: []runtime.FileFilter{
			{DisplayName: "Pictures, GIFs, and videos", Pattern: MediaPatterns()},
		},
	})
	if err != nil {
		return ImportResult{}, err
	}
	return a.ImportPaths(paths), nil
}

// ImportPaths adds dropped files to the media bin.
func (a *App) ImportPaths(paths []string) ImportResult {
	out := ImportResult{Assets: []Asset{}, Errors: []string{}}
	for i, p := range paths {
		label := "Importing " + filepath.Base(p)
		a.progress(label, 100*float64(i)/float64(len(paths)))
		asset, err := a.media.Import(p)
		if err != nil {
			out.Errors = append(out.Errors, err.Error())
			continue
		}
		out.Assets = append(out.Assets, asset)
	}
	if len(paths) > 0 {
		a.clearProgress()
	}
	return out
}

// GetYouTubeTool says whether yt-dlp is installed. Without it, ImportYouTube fetches it first.
func (a *App) GetYouTubeTool() YouTubeTool {
	return findYtdlp()
}

// ImportYouTube downloads a YouTube video (picture only) with yt-dlp and adds it to the bin.
func (a *App) ImportYouTube(req YouTubeRequest) (Asset, error) {
	link, err := parseYouTubeURL(req.URL)
	if err != nil {
		return Asset{}, err
	}
	if !a.ytMu.TryLock() {
		return Asset{}, fmt.Errorf("another YouTube video is still downloading")
	}
	defer a.ytMu.Unlock()
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	a.mu.Lock()
	a.ytCancel = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.ytCancel = nil
		a.mu.Unlock()
		cancel()
	}()

	asset, err := a.importYouTube(ctx, link, req)
	if err != nil && ctx.Err() != nil {
		return Asset{}, fmt.Errorf("cancelled")
	}
	return asset, err
}

func (a *App) importYouTube(ctx context.Context, link string, req YouTubeRequest) (Asset, error) {
	ffmpeg, err := findFFmpeg()
	if err != nil {
		return Asset{}, err
	}
	tool := findYtdlp()
	ytdlp := tool.Path
	if !tool.Installed {
		a.youtubeProgress("Getting yt-dlp", 0)
		ytdlp, err = installYtdlp(ctx, func(p float64) { a.youtubeProgress("Getting yt-dlp", p*15) })
		if err != nil {
			return Asset{}, err
		}
	} else if tool.Managed {
		a.youtubeProgress("Checking yt-dlp", 2)
		refreshYtdlp(ctx, ytdlp)
	}

	dir, err := os.MkdirTemp("", "turris-youtube-*")
	if err != nil {
		return Asset{}, err
	}
	defer os.RemoveAll(dir)

	a.youtubeProgress("Looking up the video", 16)
	info, err := probeVideo(ctx, ytdlp, link, filepath.Join(dir, "info.json"))
	if err != nil {
		return Asset{}, err
	}
	from, to, whole, err := youtubeSection(req.Start, req.End, info.Duration)
	if err != nil {
		return Asset{}, err
	}
	var file string
	for attempt := 1; ; attempt++ {
		label := "Downloading from YouTube"
		if attempt > 1 {
			label = fmt.Sprintf("Downloading from YouTube (try %d)", attempt)
		}
		a.youtubeProgress(label, 20)
		file, err = downloadVideo(ctx, ytdlp, ffmpeg, filepath.Join(dir, "info.json"), dir, from, to, whole, func(p float64) {
			a.youtubeProgress(label, 20+p*65)
		})
		if err == nil {
			break
		}
		if !errors.Is(err, errRefused) || attempt == 3 || ctx.Err() != nil {
			return Asset{}, err
		}
		select {
		case <-ctx.Done():
			return Asset{}, ctx.Err()
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	a.youtubeProgress("Preparing the video", 88)
	return a.media.ImportAs(file, cleanTitle(info.Title))
}

// youtubeProgress reports on its own event so it never mixes with a cooler transfer.
func (a *App) youtubeProgress(label string, percent float64) {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return
	}
	runtime.EventsEmit(ctx, "youtube", JobProgress{Active: true, Label: label, Percent: percent})
}

// CancelYouTube stops the YouTube download in progress, if any.
func (a *App) CancelYouTube() {
	a.mu.Lock()
	cancel := a.ytCancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// SearchKlipy lists a page of GIFs, clips, stickers, memes, or AI emoji.
// An empty query returns what KLIPY currently has trending.
func (a *App) SearchKlipy(q KlipyQuery) (KlipyPage, error) {
	c, err := newKlipyClient()
	if err != nil {
		return KlipyPage{}, err
	}
	ctx, cancel := a.klipyCtx(20 * time.Second)
	defer cancel()
	return c.search(ctx, q)
}

// ImportKlipy downloads one search result into the media bin.
func (a *App) ImportKlipy(hit KlipyHit) (Asset, error) {
	if a.media == nil {
		return Asset{}, fmt.Errorf("the media bin is unavailable")
	}
	ctx, cancel := a.klipyCtx(45 * time.Second)
	defer cancel()
	file, dir, err := downloadKlipyFile(ctx, hit.FileURL)
	if dir != "" {
		defer os.RemoveAll(dir)
	}
	if err != nil {
		return Asset{}, err
	}
	return a.media.ImportAs(file, klipyTitle(hit.Title, hit.Type))
}

// GenerateKlipyEmoji asks KLIPY to draw an emoji from a short description,
// waits until it is ready, and adds it to the media bin.
func (a *App) GenerateKlipyEmoji(prompt string) (Asset, error) {
	if a.media == nil {
		return Asset{}, fmt.Errorf("the media bin is unavailable")
	}
	c, err := newKlipyClient()
	if err != nil {
		return Asset{}, err
	}
	ctx, cancel := a.klipyCtx(90 * time.Second)
	defer cancel()
	file, dir, err := c.generateEmoji(ctx, prompt)
	if dir != "" {
		defer os.RemoveAll(dir)
	}
	if err != nil {
		return Asset{}, err
	}
	return a.media.ImportAs(file, klipyTitle(prompt, "emoji"))
}

func (a *App) klipyCtx(d time.Duration) (context.Context, context.CancelFunc) {
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, d)
}

// RemoveAsset deletes a file from the media bin. Projects that used it show a gap.
func (a *App) RemoveAsset(id string) error {
	return a.media.Remove(id)
}

// GetExportLimits is the cap the editor enforces before it renders.
func (a *App) GetExportLimits() ExportLimits {
	return ExportLimits{
		MaxSeconds: maxClipSeconds,
		MaxBytes:   maxClipBytes,
		MotionKbps: motionKbps,
		StillKbps:  stillKbps,
		Headroom:   budgetHeadroom,
		RenderFPS:  renderFPS,
	}
}

// BeginExport opens a frame stream. The editor sends frames at RenderFPS.
func (a *App) BeginExport() (string, error) {
	return a.exports.begin()
}

// AppendFrames adds base64 JPEG frames, in order, to an export.
func (a *App) AppendFrames(id string, frames []string) error {
	job, err := a.exports.get(id)
	if err != nil {
		return err
	}
	return job.append(frames)
}

// CancelExport drops an export and its frames.
func (a *App) CancelExport(id string) {
	if job, err := a.exports.take(id); err == nil {
		job.discard()
	}
}

// FinishExport encodes the frames, saves the project to the library, and shows it when asked.
func (a *App) FinishExport(req ExportRequest) (ExportResult, error) {
	job, err := a.exports.take(req.ExportID)
	if err != nil {
		return ExportResult{}, err
	}
	defer job.discard()
	framesPath, frames, err := job.close()
	if err != nil {
		return ExportResult{}, err
	}
	ffmpeg, err := findFFmpeg()
	if err != nil {
		return ExportResult{}, err
	}

	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.progress("Encoding", 0)
	clipPath := filepath.Join(job.dir, "clip.h264_1280x720")
	size, err := encodeClip(ffmpeg, framesPath, renderFPS, frames, req.Motion, clipPath, func(p float64) {
		a.progress("Encoding", 46*p)
	})
	if err != nil {
		a.clearProgress()
		return ExportResult{}, err
	}
	item, path, previous, err := a.library.SaveProject(req.Save, clipPath, size)
	if err != nil {
		a.clearProgress()
		return ExportResult{}, err
	}
	result := ExportResult{Item: item}
	if !req.Show {
		a.progress("Saved to the library", 100)
		return result, nil
	}
	if err := a.bridge.Connect(); err != nil {
		result.ShowError = err.Error()
		a.progress("Saved to the library", 100)
		return result, nil
	}
	if err := a.publishFrom(item.RemoteName, path, 50); err != nil {
		result.ShowError = err.Error()
		a.progress("Saved to the library", 100)
		return result, nil
	}
	a.removeIfInactive(previous)
	result.Shown = true
	a.progress("On the cooler", 100)
	return result, nil
}

// StillRequest is one JPEG of a live project, already rendered in the editor.
type StillRequest struct {
	ProjectID string `json:"projectId"`
	JPEG      string `json:"jpeg"`
}

// ClipRequest is a rendered live clip. The cooler restarts playback when it is shown.
type ClipRequest struct {
	ProjectID string `json:"projectId"`
	ExportID  string `json:"exportId"`
}

// RefreshStill encodes one frame and shows it in place of the project's current clip.
// It does not rewrite the project JSON or the library card.
func (a *App) RefreshStill(req StillRequest) error {
	if strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.JPEG) == "" {
		return fmt.Errorf("nothing to send")
	}
	data, err := decodeImagePayload(req.JPEG)
	if err != nil {
		return err
	}
	ffmpeg, err := findFFmpeg()
	if err != nil {
		return err
	}
	a.opMu.Lock()
	defer a.opMu.Unlock()
	dir, err := os.MkdirTemp("", "turris-still-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	imgPath := filepath.Join(dir, "frame.jpg")
	if err := os.WriteFile(imgPath, data, 0o644); err != nil {
		return err
	}
	clipPath := filepath.Join(dir, "clip")
	if err := encodeStill(ffmpeg, imgPath, clipPath); err != nil {
		return err
	}
	info, err := os.Stat(clipPath)
	if err != nil {
		return err
	}
	previous, err := a.library.ReplaceClip(req.ProjectID, clipPath, info.Size())
	if err != nil {
		return err
	}
	if err := a.bridge.Connect(); err != nil {
		return err
	}
	item, path, err := a.library.File(req.ProjectID)
	if err != nil {
		return err
	}
	if err := a.publishFrom(item.RemoteName, path, 20); err != nil {
		return err
	}
	a.removeIfInactive(previous)
	return nil
}

// RefreshClip encodes a rendered timeline and shows it in place of the current clip.
// The project JSON and the library card stay as they were. Playback starts over.
func (a *App) RefreshClip(req ClipRequest) error {
	job, err := a.exports.take(req.ExportID)
	if err != nil {
		return err
	}
	defer job.discard()
	if strings.TrimSpace(req.ProjectID) == "" {
		return fmt.Errorf("nothing to send")
	}
	framesPath, frames, err := job.close()
	if err != nil {
		return err
	}
	ffmpeg, err := findFFmpeg()
	if err != nil {
		return err
	}
	a.opMu.Lock()
	defer a.opMu.Unlock()
	defer a.clearProgress()
	clipPath := filepath.Join(job.dir, "clip.h264_1280x720")
	size, err := encodeClip(ffmpeg, framesPath, renderFPS, frames, true, clipPath, nil)
	if err != nil {
		return err
	}
	previous, err := a.library.ReplaceClip(req.ProjectID, clipPath, size)
	if err != nil {
		return err
	}
	if err := a.bridge.Connect(); err != nil {
		return err
	}
	item, path, err := a.library.File(req.ProjectID)
	if err != nil {
		return err
	}
	if err := a.publishFrom(item.RemoteName, path, 20); err != nil {
		return err
	}
	a.removeIfInactive(previous)
	return nil
}

// PickSourceFile asks for a text or JSON file a live source can read.
func (a *App) PickSourceFile() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("start the desktop app to choose a file")
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a text or JSON file",
		Filters: []runtime.FileFilter{
			{DisplayName: "Text and JSON (*.txt;*.json;*.log)", Pattern: "*.txt;*.json;*.log"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
}

// Apply sends a library clip to the cooler again.
func (a *App) Apply(id string) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if err := a.bridge.Connect(); err != nil {
		return err
	}
	a.progress("Preparing", 6)
	item, path, previous, err := a.library.Renew(id)
	if err != nil {
		a.clearProgress()
		return err
	}
	if err := a.publishFrom(item.RemoteName, path, 12); err != nil {
		a.clearProgress()
		return err
	}
	a.removeIfInactive(previous)
	a.progress("On the cooler", 100)
	return nil
}

// Remove drops an image from the library and from the cooler storage.
func (a *App) Remove(id string) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	item, _, err := a.library.File(id)
	if err != nil {
		return err
	}
	a.removeIfInactive(item.RemoteName)
	return a.library.Discard(id)
}

// RestartCooler reboots the display board. Use it when the background stops changing.
func (a *App) RestartCooler() (BridgeStatus, error) {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.progress("Restarting the cooler", 30)
	err := a.bridge.RestartCooler()
	a.clearProgress()
	return a.snapshot(), err
}

// removeIfInactive deletes a file from the cooler unless the panel is set to play it.
// Deleting the playing clip freezes the panel on its last frame.
func (a *App) removeIfInactive(remoteName string) {
	if remoteName == "" || !a.bridge.Connected() {
		return
	}
	active, ok := a.bridge.ActiveMedia()
	if !ok || active == remoteName {
		return
	}
	_ = a.bridge.RemoveMedia(remoteName)
}

// publishFrom uploads and shows a clip. Progress runs from start to 92 percent.
func (a *App) publishFrom(remoteName, h264Path string, start float64) error {
	a.progress("Sending to the cooler", start)
	err := a.bridge.TransmitMedia(h264Path, func(sent, total int64) {
		percent := start
		if total > 0 {
			percent = start + (92-start)*float64(sent)/float64(total)
		}
		if percent > 92 {
			percent = 92
		}
		a.progress("Sending to the cooler", percent)
	})
	if err != nil {
		return err
	}
	a.progress("Showing on the cooler", 96)
	return a.bridge.ShowMedia(remoteName)
}

// JobProgress is the title-bar transfer state.
type JobProgress struct {
	Active  bool    `json:"active"`
	Label   string  `json:"label"`
	Percent float64 `json:"percent"`
}

// Job returns the transfer in progress. The window polls this while a send is running.
func (a *App) Job() JobProgress {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.job
}

func (a *App) progress(label string, percent float64) {
	job := JobProgress{Active: true, Label: label, Percent: percent}
	a.mu.Lock()
	a.job = job
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return
	}
	runtime.EventsEmit(ctx, "progress", job)
}

func (a *App) clearProgress() {
	a.mu.Lock()
	a.job = JobProgress{}
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return
	}
	runtime.EventsEmit(ctx, "progress", JobProgress{Active: false})
}

func (a *App) snapshot() BridgeStatus {
	serial, detail, ok := a.bridge.Snapshot()
	return BridgeStatus{
		Connected: ok,
		Serial:    serial,
		Detail:    detail,
	}
}

func cleanName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Image"
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '\\' || r == '/' {
			return -1
		}
		return r
	}, name)
	if len(name) > 80 {
		name = name[:80]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "Image " + time.Now().Format("15:04")
	}
	return name
}
