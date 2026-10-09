package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ExportLimits tells the editor what the cooler can take.
type ExportLimits struct {
	MaxSeconds int     `json:"maxSeconds"`
	MaxBytes   int     `json:"maxBytes"`
	MotionKbps int     `json:"motionKbps"`
	StillKbps  int     `json:"stillKbps"`
	Headroom   float64 `json:"headroom"`
	RenderFPS  int     `json:"renderFps"`
}

// ExportRequest finishes an export. Motion is false when nothing on the timeline moves,
// which lets the encoder use a much lower bitrate.
type ExportRequest struct {
	ExportID string      `json:"exportId"`
	Motion   bool        `json:"motion"`
	Show     bool        `json:"show"`
	Save     ProjectSave `json:"save"`
}

// ExportResult is the saved item. ShowError is set when it was saved but not shown.
type ExportResult struct {
	Item      MediaItem `json:"item"`
	Shown     bool      `json:"shown"`
	ShowError string    `json:"showError"`
}

// renderFPS is the rate the editor draws frames at. The encoder doubles it to clipFPS.
const renderFPS = 30

type exportJob struct {
	mu     sync.Mutex
	dir    string
	file   *os.File
	frames int
}

type exports struct {
	mu   sync.Mutex
	jobs map[string]*exportJob
}

func (e *exports) begin() (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "turris-export-")
	if err != nil {
		return "", err
	}
	f, err := os.Create(filepath.Join(dir, "frames.mjpeg"))
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	e.mu.Lock()
	if e.jobs == nil {
		e.jobs = map[string]*exportJob{}
	}
	e.jobs[id] = &exportJob{dir: dir, file: f}
	e.mu.Unlock()
	return id, nil
}

func (e *exports) get(id string) (*exportJob, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	job := e.jobs[id]
	if job == nil {
		return nil, fmt.Errorf("that export was cancelled")
	}
	return job, nil
}

// take removes the job so nothing else appends to it.
func (e *exports) take(id string) (*exportJob, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	job := e.jobs[id]
	if job == nil {
		return nil, fmt.Errorf("that export was cancelled")
	}
	delete(e.jobs, id)
	return job, nil
}

// append writes base64 JPEG frames to the job's MJPEG stream.
func (job *exportJob) append(frames []string) error {
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.file == nil {
		return fmt.Errorf("that export is finished")
	}
	limit := maxClipSeconds*renderFPS + renderFPS
	if job.frames+len(frames) > limit {
		return fmt.Errorf("clips are limited to %d seconds", maxClipSeconds)
	}
	for _, frame := range frames {
		if i := strings.Index(frame, ","); i >= 0 && strings.Contains(frame[:i], "base64") {
			frame = frame[i+1:]
		}
		data, err := base64.StdEncoding.DecodeString(frame)
		if err != nil || !bytes.HasPrefix(data, []byte{0xFF, 0xD8}) {
			return fmt.Errorf("a rendered frame could not be read")
		}
		if _, err := job.file.Write(data); err != nil {
			return err
		}
		job.frames++
	}
	return nil
}

// close finishes writing and returns the stream path and frame count.
func (job *exportJob) close() (string, int, error) {
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.file == nil {
		return "", 0, fmt.Errorf("that export is finished")
	}
	path := job.file.Name()
	err := job.file.Close()
	job.file = nil
	return path, job.frames, err
}

func (job *exportJob) discard() {
	job.mu.Lock()
	if job.file != nil {
		job.file.Close()
		job.file = nil
	}
	job.mu.Unlock()
	os.RemoveAll(job.dir)
}
