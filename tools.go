package main

// warmTools unpacks ffmpeg, ffprobe, yt-dlp, and QuickJS in the background
// so the first encode does not wait on a cold disk.
func warmTools() {
	_, _ = bundledFFmpeg()
	_, _ = bundledFFprobe()
	_, _ = bundledYtdlp()
	_, _ = bundledQJS()
	installNotice()
}
