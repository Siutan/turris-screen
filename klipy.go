package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	klipyAPI       = "https://api.klipy.com/api/v1"
	klipyFileHost  = "static.klipy.com"
	klipyPerPage   = 24
	klipyMaxBytes  = 40 << 20
	klipyMaxPrompt = 300
)

// KlipyQuery picks a library and a page. Type is gif, clip, sticker, meme, or emoji.
// An empty Query asks for trending items.
type KlipyQuery struct {
	Type  string `json:"type"`
	Query string `json:"query"`
	Page  int    `json:"page"`
}

// KlipyHit is one result the picker can show and import.
type KlipyHit struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Type    string `json:"type"`
	Preview string `json:"preview"`
	FileURL string `json:"fileUrl"`
	Kind    string `json:"kind"`
}

// KlipyPage is one screen of results.
type KlipyPage struct {
	Items   []KlipyHit `json:"items"`
	Page    int        `json:"page"`
	HasNext bool       `json:"hasNext"`
}

// Clips return flat file URLs. GIFs, stickers, memes, and emoji nest formats
// under hd, md, sm, and xs.
var klipyResource = map[string]string{
	"gif":     "gifs",
	"clip":    "clips",
	"sticker": "stickers",
	"meme":    "static-memes",
	"emoji":   "emojis",
}

var klipyDownloadOrder = map[string][]string{
	"gif":     {"gif", "mp4", "webp", "png"},
	"clip":    {"mp4", "webm", "gif", "webp"},
	"sticker": {"gif", "png", "webp", "mp4"},
	"meme":    {"png", "webp", "jpg", "gif"},
	"emoji":   {"png", "webp", "jpg", "gif"},
}

type klipyClient struct {
	key      string
	customer string
	http     *http.Client
}

type rendition struct {
	ext  string
	url  string
	tier int // 0 hd, 1 md, 2 sm, 3 xs, 4 a clip's full file
}

func newKlipyClient() (*klipyClient, error) {
	key, err := klipyAPIKey()
	if err != nil {
		return nil, err
	}
	return &klipyClient{
		key:      key,
		customer: klipyCustomerID(),
		http:     &http.Client{},
	}, nil
}

func (c *klipyClient) search(ctx context.Context, q KlipyQuery) (KlipyPage, error) {
	resource, ok := klipyResource[q.Type]
	if !ok {
		return KlipyPage{}, fmt.Errorf("choose GIFs, clips, stickers, memes, or AI emoji")
	}
	action := "trending"
	query := strings.TrimSpace(q.Query)
	if query != "" {
		action = "search"
	}
	if utf8.RuneCountInString(query) > 100 {
		query = string([]rune(query)[:100])
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	if page > 50 {
		page = 50
	}
	params := url.Values{}
	params.Set("page", fmt.Sprintf("%d", page))
	params.Set("per_page", fmt.Sprintf("%d", klipyPerPage))
	if query != "" {
		params.Set("q", query)
	}
	data, err := c.get(ctx, resource+"/"+action, params)
	if err != nil {
		return KlipyPage{}, err
	}
	return parseKlipyPage(q.Type, data)
}

func (c *klipyClient) generateEmoji(ctx context.Context, prompt string) (string, string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", "", fmt.Errorf("describe the emoji first")
	}
	if utf8.RuneCountInString(prompt) > klipyMaxPrompt {
		return "", "", fmt.Errorf("keep the description under 300 characters")
	}
	body, err := json.Marshal(map[string]string{"prompt": prompt})
	if err != nil {
		return "", "", err
	}
	data, err := c.post(ctx, "emojis/generate", body)
	if err != nil {
		return "", "", err
	}
	id, err := generationID(data)
	if err != nil {
		return "", "", err
	}
	deadline := time.Now().Add(75 * time.Second)
	for {
		status, err := c.get(ctx, "emojis/generated/"+url.PathEscape(id), nil)
		if err != nil {
			return "", "", err
		}
		file, dir, done, err := generatedFile(status)
		if err != nil || done {
			return file, dir, err
		}
		if time.Now().After(deadline) {
			return "", "", fmt.Errorf("KLIPY is still drawing that emoji. Try again in a moment")
		}
		select {
		case <-ctx.Done():
			return "", "", fmt.Errorf("KLIPY took too long to answer")
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

func (c *klipyClient) get(ctx context.Context, path string, params url.Values) (json.RawMessage, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("customer_id", c.customer)
	params.Set("locale", "us")
	params.Set("content_filter", "medium")
	endpoint := c.endpoint(path) + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *klipyClient) post(ctx context.Context, path string, body []byte) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(path), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *klipyClient) endpoint(path string) string {
	return klipyAPI + "/" + url.PathEscape(c.key) + "/" + path
}

func (c *klipyClient) do(req *http.Request) (json.RawMessage, error) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "turris-screen")
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("KLIPY took too long to answer")
		}
		return nil, fmt.Errorf("could not reach KLIPY. Check your internet connection")
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("could not read KLIPY's answer")
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("KLIPY rejected the API key")
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("KLIPY is limiting requests on this key. A testing key allows about 100 an hour")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if msg := klipyErrorText(payload, c.key); msg != "" {
			return nil, fmt.Errorf("KLIPY: %s", msg)
		}
		return nil, fmt.Errorf("KLIPY answered %s", resp.Status)
	}
	var env struct {
		Result bool            `json:"result"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &env); err != nil || len(env.Data) == 0 {
		return nil, fmt.Errorf("KLIPY sent a response this app could not read")
	}
	if !env.Result {
		if msg := klipyErrorText(payload, c.key); msg != "" {
			return nil, fmt.Errorf("KLIPY: %s", msg)
		}
		return nil, fmt.Errorf("KLIPY could not complete that request")
	}
	return env.Data, nil
}

func parseKlipyPage(kind string, data json.RawMessage) (KlipyPage, error) {
	var page struct {
		Data    []json.RawMessage `json:"data"`
		Page    int               `json:"current_page"`
		HasNext bool              `json:"has_next"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return KlipyPage{}, fmt.Errorf("KLIPY sent a response this app could not read")
	}
	out := KlipyPage{Items: []KlipyHit{}, Page: page.Page, HasNext: page.HasNext}
	if out.Page < 1 {
		out.Page = 1
	}
	for _, raw := range page.Data {
		hit, ok := parseKlipyHit(kind, raw)
		if ok {
			out.Items = append(out.Items, hit)
		}
	}
	return out, nil
}

