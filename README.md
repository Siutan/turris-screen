# Turris Screen

A small desktop app for the TRYX Turris 620 display. Crop a picture to the cooler’s 1280×720 screen, send it, and keep a library of pictures you have already shown.

Quit KANALI before connecting. KANALI holds the same local bridge (`TryxBridge`) this app uses. The picture stays on the cooler after you close this app.

## Run

```
wails dev
```

`ffmpeg` comes from the KANALI install, or from `PATH` if you have it.

How KANALI, UDB, and the cooler talk to each other is written up in [docs/kanali-bridge.md](docs/kanali-bridge.md).
