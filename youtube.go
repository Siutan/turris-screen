package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"turris-screen/internal/tools"
)

const (
	ytdlpRelease = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp.exe"
	// ytdlpRefresh is how stale the app's own yt-dlp may get. YouTube changes often
	// enough that old releases stop working.
	ytdlpRefresh = 7 * 24 * time.Hour
	// youtubeMaxSeconds matches what the media bin keeps of any imported video.
	youtubeMaxSeconds = 600
	ytdlpFormat       = "bv*[height<=1080][vcodec^=avc1]/bv*[height<=1080]/b[height<=1080]/b"
)

// YouTubeRequest is a link plus an optional part of the video, in seconds. End 0 means
// "as far as the bin allows".
type YouTubeRequest struct {
	URL   string  `json:"url"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// YouTubeTool says whether yt-dlp is ready or will be downloaded on first use.
type YouTubeTool struct {
	Installed bool   `json:"installed"`
	Managed   bool   `json:"managed"`
	Path      string `json:"path"`
}

type videoInfo struct {
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
	IsLive   bool    `json:"is_live"`
	LiveStat string  `json:"live_status"`
}

var youtubeHosts = map[string]bool{
	"youtube.com":          true,
	"youtu.be":             true,
	"youtube-nocookie.com": true,
}

// parseYouTubeURL accepts youtube.com, youtu.be, and their m./www./music. forms.
func parseYouTubeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("paste a YouTube link, like https://www.youtube.com/watch?v=…")
	}
	host := strings.ToLower(u.Hostname())
	for _, prefix := range []string{"www.", "m.", "music."} {
		host = strings.TrimPrefix(host, prefix)
	}
	if !youtubeHosts[host] {
		return "", fmt.Errorf("that is not a YouTube link")
	}
	return u.String(), nil
}

func managedYtdlp() string {
	dir, err := tools.BinDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "yt-dlp.exe")
}

func findYtdlp() YouTubeTool {
	if p, err := bundledYtdlp(); err == nil && p != "" {
		return YouTubeTool{Installed: true, Managed: true, Path: p}
	}
	if p := managedYtdlp(); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return YouTubeTool{Installed: true, Managed: true, Path: p}
		}
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return YouTubeTool{Installed: true, Path: p}
	}
	return YouTubeTool{}
}

// installYtdlp downloads the official Windows build next to the app's other data.
func installYtdlp(ctx context.Context, onProgress func(float64)) (string, error) {
	dst := managedYtdlp()
	if dst == "" {
		return "", fmt.Errorf("could not find a folder for yt-dlp")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ytdlpRelease, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not download yt-dlp: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("could not download yt-dlp: GitHub answered %s", resp.Status)
	}
	tmp := dst + ".download"
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	body := io.Reader(resp.Body)
	if resp.ContentLength > 0 && onProgress != nil {
		body = &countingReader{r: resp.Body, total: resp.ContentLength, onProgress: onProgress}
	}
	n, err := io.Copy(out, body)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("could not download yt-dlp: %w", err)
	}
	if n < 1<<20 || !isWindowsExe(tmp) {
		os.Remove(tmp)
		return "", fmt.Errorf("the yt-dlp download was not a Windows program")
	}
	os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dst, nil
}

func isWindowsExe(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 2)
	_, err = io.ReadFull(f, head)
	return err == nil && head[0] == 'M' && head[1] == 'Z'
}

type countingReader struct {
	r          io.Reader
	n, total   int64
	onProgress func(float64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	c.onProgress(math.Min(1, float64(c.n)/float64(c.total)))
	return n, err
}

// refreshYtdlp asks the app's own yt-dlp to update itself about once a week.
// Failures are ignored; an older yt-dlp often still works.
func refreshYtdlp(ctx context.Context, path string) {
	st, err := os.Stat(path)
	if err != nil || time.Since(st.ModTime()) < ytdlpRefresh {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	_ = ytdlpCommand(ctx, path, "--update").Run()
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}

// ytdlpCommand runs yt-dlp hidden. yt-dlp.exe starts a copy of itself, which starts
// ffmpeg, so cancelling has to end the whole tree or the pipes never close.
func ytdlpCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	hideWindow(cmd)
	cmd.Cancel = func() error {
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		hideWindow(kill)
		if err := kill.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// ytdlpBaseArgs are shared by every call. A user's yt-dlp config is ignored so it
// cannot change formats or file names under us.
func ytdlpBaseArgs() []string {
	args := []string{"--ignore-config", "--no-playlist", "--no-warnings"}
	// YouTube needs a JavaScript runtime for most formats. Deno is on by default.
	// QuickJS ships with the app, so a machine without Deno or Node still works.
	// The first colon separates the runtime name; a Windows drive letter is the second.
	if qjs, err := bundledQJS(); err == nil && qjs != "" {
		args = append(args, "--js-runtimes", "quickjs:"+qjs)
	}
	if _, err := exec.LookPath("deno"); err != nil {
		if node, err := exec.LookPath("node"); err == nil {
			args = append(args, "--js-runtimes", "node:"+node)
		}
	}
	return args
}

// probeVideo fetches the video's details and saves them so the download can reuse them
// instead of asking YouTube twice.
func probeVideo(ctx context.Context, ytdlp, link, infoPath string) (videoInfo, error) {
	args := append(ytdlpBaseArgs(), "--dump-single-json", "--", link)
	cmd := ytdlpCommand(ctx, ytdlp, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return videoInfo{}, ctx.Err()
		}
		return videoInfo{}, ytdlpError(stderr.String(), err)
	}
	var info videoInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return videoInfo{}, fmt.Errorf("yt-dlp returned something unexpected")
	}
	if info.IsLive || info.LiveStat == "is_live" || info.LiveStat == "is_upcoming" {
		return videoInfo{}, fmt.Errorf("live streams can't be imported. Try again once it has ended")
	}
	if err := os.WriteFile(infoPath, stdout.Bytes(), 0o600); err != nil {
		return videoInfo{}, err
	}
	return info, nil
}

// youtubeSection clamps the requested part to the video and to what the bin keeps.
// whole is true when the full video fits, so no cutting is needed.
func youtubeSection(start, end, duration float64) (from, to float64, whole bool, err error) {
	if start < 0 || end < 0 {
		return 0, 0, false, fmt.Errorf("times can't be negative")
	}
	if duration > 0 && start >= duration {
		return 0, 0, false, fmt.Errorf("the video is only %s long", fmtSeconds(duration))
	}
	if end == 0 {
		end = start + youtubeMaxSeconds
	}
	if end <= start {
		return 0, 0, false, fmt.Errorf("the end time has to be after the start")
	}
	if duration > 0 {
		end = math.Min(end, duration)
	}
	end = math.Min(end, start+youtubeMaxSeconds)
	whole = start == 0 && duration > 0 && end >= duration
	return start, end, whole, nil
}

var (
	pctLine  = regexp.MustCompile(`^\[dl\]\s*([\d.]+)%`)
	timeLine = regexp.MustCompile(`time=(\d+):(\d\d):(\d\d(?:\.\d+)?)`)
)

// downloadVideo runs yt-dlp from the saved details and returns the file it wrote.
// Progress comes from yt-dlp itself, or from ffmpeg's clock when only a part is cut.
func downloadVideo(ctx context.Context, ytdlp, ffmpeg, infoPath, dir string, from, to float64, whole bool, onProgress func(float64)) (string, error) {
	args := append(ytdlpBaseArgs(),
		"--load-info-json", infoPath,
		"--no-part", "--force-overwrites", "--no-mtime", "--newline", "--progress",
		"--progress-template", "download:[dl] %(progress._percent_str)s",
		"--ffmpeg-location", ffmpegLocation(ffmpeg),
		"-f", ytdlpFormat,
		"-P", dir,
		"-o", "video.%(ext)s",
		"--print", "after_move:filepath",
	)
	if !whole {
		// Cutting a part goes through ffmpeg, which yt-dlp runs silently. Ask it for
		// progress lines (out_time=…) on stderr instead.
		args = append(args,
			"--download-sections", fmt.Sprintf("*%s-%s", secs(from), secs(to)),
			"--downloader-args", "ffmpeg:-progress pipe:2 -stats_period 0.5",
		)
	}
	cmd := ytdlpCommand(ctx, ytdlp, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("could not start yt-dlp: %w", err)
	}
	span := to - from
	report := func(line string) {
		if onProgress == nil {
			return
		}
		if m := pctLine.FindStringSubmatch(line); m != nil {
			if p, err := strconv.ParseFloat(m[1], 64); err == nil {
				onProgress(math.Min(1, p/100))
			}
		} else if m := timeLine.FindStringSubmatch(line); m != nil && span > 0 {
			h, _ := strconv.Atoi(m[1])
			mi, _ := strconv.Atoi(m[2])
			s, _ := strconv.ParseFloat(m[3], 64)
			onProgress(math.Min(1, (float64(h*3600+mi*60)+s)/span))
		}
	}
	var file string
	var errText strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(stderr)
		sc.Split(scanLinesOrCR)
		for sc.Scan() {
			line := sc.Text()
			report(line)
			if strings.Contains(line, "ERROR:") || strings.Contains(line, "403 Forbidden") {
				errText.WriteString(line + "\n")
			}
		}
	}()
	sc := bufio.NewScanner(stdout)
	sc.Split(scanLinesOrCR)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		report(line)
		if filepath.IsAbs(line) {
			file = line
		}
	}
	<-done
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", ytdlpError(errText.String(), err)
	}
	if file == "" {
		return "", fmt.Errorf("yt-dlp finished without saving a video")
	}
	if rel, err := filepath.Rel(dir, file); err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("yt-dlp saved the video somewhere unexpected")
	}
	return file, nil
}

// scanLinesOrCR splits on \n or \r, since ffmpeg rewrites its progress line in place.
func scanLinesOrCR(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// errRefused is YouTube's video server answering 403. It comes and goes, so it is worth retrying.
var errRefused = errors.New("YouTube refused the download. Try again in a moment")

// ytdlpError turns yt-dlp's last ERROR line into something a person can act on.
func ytdlpError(stderr string, err error) error {
	if strings.Contains(stderr, "403 Forbidden") || strings.Contains(stderr, "HTTP Error 403") {
		return errRefused
	}
	msg := ""
	for _, line := range strings.Split(stderr, "\n") {
		if i := strings.Index(line, "ERROR:"); i >= 0 {
			msg = strings.TrimSpace(line[i+len("ERROR:"):])
		}
	}
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "confirm your age"), strings.Contains(lower, "age-restricted"):
		return errors.New("this video is age-restricted, so it can't be downloaded without signing in")
	case strings.Contains(lower, "not a bot"):
		return errors.New("YouTube wants to check you are not a bot. Try again in a little while")
	case strings.Contains(lower, "private video"):
		return errors.New("that video is private")
	case strings.Contains(lower, "video unavailable"), strings.Contains(lower, "not available"):
		return errors.New("that video is unavailable")
	case strings.Contains(lower, "unable to download webpage"), strings.Contains(lower, "getaddrinfo"):
		return errors.New("could not reach YouTube. Check your internet connection")
	}
	// yt-dlp prefixes its errors with "[youtube] <id>: ".
	if i := strings.Index(msg, "]"); strings.HasPrefix(msg, "[") && i > 0 {
		msg = strings.TrimSpace(msg[i+1:])
		if j := strings.Index(msg, ": "); j >= 0 && j < 20 {
			msg = msg[j+2:]
		}
	}
	if msg == "" {
		return fmt.Errorf("yt-dlp failed: %w", err)
	}
	return fmt.Errorf("YouTube download failed: %s", msg)
}

// cleanTitle keeps video titles to something that fits a bin tile.
func cleanTitle(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	if r := []rune(title); len(r) > 80 {
		title = strings.TrimSpace(string(r[:80])) + "…"
	}
	if title == "" {
		return "YouTube video"
	}
	return title
}

func secs(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func fmtSeconds(v float64) string {
	s := int(math.Round(v))
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
