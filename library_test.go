package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testThumb(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 32, 18)), nil); err != nil {
		t.Fatal(err)
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestSaveProjectDraftThenExport(t *testing.T) {
	lib := &Library{dir: t.TempDir()}
	draft, _, _, err := lib.SaveProject(ProjectSave{
		Name: "Mountain View", Kind: "video", Duration: 8, Data: `{"tracks":[]}`, Thumb: testThumb(t),
	}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !draft.Draft || !draft.Editable || draft.RemoteName != "" || draft.Kind != "video" {
		t.Fatalf("unexpected draft %+v", draft)
	}
	data, err := lib.Project(draft.ID)
	if err != nil || data != `{"tracks":[]}` {
		t.Fatalf("project = %q, %v", data, err)
	}

	clip := filepath.Join(t.TempDir(), "clip")
	if err := os.WriteFile(clip, []byte("clip-one"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved, path, previous, err := lib.SaveProject(ProjectSave{
		ID: draft.ID, Name: "Mountain View", Kind: "video", Duration: 8, Data: `{"tracks":[1]}`, Thumb: testThumb(t),
	}, clip, 8)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != draft.ID || saved.Draft || saved.RemoteName == "" || previous != "" {
		t.Fatalf("unexpected export %+v previous=%q", saved, previous)
	}
	if got, _ := os.ReadFile(path); string(got) != "clip-one" {
		t.Fatalf("clip was not moved into the library: %q", got)
	}
	items, err := lib.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("list = %d items, %v", len(items), err)
	}

	renewed, renewedPath, old, err := lib.Renew(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old != saved.RemoteName || renewed.RemoteName == saved.RemoteName {
		t.Fatalf("renew kept the old name: %q -> %q", old, renewed.RemoteName)
	}
	if got, _ := os.ReadFile(renewedPath); string(got) != "clip-one" {
		t.Fatalf("renew lost the clip: %q", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the old clip name is still on disk")
	}

	if err := lib.Discard(saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(lib.dir, saved.ID+".project.json")); !os.IsNotExist(err) {
		t.Fatal("discard left the project file")
	}
}

func TestReplaceClipKeepsProject(t *testing.T) {
	lib := &Library{dir: t.TempDir()}
	clip := filepath.Join(t.TempDir(), "clip")
	if err := os.WriteFile(clip, []byte("clip-one"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved, _, _, err := lib.SaveProject(ProjectSave{
		Name: "Desk", Kind: "widget", Duration: 5, Data: `{"sources":[]}`, Thumb: testThumb(t),
	}, clip, 8)
	if err != nil {
		t.Fatal(err)
	}
	indexBefore, err := os.ReadFile(filepath.Join(lib.dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var before indexFile
	if err := json.Unmarshal(indexBefore, &before); err != nil {
		t.Fatal(err)
	}
	created := before.Items[0].Created
	projectBefore, err := os.ReadFile(filepath.Join(lib.dir, saved.ID+".project.json"))
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Millisecond)
	next := filepath.Join(t.TempDir(), "clip-two")
	if err := os.WriteFile(next, []byte("clip-two"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous, err := lib.ReplaceClip(saved.ID, next, 9)
	if err != nil {
		t.Fatal(err)
	}
	if previous != saved.RemoteName {
		t.Fatalf("previous = %q, saved %q", previous, saved.RemoteName)
	}
	indexAfter, err := os.ReadFile(filepath.Join(lib.dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var after indexFile
	if err := json.Unmarshal(indexAfter, &after); err != nil {
		t.Fatal(err)
	}
	if len(after.Items) != 1 {
		t.Fatalf("items = %d", len(after.Items))
	}
	got := after.Items[0]
	if !got.Created.Equal(created) {
		t.Fatalf("created changed %s -> %s", created, got.Created)
	}
	if got.RemoteName == saved.RemoteName || got.RemoteName == "" || got.Size != 9 || got.Name != "Desk" {
		t.Fatalf("replaced item %+v", got)
	}
	projectAfter, err := os.ReadFile(filepath.Join(lib.dir, saved.ID+".project.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(projectAfter) != string(projectBefore) {
		t.Fatalf("project json changed %s -> %s", projectBefore, projectAfter)
	}
	body, err := os.ReadFile(filepath.Join(lib.dir, got.RemoteName))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "clip-two" {
		t.Fatalf("clip = %q", body)
	}
	if _, err := os.Stat(filepath.Join(lib.dir, previous)); !os.IsNotExist(err) {
		t.Fatal("the previous clip is still on disk")
	}
}

func TestRenewDraftWithoutClip(t *testing.T) {
	lib := &Library{dir: t.TempDir()}
	draft, _, _, err := lib.SaveProject(ProjectSave{Name: "Draft", Data: `{}`, Thumb: testThumb(t)}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := lib.Renew(draft.ID); err == nil {
		t.Fatal("a draft that was never exported should not renew")
	}
}

func TestDuplicateProject(t *testing.T) {
	lib := &Library{dir: t.TempDir()}
	original := `{"name":"Mountain View","tracks":[{"clips":[{"text":"A & B <C>"}]}]}`
	draft, _, _, err := lib.SaveProject(ProjectSave{
		Name: "Mountain View", Kind: "image", Duration: 3, Data: original, Thumb: testThumb(t),
	}, "", 0)
	if err != nil {
		t.Fatal(err)
	}

	copy, err := lib.Duplicate(draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if copy.ID == draft.ID || copy.Name != "Mountain View copy" || !copy.Draft || copy.RemoteName != "" || !copy.Editable || copy.Kind != "image" {
		t.Fatalf("unexpected copy %+v", copy)
	}
	if kept, err := lib.Project(draft.ID); err != nil || kept != original {
		t.Fatalf("original changed: %q %v", kept, err)
	}
	assertProject(t, lib, copy.ID, "Mountain View copy", "A & B <C>")

	again, err := lib.Duplicate(draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Name != "Mountain View copy 2" || again.ID == copy.ID {
		t.Fatalf("unexpected second copy %+v", again)
	}
	fromCopy, err := lib.Duplicate(copy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fromCopy.Name != "Mountain View copy copy" {
		t.Fatalf("duplicating a copy named %q", fromCopy.Name)
	}

	clip := filepath.Join(t.TempDir(), "clip")
	if err := os.WriteFile(clip, []byte("clip-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved, savedPath, _, err := lib.SaveProject(ProjectSave{
		ID: draft.ID, Name: "Mountain View", Kind: "video", Duration: 8, Data: original, Thumb: testThumb(t),
	}, clip, 10)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := lib.Duplicate(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if exported.Draft || exported.RemoteName == "" || exported.RemoteName == saved.RemoteName || exported.Size != 10 {
		t.Fatalf("unexpected exported copy %+v", exported)
	}
	got, err := os.ReadFile(filepath.Join(lib.dir, exported.RemoteName))
	if err != nil || string(got) != "clip-bytes" {
		t.Fatalf("copied clip = %q, %v", got, err)
	}
	if kept, err := os.ReadFile(savedPath); err != nil || string(kept) != "clip-bytes" {
		t.Fatalf("original clip = %q, %v", kept, err)
	}
	assertProject(t, lib, exported.ID, "Mountain View copy 3", "A & B <C>")

	long := strings.Repeat("N", 80)
	longItem, _, _, err := lib.SaveProject(ProjectSave{Name: long, Data: `{"name":"x"}`, Thumb: testThumb(t)}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	longCopy, err := lib.Duplicate(longItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(longCopy.Name) > 80 || !strings.HasSuffix(longCopy.Name, " copy") || longCopy.Name == long {
		t.Fatalf("long copy name %q (%d)", longCopy.Name, len(longCopy.Name))
	}

	if _, err := lib.Duplicate("nope"); err == nil {
		t.Fatal("expected a bad id to be refused")
	}
	if _, err := lib.Duplicate("aaaaaaaaaaaaaaaa"); err == nil {
		t.Fatal("expected a missing project to be refused")
	}
	if err := os.Remove(filepath.Join(lib.dir, fromCopy.ID+".project.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Duplicate(fromCopy.ID); err == nil {
		t.Fatal("expected a legacy item to be refused")
	}
}

func assertProject(t *testing.T, lib *Library, id, name, text string) {
	t.Helper()
	raw, err := lib.Project(id)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Name   string `json:"name"`
		Tracks []struct {
			Clips []struct {
				Text string `json:"text"`
			} `json:"clips"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Name != name || doc.Tracks[0].Clips[0].Text != text {
		t.Fatalf("project = %+v, want name %q text %q", doc, name, text)
	}
}

func TestSaveProjectRejectsBadInput(t *testing.T) {
	lib := &Library{dir: t.TempDir()}
	if _, _, _, err := lib.SaveProject(ProjectSave{Data: "not json", Thumb: testThumb(t)}, "", 0); err == nil {
		t.Fatal("expected invalid JSON to be refused")
	}
	if _, _, _, err := lib.SaveProject(ProjectSave{Data: `{}`, Thumb: "nope"}, "", 0); err == nil {
		t.Fatal("expected a bad thumbnail to be refused")
	}
	if _, err := lib.Project("../../etc"); err == nil {
		t.Fatal("expected a bad id to be refused")
	}
}
