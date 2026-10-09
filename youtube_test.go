package main

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseYouTubeURL(t *testing.T) {
	ok := []string{
		"https://www.youtube.com/watch?v=jNQXAC9IVRw",
		"https://youtu.be/jNQXAC9IVRw?t=3",
		"youtube.com/shorts/abc123",
		"  https://m.youtube.com/watch?v=x  ",
		"https://music.youtube.com/watch?v=x",
		"http://www.youtube-nocookie.com/embed/x",
	}
	for _, raw := range ok {
		if _, err := parseYouTubeURL(raw); err != nil {
			t.Errorf("%q: %v", raw, err)
		}
	}
	bad := []string{
		"",
		"--exec calc",
		"https://vimeo.com/123",
		"https://youtube.com.evil.example/watch?v=x",
		"https://notyoutube.com/watch?v=x",
		"file:///C:/Windows/win.ini",
		"ftp://youtube.com/x",
	}
	for _, raw := range bad {
		if got, err := parseYouTubeURL(raw); err == nil {
			t.Errorf("%q was accepted as %q", raw, got)
		}
	}
}

func TestYouTubeSection(t *testing.T) {
	cases := []struct {
		start, end, duration float64
		from, to             float64
		whole, fails         bool
	}{
		{0, 0, 19, 0, 19, true, false},
		{0, 0, 3600, 0, 600, false, false},
		{5, 0, 19, 5, 19, false, false},
		{30, 45, 3600, 30, 45, false, false},
		{0, 900, 3600, 0, 600, false, false},
		{0, 50, 19, 0, 19, true, false},
		{20, 0, 19, 0, 0, false, true},
		{10, 5, 19, 0, 0, false, true},
		{-1, 0, 19, 0, 0, false, true},
		{0, 0, 0, 0, 600, false, false},
	}
	for _, c := range cases {
		from, to, whole, err := youtubeSection(c.start, c.end, c.duration)
		if c.fails {
			if err == nil {
				t.Errorf("%v-%v of %v should fail", c.start, c.end, c.duration)
			}
			continue
		}
		if err != nil || from != c.from || to != c.to || whole != c.whole {
			t.Errorf("%v-%v of %v = %v-%v whole=%v err=%v; want %v-%v whole=%v", c.start, c.end, c.duration, from, to, whole, err, c.from, c.to, c.whole)
		}
	}
}

func TestYtdlpError(t *testing.T) {
	cases := map[string]string{
		"ERROR: [youtube] abc: Sign in to confirm your age. This video may be inappropriate":                                  "age-restricted",
		"ERROR: [youtube] abc: Private video. Sign in if you've been granted access":                                          "private",
		"ERROR: [youtube] abc: Video unavailable":                                                                             "unavailable",
		"WARNING: x\nERROR: [youtube] abc: Something new broke":                                                               "YouTube download failed: Something new broke",
		"Error opening input files: Server returned 403 Forbidden (access denied)\nERROR: ffmpeg exited with code 3436169992": "refused",
	}
	for stderr, want := range cases {
		if got := ytdlpError(stderr, errors.New("exit status 1")).Error(); !strings.Contains(got, want) {
			t.Errorf("%q gave %q; want it to mention %q", stderr, got, want)
		}
	}
}

func TestScanLinesOrCR(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("a\rframe=1 time=00:00:01.50\rb\nc"))
	sc.Split(scanLinesOrCR)
	var got []string
	for sc.Scan() {
		got = append(got, sc.Text())
	}
	if strings.Join(got, "|") != "a|frame=1 time=00:00:01.50|b|c" {
		t.Fatalf("got %q", got)
	}
}

func TestCleanTitle(t *testing.T) {
	if got := cleanTitle("  Me   at\nthe zoo "); got != "Me at the zoo" {
		t.Errorf("got %q", got)
	}
	if got := cleanTitle(strings.Repeat("é", 100)); len([]rune(got)) != 81 {
		t.Errorf("long title kept %d runes", len([]rune(got)))
	}
	if cleanTitle("") != "YouTube video" {
		t.Error("empty title has no fallback")
	}
}

// TestYouTubeCancel checks that cancelling a cut ends yt-dlp and its ffmpeg promptly.
func TestYouTubeCancel(t *testing.T) {
	ytdlp := os.Getenv("TURRIS_YTDLP")
	if ytdlp == "" {
		t.Skip("set TURRIS_YTDLP to a yt-dlp.exe to run")
	}
	ffmpeg, err := findFFmpeg()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	info, err := probeVideo(context.Background(), ytdlp, "https://www.youtube.com/watch?v=aqz-KE-bpKQ", filepath.Join(dir, "info.json"))
	if err != nil {
		t.Fatal(err)
	}
	from, to, whole, _ := youtubeSection(0, 0, info.Duration)
	ctx, cancel := context.WithCancel(context.Background())
	var progressed float64
	time.AfterFunc(4*time.Second, cancel)
	began := time.Now()
	_, err = downloadVideo(ctx, ytdlp, ffmpeg, filepath.Join(dir, "info.json"), dir, from, to, whole, func(p float64) { progressed = p })
	if err == nil {
		t.Fatal("the download finished before it could be cancelled")
	}
	if errors.Is(err, errRefused) && ctx.Err() == nil {
		t.Skip("YouTube refused this request; it does that now and then")
	}
	if ctx.Err() == nil {
		t.Fatalf("the download failed by itself: %v", err)
	}
	if took := time.Since(began); took > 12*time.Second {
		t.Fatalf("cancelling took %s", took)
	}
	if progressed == 0 {
		t.Error("no progress was reported while cutting")
	}
	out, _ := exec.Command("tasklist", "/FI", "IMAGENAME eq yt-dlp.exe", "/NH").Output()
	if strings.Contains(strings.ToLower(string(out)), "yt-dlp.exe") {
		t.Errorf("yt-dlp is still running:\n%s", out)
	}
	t.Logf("cancelled after %s at %.0f%%", time.Since(began).Round(time.Millisecond), progressed*100)
}

// TestYouTubeDownload talks to YouTube, so it only runs when TURRIS_YTDLP names a yt-dlp.exe.
func TestYouTubeDownload(t *testing.T) {
	ytdlp := os.Getenv("TURRIS_YTDLP")
	if ytdlp == "" {
		t.Skip("set TURRIS_YTDLP to a yt-dlp.exe to run")
	}
	ffmpeg, err := findFFmpeg()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	ctx := context.Background()
	info, err := probeVideo(ctx, ytdlp, "https://www.youtube.com/watch?v=jNQXAC9IVRw", filepath.Join(dir, "info.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "Me at the zoo" || info.Duration < 15 {
		t.Fatalf("unexpected details %+v", info)
	}
	from, to, whole, err := youtubeSection(2, 6, info.Duration)
	if err != nil {
		t.Fatal(err)
	}
	var last float64
	file, err := downloadVideo(ctx, ytdlp, ffmpeg, filepath.Join(dir, "info.json"), dir, from, to, whole, func(p float64) { last = p })
	if err != nil {
		t.Fatal(err)
	}
	bin := &MediaBin{dir: t.TempDir()}
	asset, err := bin.ImportAs(file, cleanTitle(info.Title))
	if err != nil {
		t.Fatal(err)
	}
	if asset.Kind != "video" || asset.Name != "Me at the zoo" || asset.Size == 0 {
		t.Fatalf("unexpected asset %+v", asset)
	}
	t.Logf("downloaded %s (%d bytes), last progress %.2f", filepath.Base(file), asset.Size, last)
}
