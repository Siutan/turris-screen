package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFetchRejectsPlainHTTPAndOffHTTPSRedirects(t *testing.T) {
	if _, err := fetchHTTPS(FetchRequest{URL: "http://example.com/score"}); err == nil {
		t.Fatal("plain http was allowed")
	}
	next, _ := http.NewRequest(http.MethodGet, "http://example.com/next", nil)
	if err := redirectHTTPS(next, nil); err == nil {
		t.Fatal("redirect left https")
	}
	stay, _ := http.NewRequest(http.MethodGet, "https://example.com/next", nil)
	via := make([]*http.Request, 5)
	if err := redirectHTTPS(stay, via); err == nil {
		t.Fatal("too many redirects were allowed")
	}
}

func TestLimitedReadCapsTheBody(t *testing.T) {
	_, err := limitedRead(strings.NewReader(strings.Repeat("a", maxFetchBytes+1)))
	if err == nil {
		t.Fatal("oversized body was accepted")
	}
	body, err := limitedRead(strings.NewReader("ok"))
	if err != nil || string(body) != "ok" {
		t.Fatalf("small body: %q %v", body, err)
	}
}

func TestSecretsStayOutOfTheProjectJSON(t *testing.T) {
	store := &secretStore{path: t.TempDir() + "/secrets.json"}
	if err := store.set("s1", SourceSecret{Token: "secret-token", Headers: map[string]string{"X-Key": "abc"}}); err != nil {
		t.Fatal(err)
	}
	query := SourceQuery{ID: "s1", Kind: "http", Config: map[string]any{"url": "https://example.com/score", "field": "price"}}
	data, err := json.Marshal(query)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-token") || strings.Contains(string(data), "X-Key") {
		t.Fatalf("secret leaked into the query: %s", data)
	}
	got := store.get("s1")
	if got.Token != "secret-token" || got.Headers["X-Key"] != "abc" {
		t.Fatalf("secret was not stored: %+v", got)
	}
}

func TestWeatherReadsOpenMeteo(t *testing.T) {
	readers := Readers{Fetch: func(req FetchRequest) ([]byte, error) {
		if strings.Contains(req.URL, "geocoding-api") {
			return []byte(`{"results":[{"latitude":51.5072,"longitude":-0.1276}]}`), nil
		}
		return []byte(`{"current":{"temperature_2m":18.4,"apparent_temperature":17.2,"weather_code":3,"precipitation_probability":40},"daily":{"temperature_2m_max":[21.2],"temperature_2m_min":[12.4],"sunrise":["2026-10-09T07:12"],"sunset":["2026-10-09T18:24"]}}`), nil
	}}
	fields, err := sampleWeather(SourceQuery{ID: "w", Kind: "weather", Config: map[string]any{"place": "Test City Weather"}}, readers)
	if err != nil {
		t.Fatal(err)
	}
	if fields["temp"] != "18°" || fields["feels"] != "17°" || fields["condition"] != "Overcast" || fields["high"] != "21°" || fields["low"] != "12°" || fields["rain"] != "40%" {
		t.Fatalf("weather fields: %+v", fields)
	}
	if fields["sunrise"] != "7:12 AM" || fields["sunset"] != "6:24 PM" || fields["moon"] == "" {
		t.Fatalf("sun and moon: %+v", fields)
	}
	known := time.Date(2000, 1, 6, 18, 14, 0, 0, time.UTC)
	if moonPhase(known) != "New moon" {
		t.Fatal(moonPhase(known))
	}
}

func TestCalendarCachesTheNextEvent(t *testing.T) {
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:Design review\r\nDTSTART:20261009T220000Z\r\nDTEND:20261009T223000Z\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nSUMMARY:All day\r\nDTSTART;VALUE=DATE:20261009\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	now := time.Date(2026, 10, 9, 21, 0, 0, 0, time.UTC)
	fields, err := calendarFields([]byte(ics), now)
	if err != nil {
		t.Fatal(err)
	}
	if fields["next"] == "" || fields["start"] == "" {
		t.Fatalf("missing event: %+v", fields)
	}
	if _, err := time.Parse(time.RFC3339, fields["start"]); err != nil {
		t.Fatal(fields["start"])
	}
	start := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	end := start.Add(30 * time.Minute)
	live := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:Design review\r\nDTSTART:" + start.Format("20060102T150405Z") + "\r\nDTEND:" + end.Format("20060102T150405Z") + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	readers := Readers{Fetch: func(FetchRequest) ([]byte, error) { return []byte(live), nil }}
	got, err := sampleCalendar(SourceQuery{ID: "c", Config: map[string]any{"url": "https://example.com/cal.ics"}}, readers)
	if err != nil || got["next"] != "Design review" {
		t.Fatalf("sample: %+v %v", got, err)
	}
}