func parseKlipyHit(kind string, raw json.RawMessage) (KlipyHit, bool) {
	var item struct {
		Slug  string          `json:"slug"`
		Title string          `json:"title"`
		Type  string          `json:"type"`
		File  json.RawMessage `json:"file"`
		Blur  string          `json:"blur_preview"`
	}
	if err := json.Unmarshal(raw, &item); err != nil || item.Type == "ad" || len(item.File) == 0 {
		return KlipyHit{}, false
	}
	files := collectRenditions(item.File)
	download, ok := pickDownload(kind, files)
	if !ok || allowKlipyFileURL(download.url) != nil {
		return KlipyHit{}, false
	}
	preview := pickPreview(files)
	if allowKlipyFileURL(preview) != nil {
		preview = ""
	}
	if preview == "" && strings.HasPrefix(item.Blur, "data:image/") && len(item.Blur) < 30000 {
		preview = item.Blur
	}
	if preview == "" {
		preview = download.url
	}
	return KlipyHit{
		Slug:    item.Slug,
		Title:   klipyTitle(item.Title, kind),
		Type:    kind,
		Preview: preview,
		FileURL: download.url,
		Kind:    kindForExt(download.ext),
	}, true
}

func collectRenditions(raw json.RawMessage) []rendition {
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return nil
	}
	var out []rendition
	for key, val := range top {
		if url := jsonString(val); url != "" && knownFormat(key) {
			out = append(out, rendition{ext: key, url: url, tier: 4})
			continue
		}
		tier, ok := sizeTier(key)
		if !ok {
			continue
		}
		var formats map[string]json.RawMessage
		if json.Unmarshal(val, &formats) != nil {
			continue
		}
		for ext, body := range formats {
			if !knownFormat(ext) {
				continue
			}
			var file struct {
				URL string `json:"url"`
			}
			if json.Unmarshal(body, &file) == nil && file.URL != "" {
				out = append(out, rendition{ext: ext, url: file.URL, tier: tier})
			}
		}
	}
	return out
}

func pickDownload(kind string, files []rendition) (rendition, bool) {
	order := klipyDownloadOrder[kind]
	if order == nil {
		order = klipyDownloadOrder["gif"]
	}
	best := rendition{}
	bestScore := 1 << 30
	found := false
	for _, file := range files {
		extScore := indexOf(order, file.ext)
		if extScore < 0 || allowKlipyFileURL(file.url) != nil {
			continue
		}
		score := extScore*10 + file.rank()
		if score < bestScore {
			best, bestScore, found = file, score, true
		}
	}
	return best, found
}

