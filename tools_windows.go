package main

import (
	"embed"
	"fmt"
	"sync"

	"turris-screen/internal/tools"
)

// The fetch script packs these before a Windows build. They are gzipped so the
// executable stays smaller than the raw programs.
//
//go:embed bundle/ffmpeg.exe.gz
//go:embed bundle/ffprobe.exe.gz
//go:embed bundle/yt-dlp.exe.gz
//go:embed bundle/qjs.exe.gz
//go:embed third_party/NOTICE.txt
var bundledFiles embed.FS

// bundled is one packed program. The first caller unpacks it; later callers
// wait for that result instead of writing the file again.
type bundled struct {
	once sync.Once
	name string
	path string
	err  error
}

func (b *bundled) get() (string, error) {
	b.once.Do(func() {
		b.path, b.err = unpackBundled(b.name)
	})
	return b.path, b.err
}

var (
	ffmpegBundle  = &bundled{name: "ffmpeg.exe"}
	ffprobeBundle = &bundled{name: "ffprobe.exe"}
	ytdlpBundle   = &bundled{name: "yt-dlp.exe"}
	qjsBundle     = &bundled{name: "qjs.exe"}
)

func bundledFFmpeg() (string, error)  { return ffmpegBundle.get() }
func bundledFFprobe() (string, error) { return ffprobeBundle.get() }
func bundledYtdlp() (string, error)   { return ytdlpBundle.get() }
func bundledQJS() (string, error)     { return qjsBundle.get() }

func unpackBundled(name string) (string, error) {
	gz, err := bundledFiles.ReadFile("bundle/" + name + ".gz")
	if err != nil {
		return "", fmt.Errorf("could not unpack %s: %w", name, err)
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
		text, err := bundledFiles.ReadFile("third_party/NOTICE.txt")
		if err != nil {
			return
		}
		dir, err := tools.BinDir()
		if err != nil {
			return
		}
		_, _ = tools.Materialize(dir, "NOTICE.txt", text)
	})
}
