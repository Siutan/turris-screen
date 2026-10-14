package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxFetchBytes = 256 << 10

func fetchHTTPS(req FetchRequest) ([]byte, error) {
	parsed, err := parseHTTPS(req.URL)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: redirectHTTPS}
	httpReq, err := http.NewRequest(http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("use an https address")
	}
	httpReq.Header.Set("User-Agent", "turris-screen")
	for key, value := range req.Headers {
		if strings.TrimSpace(key) != "" {
			httpReq.Header.Set(key, value)
		}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("the request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("the request failed (%d)", resp.StatusCode)
	}
	return limitedRead(resp.Body)
}

func redirectHTTPS(next *http.Request, via []*http.Request) error {
	if next.URL == nil || next.URL.Scheme != "https" {
		return fmt.Errorf("use an https address")
	}
	if len(via) >= 5 {
		return fmt.Errorf("too many redirects")
	}
	return nil
}

func limitedRead(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxFetchBytes+1))
	if err != nil {
		return nil, fmt.Errorf("the request failed")
	}
	if len(body) > maxFetchBytes {
		return nil, fmt.Errorf("the response is too large")
	}
	return body, nil
}

func parseHTTPS(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("use an https address")
	}
	return parsed, nil
}

func (r Readers) fetch(id, raw string, extra map[string]string) ([]byte, error) {
	if r.Fetch == nil {
		return nil, fmt.Errorf("network sources are unavailable")
	}
	headers := map[string]string{}
	if r.Secret != nil {
		for key, value := range r.Secret(id).Headers {
			headers[key] = value
		}
	}
	for key, value := range extra {
		headers[key] = value
	}
	return r.Fetch(FetchRequest{URL: raw, Headers: headers})
}

func configString(config map[string]any, key string) string {
	if config == nil {
		return ""
	}
	text, _ := config[key].(string)
	return strings.TrimSpace(text)
}

func sampleWeather(query SourceQuery, readers Readers) (map[string]string, error) {
	lat, lon, label, err := weatherPoint(query, readers)
	if err != nil {
		return nil, err
	}
	raw := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s&current=temperature_2m,apparent_temperature,weather_code,precipitation_probability&daily=temperature_2m_max,temperature_2m_min,sunrise,sunset&forecast_days=1&timezone=auto", lat, lon)
	body, err := readers.fetch(query.ID, raw, nil)
	if err != nil {
		return nil, err
	}
	fields, err := weatherFields(body, time.Now())
	if err != nil {
		return nil, err
	}
	if label != "" {
		fields["place"] = label
	}
	return fields, nil
}

func weatherPoint(query SourceQuery, readers Readers) (lat, lon, label string, err error) {
	lat = configString(query.Config, "lat")
	lon = configString(query.Config, "lon")
	label = configString(query.Config, "place")
	if lat != "" && lon != "" {
		return lat, lon, label, nil
	}
	if label != "" {
		lat, lon, err = geocode(label, readers, query.ID)
		return lat, lon, label, err
	}
	if readers.Locate == nil {
		return "", "", "", fmt.Errorf("enter a city")
	}
	lat, lon, err = readers.Locate()
	return lat, lon, "", err
}

var geoMu sync.Mutex
var geoCache = map[string][2]string{}

func geocode(place string, readers Readers, id string) (string, string, error) {
	key := strings.ToLower(place)
	geoMu.Lock()
	if hit, ok := geoCache[key]; ok {
		geoMu.Unlock()
		return hit[0], hit[1], nil
	}
	geoMu.Unlock()
	raw := "https://geocoding-api.open-meteo.com/v1/search?count=1&language=en&format=json&name=" + url.QueryEscape(place)
	body, err := readers.fetch(id, raw, nil)
	if err != nil {
		return "", "", err
	}
	hits := placeHits(body)
	if len(hits) == 0 {
		return "", "", fmt.Errorf("that city was not found")
	}
	lat, lon := hits[0].Lat, hits[0].Lon
	geoMu.Lock()
	geoCache[key] = [2]string{lat, lon}
	geoMu.Unlock()
	return lat, lon, nil
}

// PlaceHit is one city from the geocoder. Lat and Lon are the point the forecast uses.
type PlaceHit struct {
	Name    string `json:"name"`
	Region  string `json:"region"`
	Country string `json:"country"`
	Label   string `json:"label"`
	Lat     string `json:"lat"`
	Lon     string `json:"lon"`
}

// SearchPlaces lists cities matching a few typed letters.
func (a *App) SearchPlaces(query string) ([]PlaceHit, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return []PlaceHit{}, nil
	}
	raw := "https://geocoding-api.open-meteo.com/v1/search?count=8&language=en&format=json&name=" + url.QueryEscape(query)
	body, err := fetchHTTPS(FetchRequest{URL: raw})
	if err != nil {
		return nil, err
	}
	hits := placeHits(body)
	if hits == nil {
		hits = []PlaceHit{}
	}
	return hits, nil
}

