//go:build !windows

package main

func bundledFFmpeg() (string, error)  { return "", nil }
func bundledFFprobe() (string, error) { return "", nil }
func bundledYtdlp() (string, error)   { return "", nil }
func bundledQJS() (string, error)     { return "", nil }

func installNotice() {}
