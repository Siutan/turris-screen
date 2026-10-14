//go:build windows

package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func deviceLocation() (string, string, error) {
	locMu.Lock()
	if !locCache.at.IsZero() && locCache.err == nil && time.Since(locCache.at) < 30*time.Minute {
		lat, lon := locCache.lat, locCache.lon
		locMu.Unlock()
		return lat, lon, nil
	}
	locMu.Unlock()

	lat, lon, err := queryDeviceLocation()
	locMu.Lock()
	locCache = locHit{lat: lat, lon: lon, at: time.Now(), err: err}
	locMu.Unlock()
	return lat, lon, err
}

type locHit struct {
	lat, lon string
	at       time.Time
	err      error
}

var (
	locMu    sync.Mutex
	locCache locHit
)

func queryDeviceLocation() (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", locationScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", "", fmt.Errorf("this PC's location did not arrive")
	}
	if err != nil {
		return "", "", locationError(stderr.String() + " " + err.Error())
	}
	parts := strings.Split(strings.TrimSpace(stdout.String()), ",")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("this PC's location is unavailable")
	}
	lat, lon := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	latF, latErr := strconv.ParseFloat(lat, 64)
	lonF, lonErr := strconv.ParseFloat(lon, 64)
	if latErr != nil || lonErr != nil {
		return "", "", fmt.Errorf("this PC's location is unavailable")
	}
	return strconv.FormatFloat(latF, 'f', 4, 64), strconv.FormatFloat(lonF, 'f', 4, 64), nil
}

func locationError(detail string) error {
	text := strings.ToLower(detail)
	switch {
	case strings.Contains(text, "access"), strings.Contains(text, "denied"), strings.Contains(text, "disabled"), strings.Contains(text, "cannot be started"):
		return fmt.Errorf("turn on location in Windows settings, or type a city")
	case strings.Contains(text, "timed out"):
		return fmt.Errorf("this PC's location did not arrive")
	default:
		return fmt.Errorf("this PC's location is unavailable")
	}
}

var locationScript = strings.Join([]string{
	"$ErrorActionPreference = 'Stop'",
	"Add-Type -AssemblyName System.Runtime.WindowsRuntime",
	"[void][Windows.Devices.Geolocation.Geolocator, Windows.System, ContentType=WindowsRuntime]",
	"$asTask = @([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation" + "`" + "1' })[0]",
	"function Await([object]$op, [type]$resultType) {",
	"  $task = $asTask.MakeGenericMethod($resultType).Invoke($null, @(,$op))",
	"  try { $done = $task.Wait(8000) } catch { throw $task.Exception.GetBaseException().Message }",
	"  if (-not $done) { throw 'timed out' }",
	"  if ($task.IsFaulted) { throw $task.Exception.GetBaseException().Message }",
	"  return $task.Result",
	"}",
	"$access = [string](Await ([Windows.Devices.Geolocation.Geolocator]::RequestAccessAsync()) ([Windows.Devices.Geolocation.GeolocationAccessStatus]))",
	"if ($access -ne 'Allowed') { throw ('access ' + $access) }",
	"$geo = [Windows.Devices.Geolocation.Geolocator]::new()",
	"$geo.DesiredAccuracyInMeters = 5000",
	"$pos = Await ($geo.GetGeopositionAsync()) ([Windows.Devices.Geolocation.Geoposition])",
	"$c = $pos.Coordinate.Point.Position",
	"'{0},{1}' -f $c.Latitude, $c.Longitude",
}, "\n")
