//go:build windows

package main

import "testing"

func TestLocationErrorMentionsSettings(t *testing.T) {
	err := locationError("The service cannot be started, either because it is disabled")
	if err == nil || err.Error() != "turn on location in Windows settings, or type a city" {
		t.Fatal(err)
	}
	if got := locationError("access Denied"); got == nil || got.Error() != err.Error() {
		t.Fatal(got)
	}
	if got := locationError("timed out"); got == nil || got.Error() != "this PC's location did not arrive" {
		t.Fatal(got)
	}
}
