package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"turris-screen/turris"
)

func findFFmpeg() (string, error) {
	candidates := []string{
		`C:\Program Files\KANALI\resources\Connect\ffmpeg\ffmpeg.exe`,
	}
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		candidates = append(candidates, path)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("ffmpeg was not found. Install ffmpeg and add it to PATH")
}

// encodeStill writes the media blob the cooler plays: a Tryx header plus 60 fps H.264.
func encodeStill(ffmpegPath, pngPath, outPath string) error {
	h264, err := encodeAnnexB(ffmpegPath, pngPath)
	if err != nil {
		return err
	}
	blob := turris.WrapMedia(h264, 60, turris.CountFrames(h264))
	return os.WriteFile(outPath, blob, 0o644)
}

func encodeAnnexB(ffmpegPath, pngPath string) ([]byte, error) {
	// Two seconds at 60 fps gives the panel a clip to loop. Stills use the lower bitrate from the spec.
	cmd := exec.Command(ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-loop", "1", "-t", "2", "-i", pngPath,
		"-an",
		"-vf", "scale=1280:720,fps=60",
		"-pix_fmt", "yuv420p",
		"-c:v", "libx264", "-profile:v", "main", "-level", "4.1",
		"-preset", "medium", "-b:v", "500k",
		"-x264-params", "aud=1:repeat-headers=1:annexb=1:keyint=60:min-keyint=6:scenecut=40:bframes=0:weightp=0:8x8dct=0:cabac=1:ref=3",
		"-f", "h264", "pipe:1",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, fmt.Errorf("could not prepare the image: %w", err)
		}
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		return nil, fmt.Errorf("could not prepare the image: %s", msg)
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("could not prepare the image: the encoder produced nothing")
	}
	return stdout.Bytes(), nil
}
