package main

import (
	"strings"
	"testing"
)

func TestSampleSourcesIsolatesErrors(t *testing.T) {
	out := sampleAll([]SourceQuery{
		{ID: "sys", Kind: "system"},
		{ID: "tmp", Kind: "temps"},
	}, Readers{
		System: func() (map[string]string, error) {
			return map[string]string{"cpu": "42"}, nil
		},
		Temps: func() (map[string]string, error) {
			return nil, errString(tempsUnavailable)
		},
	})
	if len(out) != 2 {
		t.Fatalf("samples = %d", len(out))
	}
	if out[0].Error != "" || out[0].Fields["cpu"] != "42" {
		t.Fatalf("system sample %+v", out[0])
	}
	if out[1].Error != tempsUnavailable || len(out[1].Fields) != 0 {
		t.Fatalf("temps sample %+v", out[1])
	}
}

func TestSampleFileLineAndJSONCap(t *testing.T) {
	payload := append([]byte("hello\n"), []byte(strings.Repeat("a", 80<<10))...)
	var asked int
	fields, err := sampleFile(map[string]any{"path": "notes.txt", "mode": "line"}, func(path string, max int) ([]byte, error) {
		if path != "notes.txt" {
			t.Fatalf("path %q", path)
		}
		asked = max
		if len(payload) > max {
			return payload[:max], nil
		}
		return payload, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if asked != maxFileBytes {
		t.Fatalf("read cap = %d", asked)
	}
	if fields["text"] != "hello" {
		t.Fatalf("line = %q", fields["text"])
	}

	fields, err = sampleFile(map[string]any{"path": "score.json", "mode": "json", "field": "game.home"}, func(string, int) ([]byte, error) {
		return []byte(`{"game":{"home":101,"name":"Celtics"}}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fields["text"] != "101" {
		t.Fatalf("json field = %q", fields["text"])
	}
}

type errString string

func (e errString) Error() string { return string(e) }
