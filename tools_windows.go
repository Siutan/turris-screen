package main

import (
	"fmt"
	"sync"

	"turris-screen/internal/tools"
)

// The fetch script packs these before a Windows build. They are gzipped so the
// executable stays smaller than the raw programs.

//go:embed bundle/ffmpeg.exe.gz
var ffmpegGZ []byte

//go:embed bundle/ffprobe.exe.gz
var ffprobeGZ []byte

//go:embed bundle/yt-dlp.exe.gz
var ytdlpGZ []byte

//go:embed bundle/qjs.exe.gz
var qjsGZ []byte

//go:embed third_party/NOTICE.txt
var noticeTXT []byte

// bundled is one packed program. The first caller unpacks it; later callers
// wait for that result instead of writing the file again.
type bundled struct {
	once sync.Once
	name string
	gz   []byte
	path string
	err  error
}

func (b *bundled) get() (string, error) {
	b.once.Do(func() {
		b.path, b.err = unpackBundled(b.name, b.gz)
	})
	return b.path, b.err
}

var (
	ffmpegBundle  = &bundled{name: "ffmpeg.exe", gz: ffmpegGZ}
	ffprobeBundle = &bundled{name: "ffprobe.exe", gz: ffprobeGZ}
	ytdlpBundle   = &bundled{name: "yt-dlp.exe", gz: ytdlpGZ}
	qjsBundle     = &bundled{name: "qjs.exe", gz: qjsGZ}
)

func bundledFFmpeg() (string, error)  { return ffmpegBundle.get() }
func bundledFFprobe() (string, error) { return ffprobeBundle.get() }
func bundledYtdlp() (string, error)   { return ytdlpBundle.get() }
func bundledQJS() (string, error)     { return qjsBundle.get() }

func unpackBundled(name string, gz []byte) (string, error) {
	if len(gz) == 0 {
		return "", nil
	}
	payload, err := tools.Gunzip(gz)
	if err != nil {
		return "", fmt.Errorf("could not unpack %s: %w", name, err)
	}
	if len(payload) < 2 || payload[0] != 'M' || payload[1] != 'Z' {
		return "", fmt.Errorf("bundled %s is not a Windows program", name)
	}
	dir, err := tools.BinDir()
	if err != nil {
		return "", err
	}
	path, err := tools.Materialize(dir, name, payload)
	if err != nil {
		return "", fmt.Errorf("could not unpack %s: %w", name, err)
	}
	installNotice()
	return path, nil
}

var noticeOnce sync.Once

func installNotice() {
	noticeOnce.Do(func() {
		dir, err := tools.BinDir()
		if err != nil {
			return
		}
		_, _ = tools.Materialize(dir, "NOTICE.txt", noticeTXT)
	})
}
