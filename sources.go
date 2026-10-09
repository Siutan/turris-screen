package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxFileBytes = 64 << 10

const tempsUnavailable = "Start LibreHardwareMonitor to read temperatures."

// SourceQuery asks for one live source. Config holds the file path or countdown target.
type SourceQuery struct {
	ID     string         `json:"id"`
	Kind   string         `json:"kind"`
	Config map[string]any `json:"config"`
}

// SourceSample is the latest text for one source. Fields stay empty when Error is set
// so the editor can keep the previous good values.
type SourceSample struct {
	ID     string            `json:"id"`
	Fields map[string]string `json:"fields"`
	Error  string            `json:"error,omitempty"`
}

// Readers is the platform side of sampling. Tests pass fakes.
type Readers struct {
	System   func() (map[string]string, error)
	Temps    func() (map[string]string, error)
	Media    func() (map[string]string, error)
	ReadFile func(path string, max int) ([]byte, error)
}

func sampleAll(queries []SourceQuery, readers Readers) []SourceSample {
	out := make([]SourceSample, 0, len(queries))
	for _, query := range queries {
		fields, err := sampleOne(query, readers)
		sample := SourceSample{ID: query.ID, Fields: map[string]string{}}
		if err != nil {
			sample.Error = err.Error()
		} else if fields != nil {
			sample.Fields = fields
		}
		out = append(out, sample)
	}
	return out
}

func sampleOne(query SourceQuery, readers Readers) (map[string]string, error) {
	switch query.Kind {
	case "system":
		if readers.System == nil {
			return nil, fmt.Errorf("system stats are unavailable")
		}
		return readers.System()
	case "temps":
		if readers.Temps == nil {
			return nil, fmt.Errorf(tempsUnavailable)
		}
		return readers.Temps()
	case "media":
		if readers.Media == nil {
			return nil, fmt.Errorf("now playing is unavailable")
		}
		return readers.Media()
	case "file":
		read := readers.ReadFile
		if read == nil {
			read = readSourceFile
		}
		return sampleFile(query.Config, read)
	default:
		return nil, fmt.Errorf("that source is not available")
	}
}

func readSourceFile(path string, max int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not read that file")
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, int64(max)))
}

func sampleFile(config map[string]any, read func(string, int) ([]byte, error)) (map[string]string, error) {
	path, _ := config["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("choose a file")
	}
	data, err := read(path, maxFileBytes)
	if err != nil {
		return nil, err
	}
	mode, _ := config["mode"].(string)
	if mode == "json" {
		field, _ := config["field"].(string)
		text, err := jsonField(data, field)
		if err != nil {
			return nil, err
		}
		return map[string]string{"text": text}, nil
	}
	return map[string]string{"text": firstLine(data)}, nil
}

func firstLine(data []byte) string {
	text := string(data)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		return text[:i]
	}
	return text
}

func jsonField(data []byte, path string) (string, error) {
	var current any
	if err := json.Unmarshal(data, &current); err != nil {
		return "", fmt.Errorf("that file is not JSON")
	}
	path = strings.TrimSpace(path)
	if path != "" {
		for _, part := range strings.Split(path, ".") {
			obj, ok := current.(map[string]any)
			if !ok {
				return "", fmt.Errorf("could not find %s", path)
			}
			next, ok := obj[part]
			if !ok {
				return "", fmt.Errorf("could not find %s", path)
			}
			current = next
		}
	}
	switch value := current.(type) {
	case string:
		return value, nil
	case float64:
		if value == math.Trunc(value) {
			return strconv.FormatInt(int64(value), 10), nil
		}
		return strconv.FormatFloat(value, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(value), nil
	case nil:
		return "", nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", fmt.Errorf("could not read %s", path)
		}
		return string(encoded), nil
	}
}

func formatBytes(n float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", n, units[i])
	}
	return fmt.Sprintf("%.1f %s", n, units[i])
}

func formatRate(perSecond float64) string {
	return formatBytes(perSecond) + "/s"
}

// hostStats remembers the last CPU and network counters so the next sample can be a rate.
type hostStats struct {
	mu                sync.Mutex
	cpuIdle, cpuTotal uint64
	netIn, netOut     uint64
	at                time.Time
	ready             bool
}

func (s *hostStats) snapshot(idle, total, in, out uint64) (cpu, down, up string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cpu, down, up = "0", "0 B/s", "0 B/s"
	now := time.Now()
	if s.ready && total > s.cpuTotal {
		dTotal := total - s.cpuTotal
		dIdle := idle - s.cpuIdle
		if dIdle > dTotal {
			dIdle = dTotal
		}
		cpu = strconv.Itoa(int(math.Round(float64(dTotal-dIdle) / float64(dTotal) * 100)))
		if sec := now.Sub(s.at).Seconds(); sec > 0.2 {
			down = formatRate(float64(counterDelta(in, s.netIn)) / sec)
			up = formatRate(float64(counterDelta(out, s.netOut)) / sec)
		}
	}
	s.cpuIdle, s.cpuTotal = idle, total
	s.netIn, s.netOut = in, out
	s.at = now
	s.ready = true
	return cpu, down, up
}

func counterDelta(cur, prev uint64) uint64 {
	if cur < prev {
		return 0
	}
	return cur - prev
}