func placeHits(body []byte) []PlaceHit {
	var doc struct {
		Results []struct {
			Name      string  `json:"name"`
			Admin1    string  `json:"admin1"`
			Country   string  `json:"country"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil
	}
	out := make([]PlaceHit, 0, len(doc.Results))
	for _, result := range doc.Results {
		if result.Name == "" {
			continue
		}
		label := result.Name
		if result.Admin1 != "" && result.Admin1 != result.Name {
			label += ", " + result.Admin1
		}
		if result.Country != "" {
			label += ", " + result.Country
		}
		out = append(out, PlaceHit{
			Name:    result.Name,
			Region:  result.Admin1,
			Country: result.Country,
			Label:   label,
			Lat:     strconv.FormatFloat(result.Latitude, 'f', 4, 64),
			Lon:     strconv.FormatFloat(result.Longitude, 'f', 4, 64),
		})
	}
	return out
}

func weatherFields(body []byte, now time.Time) (map[string]string, error) {
	var doc struct {
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			Feels       float64 `json:"apparent_temperature"`
			Code        int     `json:"weather_code"`
			Rain        float64 `json:"precipitation_probability"`
		} `json:"current"`
		Daily struct {
			High    []float64 `json:"temperature_2m_max"`
			Low     []float64 `json:"temperature_2m_min"`
			Sunrise []string  `json:"sunrise"`
			Sunset  []string  `json:"sunset"`
		} `json:"daily"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("the weather could not be read")
	}
	high, low, sunrise, sunset := "", "", "", ""
	if len(doc.Daily.High) > 0 {
		high = degrees(doc.Daily.High[0])
	}
	if len(doc.Daily.Low) > 0 {
		low = degrees(doc.Daily.Low[0])
	}
	if len(doc.Daily.Sunrise) > 0 {
		sunrise = wallClock(doc.Daily.Sunrise[0])
	}
	if len(doc.Daily.Sunset) > 0 {
		sunset = wallClock(doc.Daily.Sunset[0])
	}
	return map[string]string{
		"temp":      degrees(doc.Current.Temperature),
		"feels":     degrees(doc.Current.Feels),
		"condition": weatherText(doc.Current.Code),
		"high":      high,
		"low":       low,
		"rain":      fmt.Sprintf("%d%%", int(math.Round(doc.Current.Rain))),
		"sunrise":   sunrise,
		"sunset":    sunset,
		"moon":      moonPhase(now),
	}, nil
}

func degrees(v float64) string {
	return fmt.Sprintf("%d°", int(math.Round(v)))
}

func wallClock(raw string) string {
	for _, layout := range []string{"2006-01-02T15:04", time.RFC3339} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.Format("3:04 PM")
		}
	}
	return raw
}

func weatherText(code int) string {
	switch {
	case code == 0:
		return "Clear"
	case code <= 2:
		return "Partly cloudy"
	case code == 3:
		return "Overcast"
	case code == 45 || code == 48:
		return "Fog"
	case code >= 51 && code <= 57:
		return "Drizzle"
	case code >= 61 && code <= 67:
		return "Rain"
	case code >= 71 && code <= 77:
		return "Snow"
	case code >= 80 && code <= 82:
		return "Showers"
	case code >= 95:
		return "Thunderstorm"
	default:
		return "Unknown"
	}
}

func moonPhase(t time.Time) string {
	known := time.Date(2000, 1, 6, 18, 14, 0, 0, time.UTC)
	age := math.Mod(t.Sub(known).Hours()/24, 29.530588853)
	if age < 0 {
		age += 29.530588853
	}
	switch {
	case age < 1.85:
		return "New moon"
	case age < 7.38:
		return "Waxing crescent"
	case age < 9.22:
		return "First quarter"
	case age < 14.76:
		return "Waxing gibbous"
	case age < 16.61:
		return "Full moon"
	case age < 22.15:
		return "Waning gibbous"
	case age < 23.99:
		return "Last quarter"
	default:
		return "Waning crescent"
	}
}

func sampleCalendar(query SourceQuery, readers Readers) (map[string]string, error) {
	raw := configString(query.Config, "url")
	if raw == "" {
		return nil, fmt.Errorf("enter a calendar address")
	}
	body, err := readers.fetch(query.ID, raw, nil)
	if err != nil {
		return nil, err
	}
	return calendarFields(body, time.Now())
}

type calEvent struct {
	summary string
	start   time.Time
	end     time.Time
	allDay  bool
}

