# Turris Screen

A small desktop app for the TRYX Turris 620 display. Crop a picture to the cooler’s 1280×720 screen, send it, and keep a library of pictures you have already shown.

Quit KANALI before connecting. KANALI holds the same local bridge (`TryxBridge`) this app uses. The picture stays on the cooler after you close this app.

## Run

```
wails dev
```

## Standalone app

```
wails build -platform windows/amd64
```

That writes `build/bin/turris-screen.exe`. The executable contains the window, ffmpeg, ffprobe, yt-dlp, and QuickJS. The first launch unpacks them next to the app's other data. Windows 11 already has the WebView2 runtime the window needs; on a machine that does not, the exe downloads it once.

The build downloads those programs into `bundle/` (that folder is not committed) and packs them in. `go run ./scripts/fetch-tools.go` does the download on its own. A dev build that has not downloaded them yet still uses ffmpeg from the KANALI install, or from `PATH`.

How KANALI, UDB, and the cooler talk to each other is written up in [docs/kanali-bridge.md](docs/kanali-bridge.md).
