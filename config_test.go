package main

import (
	"encoding/json"
	"testing"
)

func TestPlaybackConfigMatchesKANALI(t *testing.T) {
	data, err := json.Marshal(playbackConfig(24, "2026-10-08_07-20-49-595.gif.h264_1280x720"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"poweronConfig":{"mediaFile":"default_poweron_1280x720.mp4.h264"},"standbyConfig":{"enable":true,"mediaFile":"default_standby_1280x720.mp4.h264"},"workConfig":{"mediaMode":"MediaMode_Single","singleModeMediaFile":"2026-10-08_07-20-49-595.gif.h264_1280x720","dualModeLeftMediaFile":"","dualModeRightMediaFile":"","loopMode":"LoopMode_Single"},"displayConfig":{"backlightBrightness":24,"backlightEnable":true}}`
	if string(data) != want {
		t.Fatalf("payload differs from KANALI's\n got %s\nwant %s", data, want)
	}
}

func TestPlaybackConfigDefaultsBrightness(t *testing.T) {
	if got := playbackConfig(0, "a").Display.Brightness; got != 80 {
		t.Fatalf("brightness = %d", got)
	}
}