func calendarFields(body []byte, now time.Time) (map[string]string, error) {
	events := parseICS(body)
	if len(events) == 0 {
		return map[string]string{"next": "", "when": "", "today": "", "start": ""}, nil
	}
	var upcoming []calEvent
	for _, event := range events {
		if event.end.Before(now) {
			continue
		}
		upcoming = append(upcoming, event)
	}
	if len(upcoming) == 0 {
		return map[string]string{"next": "", "when": "", "today": "", "start": ""}, nil
	}
	sortEvents(upcoming)
	next := upcoming[0]
	today := make([]string, 0, 3)
	y, m, d := now.Date()
	for _, event := range upcoming {
		ey, em, ed := event.start.Date()
		if ey == y && em == m && ed == d && len(today) < 3 {
			today = append(today, event.summary)
		}
	}
	when := next.start.Local().Format("Mon, 3:04 PM")
	if next.allDay {
		when = next.start.Local().Format("Mon, Jan 2")
	}
	return map[string]string{
		"next":  next.summary,
		"when":  when,
		"today": strings.Join(today, " · "),
		"start": next.start.Format(time.RFC3339),
	}, nil
}

func sortEvents(events []calEvent) {
	for i := 1; i < len(events); i++ {
		item := events[i]
		j := i
		for j > 0 && events[j-1].start.After(item.start) {
			events[j] = events[j-1]
			j--
		}
		events[j] = item
	}
}

func parseICS(body []byte) []calEvent {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	unfolded := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(unfolded) > 0 && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			unfolded[len(unfolded)-1] += strings.TrimLeft(line, " \t")
			continue
		}
		unfolded = append(unfolded, line)
	}
	var events []calEvent
	var current *calEvent
	for _, line := range unfolded {
		upper := strings.ToUpper(line)
		switch {
		case upper == "BEGIN:VEVENT":
			current = &calEvent{}
		case upper == "END:VEVENT" && current != nil:
			if current.summary != "" && !current.start.IsZero() {
				if current.end.IsZero() {
					if current.allDay {
						current.end = current.start.Add(24 * time.Hour)
					} else {
						current.end = current.start.Add(time.Hour)
					}
				}
				events = append(events, *current)
			}
			current = nil
		case current != nil && strings.HasPrefix(upper, "SUMMARY"):
			if _, value, ok := strings.Cut(line, ":"); ok {
				current.summary = strings.TrimSpace(value)
			}
		case current != nil && strings.HasPrefix(upper, "DTSTART"):
			current.start, current.allDay = parseICSTime(line)
		case current != nil && strings.HasPrefix(upper, "DTEND"):
			current.end, _ = parseICSTime(line)
		}
	}
	return events
}

func parseICSTime(line string) (time.Time, bool) {
	_, value, ok := strings.Cut(line, ":")
	if !ok {
		return time.Time{}, false
	}
	value = strings.TrimSpace(value)
	allDay := strings.Contains(strings.ToUpper(line), "VALUE=DATE") && !strings.Contains(value, "T")
	if allDay {
		t, err := time.ParseInLocation("20060102", value, time.Local)
		if err != nil {
			return time.Time{}, false
		}
		return t, true
	}
	if strings.HasSuffix(value, "Z") {
		t, err := time.Parse("20060102T150405Z", value)
		if err != nil {
			return time.Time{}, false
		}
		return t, false
	}
	loc := time.Local
	if _, rest, ok := strings.Cut(strings.ToUpper(line), "TZID="); ok {
		name := rest
		if i := strings.IndexAny(name, ":;"); i >= 0 {
			name = name[:i]
		}
		if zone, err := time.LoadLocation(name); err == nil {
			loc = zone
		}
	}
	t, err := time.ParseInLocation("20060102T150405", value, loc)
	if err != nil {
		return time.Time{}, false
	}
	return t, false
}

func sampleHTTP(query SourceQuery, readers Readers) (map[string]string, error) {
	switch configString(query.Config, "preset") {
	case "ha":
		return sampleHA(query, readers)
	case "github":
		return sampleGitHub(query, readers)
	default:
		raw := configString(query.Config, "url")
		if raw == "" {
			return nil, fmt.Errorf("enter an address")
		}
		body, err := readers.fetch(query.ID, raw, nil)
		if err != nil {
			return nil, err
		}
		path := configString(query.Config, "field")
		if path == "" {
			return nil, fmt.Errorf("enter a field")
		}
		fields := map[string]string{}
		value, err := jsonField(body, path)
		if err != nil {
			return nil, err
		}
		fields["value"] = value
		if path := configString(query.Config, "status"); path != "" {
			if text, err := jsonField(body, path); err == nil {
				fields["status"] = text
			}
		}
		if path := configString(query.Config, "change"); path != "" {
			if text, err := jsonField(body, path); err == nil {
				fields["change"] = text
			}
		}
		return fields, nil
	}
}

