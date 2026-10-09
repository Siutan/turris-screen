package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseKlipyGIFPage(t *testing.T) {
	page, err := parseKlipyPage("gif", []byte(`{
		"data": [
			{"type":"ad","content":"<b></b>","width":300,"height":250},
			{
				"slug":"hello-hi","title":"Hello","type":"gif",
				"file":{
					"hd":{"gif":{"url":"https://static.klipy.com/hd.gif","width":498,"height":400,"size":1000},"webp":{"url":"https://static.klipy.com/hd.webp","width":498,"height":400,"size":100}},
					"sm":{"gif":{"url":"https://static.klipy.com/sm.gif","width":220,"height":180,"size":100},"webp":{"url":"https://static.klipy.com/sm.webp","width":220,"height":180,"size":40}}
				}
			},
			{"slug":"foreign","title":"No","type":"gif","file":{"hd":{"gif":{"url":"https://evil.example/x.gif","width":1,"height":1,"size":1}}}}
		],
		"current_page":2,
		"has_next":true
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasNext || page.Page != 2 || len(page.Items) != 1 {
		t.Fatalf("page = %+v", page)
	}
	hit := page.Items[0]
	if hit.FileURL != "https://static.klipy.com/hd.gif" || hit.Preview != "https://static.klipy.com/sm.webp" || hit.Kind != "gif" || hit.Title != "Hello" {
		t.Fatalf("hit = %+v", hit)
	}
}

func TestParseKlipyClip(t *testing.T) {
	page, err := parseKlipyPage("clip", []byte(`{
		"data":[{
			"slug":"spider","title":"They love me","type":"clip",
			"file":{"mp4":"https://static.klipy.com/c.mp4","gif":"https://static.klipy.com/c.gif","webp":"https://static.klipy.com/c.webp"}
		}],
		"current_page":1,
		"has_next":false
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %+v", page.Items)
	}
	hit := page.Items[0]
	if hit.FileURL != "https://static.klipy.com/c.mp4" || hit.Preview != "https://static.klipy.com/c.webp" || hit.Kind != "video" {
		t.Fatalf("hit = %+v", hit)
	}
}

func TestParseKlipyStickerPrefersGIF(t *testing.T) {
	raw := []byte(`{"slug":"wave","title":"Wave","type":"sticker","file":{
		"hd":{"png":{"url":"https://static.klipy.com/s.png","width":498,"height":498,"size":10},"gif":{"url":"https://static.klipy.com/s.gif","width":498,"height":498,"size":20}},
		"sm":{"webp":{"url":"https://static.klipy.com/s.webp","width":90,"height":90,"size":2}}
	}}`)
	hit, ok := parseKlipyHit("sticker", raw)
	if !ok || hit.FileURL != "https://static.klipy.com/s.gif" || hit.Kind != "gif" || hit.Preview != "https://static.klipy.com/s.webp" {
		t.Fatalf("ok=%v hit=%+v", ok, hit)
	}
}

func TestAllowKlipyFileURL(t *testing.T) {
	ok := "https://static.klipy.com/ii/abc/file.gif"
	if err := allowKlipyFileURL(ok); err != nil {
		t.Fatal(err)
	}
	bad := []string{
		"http://static.klipy.com/a.gif",
		"https://user:pass@static.klipy.com/a.gif",
		"https://evil.example/a.gif",
		"https://static.klipy.com.evil.example/a.gif",
		"https://api.klipy.com/api/v1/secret/gifs/search",
		"",
	}
	for _, raw := range bad {
		if err := allowKlipyFileURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestKlipyErrorTextScrubsKey(t *testing.T) {
	body := []byte(`{"message":"bad key secret-key in https://api.klipy.com/api/v1/secret-key/gifs"}`)
	got := klipyErrorText(body, "secret-key")
	if strings.Contains(got, "secret-key") || got == "" {
		t.Fatal(got)
	}
}

func TestGenerationID(t *testing.T) {
	id, err := generationID([]byte(`{"id":"c88e2aa7-5c25-40d1-bc24-bfa5562a71e4"}`))
	if err != nil || id != "c88e2aa7-5c25-40d1-bc24-bfa5562a71e4" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if _, err := generationID([]byte(`{"id":"../gifs"}`)); err == nil {
		t.Fatal("accepted a path")
	}
}

func TestGeneratedFileProcessing(t *testing.T) {
	_, _, done, err := generatedFile([]byte(`{"id":"abc","status":"processing","result":null}`))
	if err != nil || done {
		t.Fatalf("done=%v err=%v", done, err)
	}
}

func TestSniffMedia(t *testing.T) {
	if sniffMedia([]byte("GIF89ahello")) != ".gif" {
		t.Fatal("gif")
	}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0}
	if sniffMedia(png) != ".png" {
		t.Fatal("png")
	}
	webp := append([]byte("RIFF"), []byte{0, 0, 0, 0}...)
	webp = append(webp, []byte("WEBP")...)
	if sniffMedia(webp) != ".webp" {
		t.Fatal("webp")
	}
	mp4 := make([]byte, 12)
	copy(mp4[4:], []byte("ftyp"))
	if sniffMedia(mp4) != ".mp4" {
		t.Fatal("mp4")
	}
}

func TestKlipyGenerate(t *testing.T) {
	if os.Getenv("KLIPY_GENERATE") == "" {
		t.Skip("set KLIPY_GENERATE=1 to spend one AI emoji generation")
	}
	loadDotEnv()
	c, err := newKlipyClient()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	file, dir, err := c.generateEmoji(ctx, "a small blue star")
	if dir != "" {
		t.Cleanup(func() { os.RemoveAll(dir) })
	}
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("generated emoji was empty")
	}
}

func TestKlipyLive(t *testing.T) {
	loadDotEnv()
	if strings.TrimSpace(os.Getenv("KLIPY_API_KEY")) == "" {
		t.Skip("KLIPY_API_KEY is not set")
	}
	c, err := newKlipyClient()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	kinds := []string{"gif", "clip", "sticker", "meme", "emoji"}
	var sample string
	for _, kind := range kinds {
		page, err := c.search(ctx, KlipyQuery{Type: kind, Page: 1})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if len(page.Items) == 0 {
			t.Fatalf("%s: no items", kind)
		}
		hit := page.Items[0]
		if err := allowKlipyFileURL(hit.FileURL); err != nil {
			t.Fatalf("%s file: %v", kind, err)
		}
		wantKind := map[string]string{"gif": "gif", "clip": "video", "meme": "image", "emoji": "image"}[kind]
		if wantKind != "" && hit.Kind != wantKind {
			t.Fatalf("%s kind = %s, want %s (%s)", kind, hit.Kind, wantKind, hit.FileURL)
		}
		if kind == "sticker" && hit.Kind != "gif" && hit.Kind != "image" {
			t.Fatalf("sticker kind = %s", hit.Kind)
		}
		if hit.Title == "" {
			t.Fatalf("%s hit = %+v", kind, hit)
		}
		if sample == "" && (hit.Kind == "image" || hit.Kind == "gif") {
			sample = hit.Preview
			if allowKlipyFileURL(sample) != nil {
				sample = hit.FileURL
			}
		}
		blob, _ := json.Marshal(hit)
		if strings.Contains(string(blob), c.key) {
			t.Fatalf("%s result included the API key", kind)
		}
	}
	searched, err := c.search(ctx, KlipyQuery{Type: "gif", Query: "cat", Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(searched.Items) == 0 {
		t.Fatal("gif search returned nothing")
	}
	if sample == "" {
		t.Fatal("no file to download")
	}
	file, dir, err := downloadKlipyFile(ctx, sample)
	if dir != "" {
		t.Cleanup(func() { os.RemoveAll(dir) })
	}
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("downloaded file was empty")
	}
}
