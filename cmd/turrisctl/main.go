package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"turris-screen/turris"
)

func main() {
	args := os.Args[1:]
	verbose := false
	if len(args) > 0 && (args[0] == "-v" || args[0] == "--verbose") {
		verbose = true
		args = args[1:]
	}
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	if verbose {
		turris.Trace = trace
	}
	var err error
	switch args[0] {
	case "info":
		err = withDev(func(d *turris.Device) error {
			lines, err := d.Info()
			if err != nil {
				return err
			}
			fmt.Println("serial", d.Serial())
			for _, line := range lines {
				fmt.Println(line)
			}
			return nil
		})
	case "list":
		err = withDev(func(d *turris.Device) error {
			files, err := d.Files()
			if err != nil {
				return err
			}
			if len(files) == 0 {
				fmt.Println("(no files)")
				return nil
			}
			for _, f := range files {
				kind := "user"
				if f.Preset {
					kind = "preset"
				}
				fmt.Printf("%-6s %10d  %s\n", kind, f.Size, f.Path)
			}
			return nil
		})
	case "config":
		err = withDev(func(d *turris.Device) error {
			cfg, err := d.Config()
			if err != nil {
				return err
			}
			fmt.Printf("active    %s\n", cfg.Active)
			fmt.Printf("poweron   %s\n", cfg.Poweron)
			fmt.Printf("standby   %s\n", cfg.Standby)
			fmt.Printf("brightness %d  backlight %v\n", cfg.Brightness, cfg.BacklightOn)
			return nil
		})
	case "ping":
		err = withDev(func(d *turris.Device) error {
			_, err := d.Call(turris.BuildPing(1), time.Second)
			if err != nil {
				return err
			}
			fmt.Println("pong")
			return nil
		})
	case "play":
		if len(args) != 2 {
			fatalf("usage: turrisctl play NAME")
		}
		name := args[1]
		err = withDev(func(d *turris.Device) error {
			if err := d.Play(name); err != nil {
				return err
			}
			fmt.Println("playing", name)
			return nil
		})
	case "rm":
		if len(args) < 2 {
			fatalf("usage: turrisctl rm NAME [category]")
		}
		category := "media"
		if len(args) == 3 {
			category = args[2]
		}
		err = withDev(func(d *turris.Device) error {
			if err := d.Remove(args[1], category); err != nil {
				return err
			}
			fmt.Println("removed", args[1])
			return nil
		})
	case "brightness":
		if len(args) != 2 {
			fatalf("usage: turrisctl brightness 0-100")
		}
		var value int
		if _, scanErr := fmt.Sscan(args[1], &value); scanErr != nil {
			fatalf("brightness must be a number")
		}
		err = withDev(func(d *turris.Device) error {
			if err := d.Brightness(value); err != nil {
				return err
			}
			fmt.Println("brightness", value)
			return nil
		})
	case "upload":
		if len(args) != 2 {
			fatalf("usage: turrisctl upload FILE")
		}
		err = upload(args[1])
	case "reboot":
		err = reboot()
	case "reset":
		err = resetPort()
	case "raw":
		if len(args) != 2 {
			fatalf("usage: turrisctl raw OPCODE")
		}
		var op int
		if _, scanErr := fmt.Sscan(args[1], &op); scanErr != nil {
			fatalf("opcode must be a number")
		}
		err = withDev(func(d *turris.Device) error {
			body, err := d.Call(turris.BuildSysCtl(op, "", 1), 2*time.Second)
			fmt.Printf("reply err=%v\n%s\n", err, hex.EncodeToString(body))
			for _, s := range turris.Strings(body) {
				fmt.Println(s)
			}
			return nil
		})
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func withDev(fn func(*turris.Device) error) error {
	dev, err := turris.Open()
	if err != nil {
		return err
	}
	defer dev.Close()
	fmt.Println("connected", dev.Serial())
	return fn(dev)
}

func reboot() error {
	dev, err := turris.Open()
	if err != nil {
		return err
	}
	fmt.Println("connected", dev.Serial())
	defer dev.Close()
	if err := dev.Reboot(); err != nil {
		return err
	}
	fmt.Println("cooler restarted")
	return nil
}

func resetPort() error {
	dev, err := turris.Open()
	if err != nil {
		return err
	}
	fmt.Println("connected", dev.Serial())
	err = dev.PortReset()
	dev.Close()
	if err != nil {
		return err
	}
	fmt.Println("port reset sent, waiting for the cooler to come back")
	if !waitPresent(true, 15*time.Second) {
		return fmt.Errorf("the cooler did not come back after the port reset")
	}
	return withDev(func(d *turris.Device) error {
		fmt.Println("back", d.Serial())
		return nil
	})
}

func waitPresent(want bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ok, err := turris.Present()
		if err == nil && ok == want {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

func upload(path string) error {
	blob, name, err := mediaBlob(path)
	if err != nil {
		return err
	}
	fmt.Printf("uploading %s (%d bytes)\n", name, len(blob))
	return withDev(func(d *turris.Device) error {
		if err := d.Upload(name, blob, func(sent, total int64) {
			fmt.Printf("\r%d/%d", sent, total)
		}); err != nil {
			fmt.Println()
			return err
		}
		fmt.Println()
		if err := d.Play(name); err != nil {
			return err
		}
		fmt.Println("showing", name)
		return nil
	})
}

func mediaBlob(path string) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	name := time.Now().Format("2006-01-02_15-04-05.000")
	name = strings.ReplaceAll(name, ".", "-") + ".mp4.h264_1280x720"
	if turris.IsMediaBlob(data) {
		return data, name, nil
	}
	if bytes.Contains(data[:min(16, len(data))], []byte{0, 0, 0, 1}) {
		return turris.WrapMedia(data, 60, turris.CountFrames(data)), name, nil
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		ffmpeg = `C:\Program Files\KANALI\resources\Connect\ffmpeg\ffmpeg.exe`
	}
	fmt.Println("encoding", path)
	cmd := exec.Command(ffmpeg,
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-loop", "1", "-t", "2", "-i", path,
		"-an", "-vf", "scale=1280:720,fps=60", "-pix_fmt", "yuv420p",
		"-c:v", "libx264", "-profile:v", "main", "-level", "4.1",
		"-preset", "medium", "-b:v", "500k",
		"-x264-params", "aud=1:repeat-headers=1:annexb=1:keyint=60:min-keyint=6:scenecut=40:bframes=0:weightp=0:8x8dct=0:cabac=1:ref=3",
		"-f", "h264", "pipe:1",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("%s", msg)
	}
	return turris.WrapMedia(stdout.Bytes(), 60, turris.CountFrames(stdout.Bytes())), name, nil
}

func trace(dir string, data []byte, err error) {
	n := len(data)
	show := data
	if len(show) > 48 {
		show = show[:48]
	}
	if err != nil {
		fmt.Printf("  %s err=%v %s\n", dir, err, hex.EncodeToString(show))
		return
	}
	fmt.Printf("  %s %d %s\n", dir, n, hex.EncodeToString(show))
}

func usage() {
	fmt.Fprintf(os.Stderr, `turrisctl talks to the TRYX Turris 620 over USB.

Quit KANALI and Turris Studio first. Only one program can hold the cooler.

  turrisctl info
  turrisctl list
  turrisctl config
  turrisctl ping
  turrisctl play NAME
  turrisctl upload FILE
  turrisctl rm NAME [category]
  turrisctl brightness 0-100
  turrisctl reboot
  turrisctl reset
  turrisctl raw OPCODE

Add -v before the command to print the frames.
`)
}

func fatalf(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(2)
}
