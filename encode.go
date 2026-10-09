package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"turris-screen/turris"
)

const (
	// clipFPS is what the cooler plays. KANALI re-encodes everything to 60 fps.
	clipFPS = 60
	// maxClipSeconds and maxClipBytes bound what the editor exports. The cooler ships
	// 15 MB presets and has stored and played 26 MB clips from KANALI.
	maxClipSeconds = 60
	maxClipBytes   = 24 << 20
	// motionKbps is KANALI's video bitrate. Stills and slideshows need far less.
	motionKbps = 5520
	stillKbps  = 1500
	// x264 overshoots its target a little; aim under the cap so one pass usually fits.
	budgetHeadroom = 0.85
)

const x264Params = "aud=1:repeat-headers=1:annexb=1:keyint=60:min-keyint=6:scenecut=40:bframes=0:weightp=0:8x8dct=0:cabac=1:ref=3"

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
	blob := turris.WrapMedia(h264, clipFPS, turris.CountFrames(h264))
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
		"-x264-params", x264Params,
		"-f", "h264", "pipe:1",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, ffmpegError("could not prepare the image", err, stderr.String())
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("could not prepare the image: the encoder produced nothing")
	}
	return stdout.Bytes(), nil
}

// clipKbps is the x264 target that keeps a clip of this length under maxClipBytes.
func clipKbps(seconds float64, motion bool) int {
	rate := stillKbps
	if motion {
		rate = motionKbps
	}
	if seconds <= 0 {
		return rate
	}
	budget := int(float64(maxClipBytes) * 8 * budgetHeadroom / seconds / 1000)
	if budget < rate {
		rate = budget
	}
	return rate
}

// encodeClip turns a raw MJPEG frame stream into the blob the cooler loops.
// The result is re-encoded at a lower bitrate until it fits maxClipBytes.
func encodeClip(ffmpegPath, framesPath string, srcFPS, frames int, motion bool, outPath string, onProgress func(float64)) (int64, error) {
	if srcFPS <= 0 || frames <= 0 {
		return 0, fmt.Errorf("there are no frames to encode")
	}
	seconds := float64(frames) / float64(srcFPS)
	if seconds > maxClipSeconds+0.5 {
		return 0, fmt.Errorf("clips are limited to %d seconds", maxClipSeconds)
	}
	want := frames * clipFPS / srcFPS
	rate := clipKbps(seconds, motion)
	for attempt := 0; attempt < 3; attempt++ {
		h264, err := encodeFrames(ffmpegPath, framesPath, srcFPS, rate, want, onProgress)
		if err != nil {
			return 0, err
		}
		blob := turris.WrapMedia(h264, clipFPS, turris.CountFrames(h264))
		if len(blob) <= maxClipBytes {
			if err := os.WriteFile(outPath, blob, 0o644); err != nil {
				return 0, err
			}
			return int64(len(blob)), nil
		}
		rate = int(float64(rate) * float64(maxClipBytes) / float64(len(blob)) * 0.9)
	}
	return 0, fmt.Errorf("the clip is still over %d MB after re-encoding. Shorten it and try again", maxClipBytes>>20)
}

func encodeFrames(ffmpegPath, framesPath string, srcFPS, kbps, want int, onProgress func(float64)) ([]byte, error) {
	out, err := os.CreateTemp("", "turris-clip-*.h264")
	if err != nil {
		return nil, err
	}
	outPath := out.Name()
	out.Close()
	defer os.Remove(outPath)

	cmd := exec.Command(ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-f", "mjpeg", "-framerate", strconv.Itoa(srcFPS), "-i", framesPath,
		"-an",
		"-vf", fmt.Sprintf("scale=1280:720,fps=%d,format=yuv420p", clipFPS),
		"-c:v", "libx264", "-profile:v", "main", "-level", "4.1",
		"-preset", "medium",
		"-b:v", fmt.Sprintf("%dk", kbps),
		"-maxrate", fmt.Sprintf("%dk", kbps*5/4),
		"-bufsize", fmt.Sprintf("%dk", kbps*2),
		"-x264-params", x264Params,
		"-progress", "pipe:1", "-nostats",
		"-f", "h264", outPath,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	readProgress(stdout, want, onProgress)
	if err := cmd.Wait(); err != nil {
		return nil, ffmpegError("could not encode the clip", err, stderr.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("could not encode the clip: the encoder produced nothing")
	}
	return data, nil
}

// transcodeVideo prepares an imported video for the editor: H.264 MP4, no audio,
// at most 1920×1080, and a keyframe every 15 frames so seeking stays cheap.
func transcodeVideo(ffmpegPath, src, dst string) error {
	cmd := exec.Command(ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-i", src,
		"-map", "0:v:0", "-an", "-sn",
		"-t", "600",
		"-vf", "scale='min(1920,iw)':'min(1080,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2,format=yuv420p",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-g", "15", "-bf", "0",
		"-movflags", "+faststart",
		dst,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ffmpegError("could not import the video", err, stderr.String())
	}
	return nil
}

func readProgress(r io.Reader, want int, onProgress func(float64)) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if onProgress == nil || want <= 0 || !strings.HasPrefix(line, "frame=") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "frame=")))
		if err != nil {
			continue
		}
		p := float64(n) / float64(want)
		if p > 1 {
			p = 1
		}
		onProgress(p)
	}
}

func ffmpegError(what string, err error, stderr string) error {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		return fmt.Errorf("%s: %w", what, err)
	}
	if len(msg) > 400 {
		msg = msg[len(msg)-400:]
	}
	return fmt.Errorf("%s: %s", what, msg)
}
