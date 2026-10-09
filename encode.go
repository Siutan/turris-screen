package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
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
	return "", fmt.Errorf("ffmpeg was not found. KANALI includes one, or install ffmpeg and add it to PATH")
}

// encodeStill writes the raw H.264 clip the cooler plays.
// KANALI's working clips are GIFs converted by MediaX at 60 fps, which come out all-intra.
// A still or an mp4 through the same DLL, or plain ffmpeg output, is stored and then left off the screen.
func encodeStill(ffmpegPath, pngPath, h264Path string) error {
	if _, err := os.Stat(mediaXDLL); err != nil {
		return fmt.Errorf("KANALI's video encoder (MediaX.dll) was not found. Install KANALI, then try again")
	}
	gifPath := h264Path + ".src.gif"
	defer os.Remove(gifPath)
	if err := stillToGIF(ffmpegPath, pngPath, gifPath); err != nil {
		return err
	}
	return encodeWithMediaX(gifPath, h264Path, 60)
}

const mediaXDLL = `C:\Program Files\KANALI\MediaX.dll`

func stillToGIF(ffmpegPath, pngPath, gifPath string) error {
	cmd := exec.Command(ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-y",
		"-loop", "1", "-framerate", "10", "-i", pngPath,
		"-t", "2",
		"-vf", "scale=1280:720:flags=lanczos,fps=10,split[s0][s1];[s0]palettegen=stats_mode=single[p];[s1][p]paletteuse",
		gifPath,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("could not prepare the image: %w", err)
		}
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		return fmt.Errorf("could not prepare the image: %s", msg)
	}
	return nil
}

func encodeWithMediaX(srcPath, h264Path string, fps uintptr) error {
	dllPath := mediaXDLL
	if _, err := os.Stat(dllPath); err != nil {
		return err
	}
	_ = windows.SetDllDirectory(filepath.Dir(dllPath))
	dll := windows.NewLazyDLL(dllPath)
	convert := dll.NewProc("MX_ConvertToH264Raw")
	statusFn := dll.NewProc("MX_SessionGetStatus")
	if err := dll.Load(); err != nil {
		return err
	}
	src := append([]byte(srcPath), 0)
	dst := append([]byte(h264Path), 0)
	r, _, _ := convert.Call(
		uintptr(unsafe.Pointer(&src[0])),
		uintptr(unsafe.Pointer(&dst[0])),
		1280,
		720,
		fps,
	)
	session := int64(r)
	if session <= 0 {
		runtime.KeepAlive(src)
		runtime.KeepAlive(dst)
		return fmt.Errorf("the cooler encoder did not start")
	}
	deadline := time.Now().Add(30 * time.Second)
	var code int32
	for time.Now().Before(deadline) {
		ret, _, _ := statusFn.Call(uintptr(session), uintptr(unsafe.Pointer(&code)))
		if int64(ret) < 0 {
			break
		}
		switch code {
		case 3:
			runtime.KeepAlive(src)
			runtime.KeepAlive(dst)
			st, err := os.Stat(h264Path)
			if err != nil || st.Size() == 0 {
				return fmt.Errorf("the cooler encoder produced an empty video")
			}
			return nil
		case 4, 5:
			runtime.KeepAlive(src)
			runtime.KeepAlive(dst)
			return fmt.Errorf("the cooler encoder failed")
		}
		time.Sleep(40 * time.Millisecond)
	}
	runtime.KeepAlive(src)
	runtime.KeepAlive(dst)
	return fmt.Errorf("the cooler encoder timed out")
}