func pickPreview(files []rendition) string {
	best := ""
	bestScore := 1 << 30
	for _, file := range files {
		if file.ext == "mp4" || file.ext == "webm" || allowKlipyFileURL(file.url) != nil {
			continue
		}
		score := previewTier(file.tier)*10 + previewExt(file.ext)
		if score < bestScore {
			best, bestScore = file.url, score
		}
	}
	return best
}

func (r rendition) rank() int {
	switch r.tier {
	case 0:
		return 0
	case 4:
		return 1
	case 1:
		return 2
	case 2:
		return 3
	default:
		return 4
	}
}

func previewTier(tier int) int {
	switch tier {
	case 2:
		return 0
	case 3:
		return 1
	case 1:
		return 2
	case 4:
		return 3
	default:
		return 4
	}
}

func previewExt(ext string) int {
	switch ext {
	case "webp":
		return 0
	case "png":
		return 1
	case "gif":
		return 2
	case "jpg":
		return 3
	default:
		return 9
	}
}

func knownFormat(ext string) bool {
	switch ext {
	case "gif", "webp", "jpg", "png", "mp4", "webm":
		return true
	default:
		return false
	}
}

func sizeTier(name string) (int, bool) {
	switch name {
	case "hd":
		return 0, true
	case "md":
		return 1, true
	case "sm":
		return 2, true
	case "xs":
		return 3, true
	default:
		return 0, false
	}
}

func kindForExt(ext string) string {
	switch ext {
	case "gif":
		return "gif"
	case "mp4", "webm":
		return "video"
	default:
		return "image"
	}
}

func indexOf(list []string, want string) int {
	for i, item := range list {
		if item == want {
			return i
		}
	}
	return -1
}

func jsonString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

func klipyTitle(title, kind string) string {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		switch kind {
		case "clip":
			return "Clip"
		case "sticker":
			return "Sticker"
		case "meme":
			return "Meme"
		case "emoji":
			return "Emoji"
		default:
			return "GIF"
		}
	}
	runes := []rune(title)
	if len(runes) > 80 {
		title = string(runes[:80])
	}
	return title
}

func generationID(data json.RawMessage) (string, error) {
	var obj struct {
		ID string `json:"id"`
	}
	id := ""
	if json.Unmarshal(data, &obj) == nil {
		id = obj.ID
	}
	if id == "" {
		var asString string
		if json.Unmarshal(data, &asString) == nil {
			id = asString
		}
	}
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 80 {
		return "", fmt.Errorf("KLIPY did not return a generation id")
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return "", fmt.Errorf("KLIPY did not return a generation id")
	}
	return id, nil
}