func TestJSONUsesTheSecretHeaderAndRSSReadsTitles(t *testing.T) {
	var header string
	readers := Readers{
		Secret: func(string) SourceSecret { return SourceSecret{Headers: map[string]string{"X-Token": "abc"}} },
		Fetch: func(req FetchRequest) ([]byte, error) {
			header = req.Headers["X-Token"]
			if strings.Contains(req.URL, "feed") {
				return []byte(`<rss><channel><title>Desk</title><item><title>One</title></item><item><title>Two</title></item></channel></rss>`), nil
			}
			return []byte(`{"score":{"home":3},"status":"LIVE","change":"+1"}`), nil
		},
	}
	fields, err := sampleHTTP(SourceQuery{ID: "h", Config: map[string]any{"url": "https://example.com/score", "field": "score.home", "status": "status", "change": "change"}}, readers)
	if err != nil {
		t.Fatal(err)
	}
	if header != "abc" || fields["value"] != "3" || fields["status"] != "LIVE" || fields["change"] != "+1" {
		t.Fatalf("json: header %q fields %+v", header, fields)
	}
	feed, err := sampleFeed(SourceQuery{ID: "r", Config: map[string]any{"url": "https://example.com/feed"}}, readers)
	if err != nil {
		t.Fatal(err)
	}
	if feed["feed"] != "Desk" || feed["headline"] != "One" || feed["headlines"] != "One · Two" {
		t.Fatalf("rss: %+v", feed)
	}
	atom, err := feedFields([]byte(`<feed><title>Atom</title><entry><title>Post</title></entry></feed>`))
	if err != nil || atom["headline"] != "Post" || atom["feed"] != "Atom" {
		t.Fatalf("atom: %+v %v", atom, err)
	}
}

func TestHomeAssistantAndGitHubUseTheToken(t *testing.T) {
	readers := Readers{
		Secret: func(string) SourceSecret { return SourceSecret{Token: "tok"} },
		Fetch: func(req FetchRequest) ([]byte, error) {
			if req.Headers["Authorization"] != "Bearer tok" {
				t.Fatalf("token missing: %+v", req.Headers)
			}
			switch {
			case strings.Contains(req.URL, "/api/states/"):
				return []byte(`{"state":"on","attributes":{"friendly_name":"Lamp"}}`), nil
			case strings.Contains(req.URL, "/commits/"):
				return []byte(`{"state":"success"}`), nil
			case strings.Contains(req.URL, "/pulls"):
				return []byte(`[{},{}]`), nil
			default:
				return []byte(`{"default_branch":"main"}`), nil
			}
		},
	}
	ha, err := sampleHA(SourceQuery{ID: "ha", Config: map[string]any{"base": "https://home.example", "entity": "light.lamp"}}, readers)
	if err != nil || ha["state"] != "on" || ha["name"] != "Lamp" {
		t.Fatalf("ha: %+v %v", ha, err)
	}
	if _, err := sampleHA(SourceQuery{ID: "ha", Config: map[string]any{"base": "https://home.example", "entity": "light.lamp"}}, Readers{Fetch: readers.Fetch}); err == nil {
		t.Fatal("missing token was accepted")
	}
	gh, err := sampleGitHub(SourceQuery{ID: "gh", Config: map[string]any{"repo": "octo/screen"}}, readers)
	if err != nil || gh["status"] != "checks passed" || gh["reviews"] != "2" {
		t.Fatalf("github: %+v %v", gh, err)
	}
}

func TestMailStaysOffUntilConnectedAndCommandNeedsConfirmation(t *testing.T) {
	if _, err := sampleMail(SourceQuery{ID: "m", Config: map[string]any{"host": "imap.example.com"}}, Readers{}); err == nil {
		t.Fatal("mail ran without an account")
	}
	fields, err := sampleMail(SourceQuery{ID: "m", Config: map[string]any{"host": "imap.example.com"}}, Readers{
		Secret: func(string) SourceSecret { return SourceSecret{User: "a", Pass: "b"} },
		Mail: func(string, SourceSecret) (map[string]string, error) {
			return map[string]string{"count": "2", "subject": "Hi"}, nil
		},
	})
	if err != nil || fields["count"] != "2" || fields["subject"] != "Hi" {
		t.Fatalf("mail: %+v %v", fields, err)
	}
	ran := false
	readers := Readers{Run: func(string) (string, error) { ran = true; return "hello\nmore", nil }}
	if _, err := sampleCommand(SourceQuery{Config: map[string]any{"path": "C:\\clock.exe", "confirmed": false}}, readers); err == nil || ran {
		t.Fatal("command ran before confirmation")
	}
	got, err := sampleCommand(SourceQuery{Config: map[string]any{"path": "C:\\clock.exe", "confirmed": true}}, readers)
	if err != nil || got["text"] != "hello" {
		t.Fatalf("command: %+v %v", got, err)
	}
}

func TestIMAPCountsUnseenAndReadsTheLatestSubject(t *testing.T) {
	if n := imapUnseen("* SEARCH 1 4 9\r\n"); n != 3 {
		t.Fatal(n)
	}
	if n := imapExists("* 12 EXISTS\r\n"); n != 12 {
		t.Fatal(n)
	}
	if imapSubject("Subject: Hello there\r\n") != "Hello there" {
		t.Fatal(imapSubject("Subject: Hello there\r\n"))
	}
}

func TestLocalKindsAreNotFetched(t *testing.T) {
	for _, kind := range []string{"zone", "countdown", "focus"} {
		fields, err := sampleOne(SourceQuery{Kind: kind}, Readers{})
		if err != nil || len(fields) != 0 {
			t.Fatalf("%s: %+v %v", kind, fields, err)
		}
	}
}

func TestParseHTTPS(t *testing.T) {
	if _, err := parseHTTPS("https://example.com/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseHTTPS("not a url"); err == nil {
		t.Fatal("accepted junk")
	}
	u, _ := url.Parse("https://example.com")
	if u.Host == "" {
		t.Fatal("host")
	}
}