func sampleHA(query SourceQuery, readers Readers) (map[string]string, error) {
	base := strings.TrimRight(configString(query.Config, "base"), "/")
	entity := configString(query.Config, "entity")
	if base == "" || entity == "" {
		return nil, fmt.Errorf("enter the Home Assistant address and entity")
	}
	secret := SourceSecret{}
	if readers.Secret != nil {
		secret = readers.Secret(query.ID)
	}
	if secret.Token == "" {
		return nil, fmt.Errorf("add a Home Assistant token")
	}
	raw := base + "/api/states/" + url.PathEscape(entity)
	body, err := readers.fetch(query.ID, raw, map[string]string{"Authorization": "Bearer " + secret.Token})
	if err != nil {
		return nil, err
	}
	state, err := jsonField(body, "state")
	if err != nil {
		return nil, err
	}
	name, _ := jsonField(body, "attributes.friendly_name")
	return map[string]string{"state": state, "name": name}, nil
}

func sampleGitHub(query SourceQuery, readers Readers) (map[string]string, error) {
	repo := configString(query.Config, "repo")
	if !strings.Contains(repo, "/") {
		return nil, fmt.Errorf("enter a repository as owner/name")
	}
	secret := SourceSecret{}
	if readers.Secret != nil {
		secret = readers.Secret(query.ID)
	}
	headers := map[string]string{"Accept": "application/vnd.github+json"}
	if secret.Token != "" {
		headers["Authorization"] = "Bearer " + secret.Token
	}
	body, err := readers.fetch(query.ID, "https://api.github.com/repos/"+repo, headers)
	if err != nil {
		return nil, err
	}
	branch, err := jsonField(body, "default_branch")
	if err != nil || branch == "" {
		branch = "main"
	}
	status := "unknown"
	if statusBody, err := readers.fetch(query.ID, "https://api.github.com/repos/"+repo+"/commits/"+url.PathEscape(branch)+"/status", headers); err == nil {
		if text, err := jsonField(statusBody, "state"); err == nil && text != "" {
			status = text
		}
	}
	reviews := "0"
	if pulls, err := readers.fetch(query.ID, "https://api.github.com/repos/"+repo+"/pulls?state=open&per_page=30", headers); err == nil {
		var list []any
		if json.Unmarshal(pulls, &list) == nil {
			reviews = strconv.Itoa(len(list))
		}
	}
	label := status
	switch status {
	case "success":
		label = "checks passed"
	case "failure", "error":
		label = "checks failed"
	case "pending":
		label = "checks pending"
	}
	summary := label
	if reviews != "0" {
		summary = label + " · " + reviews + " reviews"
	}
	return map[string]string{"status": label, "reviews": reviews, "summary": summary}, nil
}

func sampleFeed(query SourceQuery, readers Readers) (map[string]string, error) {
	raw := configString(query.Config, "url")
	if raw == "" {
		return nil, fmt.Errorf("enter a feed address")
	}
	body, err := readers.fetch(query.ID, raw, nil)
	if err != nil {
		return nil, err
	}
	return feedFields(body)
}

func feedFields(body []byte) (map[string]string, error) {
	var rss struct {
		Channel struct {
			Title string `xml:"title"`
			Items []struct {
				Title string `xml:"title"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(body, &rss); err == nil && (rss.Channel.Title != "" || len(rss.Channel.Items) > 0) {
		return feedMap(rss.Channel.Title, titles(len(rss.Channel.Items), func(i int) string { return rss.Channel.Items[i].Title })), nil
	}
	var atom struct {
		Title   string `xml:"title"`
		Entries []struct {
			Title string `xml:"title"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(body, &atom); err != nil || (atom.Title == "" && len(atom.Entries) == 0) {
		return nil, fmt.Errorf("that feed could not be read")
	}
	return feedMap(atom.Title, titles(len(atom.Entries), func(i int) string { return atom.Entries[i].Title })), nil
}

func titles(n int, at func(int) string) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		text := strings.TrimSpace(at(i))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func feedMap(name string, items []string) map[string]string {
	headline := ""
	joined := ""
	if len(items) > 0 {
		headline = items[0]
		limit := len(items)
		if limit > 3 {
			limit = 3
		}
		joined = strings.Join(items[:limit], " · ")
	}
	return map[string]string{"feed": strings.TrimSpace(name), "headline": headline, "headlines": joined}
}

func sampleCommand(query SourceQuery, readers Readers) (map[string]string, error) {
	path := configString(query.Config, "path")
	if path == "" {
		return nil, fmt.Errorf("choose a program")
	}
	confirmed, _ := query.Config["confirmed"].(bool)
	if !confirmed {
		return nil, fmt.Errorf("confirm this program runs on this PC")
	}
	if readers.Run == nil {
		return nil, fmt.Errorf("commands are unavailable")
	}
	text, err := readers.Run(path)
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(firstLine([]byte(text)))
	if len(text) > 200 {
		text = text[:200]
	}
	return map[string]string{"text": text}, nil
}