// generatedFile writes a finished emoji. done is false while KLIPY is still drawing.
func generatedFile(data json.RawMessage) (string, string, bool, error) {
	var status struct {
		Status string `json:"status"`
		Result *struct {
			Base64 string `json:"base64_encoded"`
			MIME   string `json:"mime_type"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return "", "", true, fmt.Errorf("KLIPY sent a response this app could not read")
	}
	switch strings.ToLower(status.Status) {
	case "processing", "":
		return "", "", false, nil
	case "failed":
		return "", "", true, fmt.Errorf("KLIPY could not draw that emoji")
	case "success":
		if status.Result == nil || status.Result.Base64 == "" {
			return "", "", true, fmt.Errorf("KLIPY did not return the emoji")
		}
		file, dir, err := writeGenerated(status.Result.Base64, status.Result.MIME)
		return file, dir, true, err
	default:
		return "", "", false, nil
	}
}

func writeGenerated(encoded, mime string) (string, string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil || len(raw) == 0 {
		return "", "", fmt.Errorf("KLIPY did not return the emoji")
	}
	if len(raw) > klipyMaxBytes {
		return "", "", fmt.Errorf("that emoji is too large to add")
	}
	ext := sniffMedia(raw)
	if ext == "" {
		ext = extFromMIME(mime)
	}
	if ext == "" || mediaKind("file"+ext) == "" {
		return "", "", fmt.Errorf("KLIPY sent a file this app could not use")
	}
	return writeTempMedia(raw, ext)
}

func downloadKlipyFile(ctx context.Context, rawURL string) (string, string, error) {
	if err := allowKlipyFileURL(rawURL); err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("that KLIPY file could not be downloaded")
	}
	req.Header.Set("User-Agent", "turris-screen")
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("KLIPY redirected too many times")
		}
		return allowKlipyFileURL(req.URL.String())
	}}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return "", "", fmt.Errorf("KLIPY took too long to answer")
		}
		return "", "", fmt.Errorf("could not download that from KLIPY")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", "", fmt.Errorf("that KLIPY file is no longer available")
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("could not download that from KLIPY")
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, klipyMaxBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("could not download that from KLIPY")
	}
	if len(payload) > klipyMaxBytes {
		return "", "", fmt.Errorf("that KLIPY file is too large to add")
	}
	ext := sniffMedia(payload)
	if ext == "" && resp.Request != nil && resp.Request.URL != nil {
		ext = strings.ToLower(filepath.Ext(resp.Request.URL.Path))
		if ext == ".jpeg" {
			ext = ".jpg"
		}
	}
	if ext == "" || mediaKind("file"+ext) == "" {
		return "", "", fmt.Errorf("KLIPY sent a file this app could not use")
	}
	return writeTempMedia(payload, ext)
}

func writeTempMedia(payload []byte, ext string) (string, string, error) {
	dir, err := os.MkdirTemp("", "turris-klipy-*")
	if err != nil {
		return "", "", err
	}
	file := filepath.Join(dir, "media"+ext)
	if err := os.WriteFile(file, payload, 0o644); err != nil {
		os.RemoveAll(dir)
		return "", "", err
	}
	return file, dir, nil
}

func allowKlipyFileURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || !strings.EqualFold(u.Hostname(), klipyFileHost) {
		return fmt.Errorf("that KLIPY file could not be downloaded")
	}
	return nil
}

func sniffMedia(data []byte) string {
	if len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a"))) {
		return ".gif"
	}
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return ".png"
	}
	if len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return ".webp"
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return ".jpg"
	}
	if len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp")) {
		return ".mp4"
	}
	return ""
}

func extFromMIME(mime string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0])) {
	case "image/gif":
		return ".gif"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/jpeg":
		return ".jpg"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	default:
		return ""
	}
}

func klipyErrorText(payload []byte, key string) string {
	var env struct {
		Message string          `json:"message"`
		Error   string          `json:"error"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(payload, &env) != nil {
		return ""
	}
	msg := strings.TrimSpace(env.Message)
	if msg == "" {
		msg = strings.TrimSpace(env.Error)
	}
	if msg == "" && len(env.Data) > 0 {
		var asString string
		var asObj struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if json.Unmarshal(env.Data, &asString) == nil {
			msg = asString
		} else if json.Unmarshal(env.Data, &asObj) == nil {
			msg = asObj.Message
			if msg == "" {
				msg = asObj.Error
			}
		}
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if key != "" {
		msg = strings.ReplaceAll(msg, key, "…")
	}
	if utf8.RuneCountInString(msg) > 180 {
		msg = string([]rune(msg)[:180])
	}
	return msg
}

var dotenvOnce sync.Once

func loadDotEnv() {
	dotenvOnce.Do(func() {
		for _, path := range dotEnvPaths() {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			applyDotEnv(string(data))
			return
		}
	})
}

func dotEnvPaths() []string {
	var out []string
	if wd, err := os.Getwd(); err == nil {
		for i := 0; i < 4 && wd != ""; i++ {
			out = append(out, filepath.Join(wd, ".env"))
			next := filepath.Dir(wd)
			if next == wd {
				break
			}
			wd = next
		}
	}
	if exe, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(exe), ".env"))
	}
	return out
}

func applyDotEnv(text string) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		os.Setenv(key, val)
	}
}

func klipyAPIKey() (string, error) {
	loadDotEnv()
	key := strings.TrimSpace(os.Getenv("KLIPY_API_KEY"))
	if key == "" {
		return "", fmt.Errorf("add KLIPY_API_KEY to the .env file to use KLIPY")
	}
	return key, nil
}

func klipyCustomerID() string {
	root, err := os.UserConfigDir()
	if err != nil {
		return "turris-screen"
	}
	path := filepath.Join(root, "turris-screen", "klipy.id")
	if data, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id
		}
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "turris-screen"
	}
	id := hex.EncodeToString(buf)
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
		os.WriteFile(path, []byte(id), 0o600)
	}
	return id
}
