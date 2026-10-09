package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
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
