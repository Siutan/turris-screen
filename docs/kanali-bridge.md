# KANALI, UDB, and the Turris 620 bridge

Notes from talking to a TRYX Turris 620 on Windows through the same bridge KANALI uses. This is what was observed on one machine (KANALI 2.5.2, display app `v1.0.8.20260715`, UDB version string `20260909`). It is not an official protocol spec.

The panel still has to be checked by eye after a Show. Uploads and config writes succeed in the logs. A config write updates the stored file name even when the LCD keeps the previous picture.

## Hardware

| | |
|---|---|
| Product | TRYX Turris 620, friendly name “Tryx Turris” |
| USB | VID `0x391A`, PID `0x2011` |
| Example serial | `TURJJ26010002837` |
| Example chip id | `2C88AAD16A551A15` |
| Windows binding | USB printer (`usbprint`, for example `USB001`). KANALI does not print to it. |
| OS on the board | Linux, kernel `5.10.4` |
| Display app | `v1.0.8.20260715` |
| Decode size | 1280×720 |
| Panel | 720×1280, rotated 270° |

KANALI’s product table uses manager key `TURR`, manager type `CUSTOM`, IPC prefix `turris`.

Registry path for the device:

`HKLM\SYSTEM\CurrentControlSet\Enum\USB\VID_391A&PID_2011\<chip id>`

Other TRYX products share this software and a different resolution. A Panorama-class screen in the same app is 2240×1080 or 1440×720. Do not copy those sizes onto a Turris. The Turris class default in the app is 1280×720.

## Where KANALI lives

| Path | What it is |
|---|---|
| `C:\Program Files\KANALI\` | Electron app |
| `C:\Program Files\KANALI\resources\app.asar` | Minified main process. The pipe commands and the media pipeline are in here. |
| `C:\Program Files\KANALI\MediaX.dll` | Native converter. Export `MX_ConvertToH264Raw`. |
| `C:\Program Files\KANALI\resources\Connect\tm\UDB.exe` | USB bridge. Owns WinUSB and the named pipe. |
| `C:\Program Files\KANALI\resources\Connect\ffmpeg\ffmpeg.exe` | FFmpeg `N-119074-gf47c8bca2c-20250330`. `ffprobe.exe` is beside it. KANALI does not use this ffmpeg to build the file the cooler plays. |
| `%AppData%\Roaming\kanali\logs\` | KANALI and UDB logs |
| `%AppData%\Roaming\kanali\media\` | Local copies of media KANALI has prepared |

UDB is a single client. A second program that opens the pipe while KANALI (or this app) is connected gets access denied, Windows error 5. Pipe-busy is error 231. Both mean “someone else has the bridge.”

## How a picture gets to the screen

```
KANALI (Electron)
    JSON over \\.\pipe\TryxBridge
UDB.exe
    WinUSB
Turris (Linux display app)
```

KANALI does not talk USB itself. UDB does. This app does the same: it speaks JSON to UDB and lets UDB own the device.

UDB authenticates when a client is connected and the device is present:

1. `GettingDeviceInfo`
2. `GettingSysConfig`
3. `GettingAuth`
4. `CMD_Ntf_DevOnline`

On a healthy bridge that takes about 100–200 ms. Auth does not start if the client asks for a file list with an empty serial and then drops the pipe. UDB closes USB when the client disconnects.

Official preset downloads are a different path: file type `tmp`, then an SM4 decrypt job. That path is not needed for a picture you encoded yourself, and the keys are not part of this app.

## Who is allowed to own UDB

- If KANALI is running and connected, quit KANALI. Do not kill it.
- If the pipe is missing, start `UDB.exe` yourself. Hide its window.
- On shutdown, kill only the UDB process you started, so KANALI can start its own later.
- A bridge that has been up for a while can open USB and then never send `CMD_Ntf_DevOnline`. Restarting that UDB is what makes the cooler announce itself.
- After a restart, wait for `CMD_Ntf_DevOnline` or a file list that includes a serial. Do not send `CMD_Get_FileList` with an empty `sn`. UDB logs `[Pipe] device not found: sn=` and will not discover the cooler that way.

## The named pipe

Name: `\\.\pipe\TryxBridge`

Messages are JSON, one object per write. UDB is message-mode.

Windows traps:

- A synchronous `ReadFile` holds the handle. A `WriteFile` on the same handle deadlocks. Use overlapped I/O. `github.com/natefinch/npipe` (the 2016 module, `v0.0.0-20160621034901-c1b8fa8bdcce`) does that. Newer pseudo-versions of that module are not fetchable.
- `DialTimeout` turns “pipe not found” and “pipe busy” into a timeout once the deadline passes. Treat that timeout as “pipe missing” only when you are willing to start UDB. Access denied (5) while KANALI is running must stay “KANALI is using the cooler.”
- A short read buffer returns `ERROR_MORE_DATA` (234) with `n > 0`. That is a successful short read, not a failure.
- Two writes back to back can arrive as one read. UDB logs `detected concatenated json payload, parts=2` and still parses both. Concatenated `CMD_Set_UserConfig` + `CMD_Set_RunConfig` has saved the file name without the panel changing. Leave a short gap so they are two messages. KANALI sends them about 1 ms apart.
- A trailing newline on the JSON has been accepted for `CMD_File_Transmit` and `CMD_Set_UserConfig`.

`trackId` must be a numeric string such as `"509116070"`. A value like `"cfg123"` makes UDB log `JsonStringToMessage failed` and drop the message. KANALI’s log prints `trackId` as a number before stringify; a numeric string works.

Objects can be read with `encoding/json` `Decoder` and `UseNumber`. Notifications are not matched by the `trackId` you sent for config reads: `CMD_Ntf_UserConfig` often comes back with `trackId` `"0"`.

## Commands that matter

UDB notifies the client. The client does not poll USB.

| Command | Direction | Role |
|---|---|---|
| `CMD_Ntf_DevOnline` | device → app | Cooler is authenticated. Carries vid, pid, serial, chip id, firmware. |
| `CMD_Ntf_DevOffline` | device → app | Cooler left. |
| `CMD_Get_FileList` | app → device | Needs a real `sn`. |
| `CMD_Ntf_FileList` | device → app | Media and preset files. |
| `CMD_File_Transmit` | app → device | Upload. `fileType` `"media"` for a picture. `fileName` is the full Windows path. |
| `CMD_Ntf_FileTransmitStatus` | device → app | `Transmit` while copying, then `Success`. `fileSize` and `transSize` are the progress counters. UDB echoes the full Windows path, not the basename. |
| `CMD_File_Remove` | app → device | `fileName` is the basename, `fileType` `"media"`. |
| `CMD_Get_UserConfig` | app → device | Read what the cooler has stored. |
| `CMD_Ntf_UserConfig` | device → app | That stored config, expanded with defaults. |
| `CMD_Set_UserConfig` | app → device | Choose the file and the backlight. |
| `CMD_Set_RunConfig` | app → device | Labels. An empty `labelGroups` array is what KANALI sends for a full-screen picture with no text. |
| `CMD_Update_UIItemText` | app → device | Live text on labels. Not used for a still image. |
| `CMD_SysCtl_Exec` | app → device | `Reboot`, or `InstallApp` for firmware. Not part of showing a picture. |
| `CMD_Ntf_FileDecryptJobStatus` | device → app | Official encrypted presets only. |

Transmit status `Success` means the bytes were copied. It does not mean the panel is showing them.

File list entries look like this:

```json
{
  "filePath": "/mnt/data/app/media/2026-10-08_07-20-49-595.gif.h264_1280x720",
  "fileExt": ".h264_1280x720",
  "fileSize": 7074988,
  "readOnly": false
}
```

User files live under `/mnt/data/app/media/`. Presets live under `/mnt/data/app/preset/`. `singleModeMediaFile` is the basename only, not the `/mnt/data/...` path.

Preset names seen on this cooler: `Cosmos`, `Erebos`, `Festival`, `MechanicalArm`, `Meteorite`, `download_117`, all `*.mp4.h264_1280x720`. One KANALI UI path has used `festival.mp4.h264_1440x720`; the file the device listed was the 1280×720 one.

## Showing a clip, the way KANALI does it

From a real GIF apply (`2026-10-8` log, 07:20:52):

1. Crop with MediaX (`MX_ImageClip` / the GIF clip session), not ffmpeg.
2. `MX_ConvertToH264Raw(src, dst, 1280, 720, 60)`. The log line is `convertMediaToH264: sesstion: …, fps: 60`.
3. `CMD_File_Transmit` of the full path to `….gif.h264_1280x720`, `fileType` `"media"`.
4. Wait until `CMD_Ntf_FileTransmitStatus` says `Success`. KANALI then spends about 100 ms in the UI before the next step.
5. `CMD_Set_UserConfig` with a clean object (below).
6. `CMD_Set_RunConfig` with `{"labelGroups":[]}` immediately after, as its own pipe message.

Image uploads in the app call the same converter with fps 30 (`let x=30` in `uploadImageFile`). GIF uploads pass 60. A PNG handed straight to the DLL becomes one frame. A short MP4 handed to the DLL becomes a normal I/P stream. A GIF handed to the DLL at 60 fps becomes a long all-intra stream (on this machine, 120 I-frames, about 6 MB, ~23 Mbit/s) plus the Tryx header. The GIF that KANALI itself put on this cooler was about 7 MB for a 2.1 s source. That is the shape to copy.

`CMD_Set_UserConfig` is persisted. A later `CMD_Get_UserConfig` returns the new basename. The USB payload is sparse (often only `single_mode_media_file` under `work_config`). UDB expands that into `mediaMode`, `loopMode`, empty dual files, and sometimes kaleidoscope and rotation fields. Sending those echoed fields back is unnecessary. KANALI’s working payload does not include them.

Clean payload, field order as KANALI sent it:

```json
{
  "header": { "cmd": "CMD_Set_UserConfig", "sn": "TURJJ26010002837" },
  "userConfig": {
    "poweronConfig": { "mediaFile": "default_poweron_1280x720.mp4.h264" },
    "standbyConfig": { "enable": true, "mediaFile": "default_standby_1280x720.mp4.h264" },
    "workConfig": {
      "mediaMode": "MediaMode_Single",
      "singleModeMediaFile": "2026-10-08_07-20-49-595.gif.h264_1280x720",
      "dualModeLeftMediaFile": "",
      "dualModeRightMediaFile": "",
      "loopMode": "LoopMode_Single"
    },
    "displayConfig": { "backlightBrightness": 80, "backlightEnable": true }
  }
}
```

```json
{
  "header": { "cmd": "CMD_Set_RunConfig", "sn": "TURJJ26010002837" },
  "runConfig": { "labelGroups": [] }
}
```

Names KANALI sends for the built-in boot and standby clips:

- `default_poweron_1280x720.mp4.h264`
- `default_standby_1280x720.mp4.h264`

The device reports those same clips as:

- `default_poweron.mp4.h264_1280x720`
- `default_standby.mp4.h264_1280x720`

Send KANALI’s names. The device rewrites them on readback.

`loopMode` values used in the app: `LoopMode_Single`, `LoopMode_MediaAll`, `LoopMode_MediaRand`. The Turris class default in the bundle is `loopMode: ""`. A successful full-screen apply on this cooler used `LoopMode_Single`. Full-screen mode in the KANALI UI still maps to `MediaMode_Single`.

Go’s `encoding/json` sorts map keys. That produces a different order (`displayConfig` first, `singleModeMediaFile` last). Build the object with a struct so the order stays the one above.

Set the config only after the new basename shows up in `CMD_Ntf_FileList`. Setting it in the same few milliseconds as transmit `Success` stores the name and leaves the old picture up.

Removing the file that is currently `singleModeMediaFile` leaves the player on the last frame it decoded.

## File names

The cooler keys off the basename:

```
YYYY-MM-DD_HH-mm-ss-mmm.<kind>.h264_1280x720
```

`<kind>` is `gif` or `mp4` in files that have played. Milliseconds use a hyphen, not a dot: `2026-10-08_07-20-49-595.gif.h264_1280x720`.

`fileExt` is `.h264_1280x720`. The token before that (`.gif` or `.mp4`) is part of the basename, not a container. The bytes are not an mp4 or a gif.

Transmit `fileName` = full Windows path. Config `singleModeMediaFile` = basename.

## MediaX

`C:\Program Files\KANALI\MediaX.dll`

Loaded by KANALI through a FFI table, cdecl on paper, which is the normal Microsoft x64 calling convention. Strings are `char*`, not UTF-16. ASCII paths are fine.

| Export | Signature from the asar | Notes |
|---|---|---|
| `MX_ConvertToH264Raw` | `int64 (string src, string dst, int width, int height, int fps)` | Returns a session id, or a negative number. Default fps in the wrapper is 60. |
| `MX_SessionGetStatus` | `int64 (int64 session, pointer)` | Return `< 0` means the call failed. The status code is a 4-byte little-endian int in the buffer. |
| `MX_SessionGetProgress` | `int64 (int64 session, pointer)` | Same buffer style. |
| `MX_SessionWait` | `int64 (int64 session, int timeoutMs)` | Not required if you poll status. |
| `MX_SessionGetLastError` | `int64 (int64 session, pointer, pointer)` | |
| `MX_SessionStop` | `int64 (int64 session)` | |

Status codes next to the loader: `1` running, `3` success, `4` canceled, `5` failed. Poll until 3, 4, or 5.

Call `SetDllDirectory` on `C:\Program Files\KANALI` before loading the DLL so its dependencies resolve. Keep the path buffers alive until the session finishes.

The DLL writes its own x264 log to the parent console, including `Error parsing option 'range = pc'`. It still finishes with status 3. That line is from MediaX, not from our ffmpeg.

What came out of it on this PC:

| Input | fps argument | Result |
|---|---|---|
| PNG still | 30 | One I-frame, ~27 KB. Stored. Not shown. |
| 2 s MP4 made by ffmpeg | 30 | ~60 frames, I and P, ~100–130 KB, `yuvj420p`, Main 4.1. Stored. Not shown. |
| 2 s GIF made from the same still | 60 | 120 I-frames, ~5.8 MB, `pix_fmt=yuvj420p`, `color_range=pc`, level 4.1, `r_frame_rate=120/1`. Same family as KANALI’s 7 MB GIF. |

A hand-rolled ffmpeg raw `.h264` (yuv420p, TV range, High or Main, CFR) is also stored and not shown. Do not ship that to the cooler when MediaX is installed.

### Tryx media header

MediaX does not write a bare Annex-B file. The first bytes are a small header, then the H.264.

Observed prefix on a GIF convert (fps 60, 1280×720), hex:

```
41 00 00 00 08 C4 90 E1 EA 04 12 2B
54 72 79 78 20 6D 65 64 69 61 20 68 65 61 64 65 72 20 76 31
2C 20 66 70 73 3D 36 30 2C 20 73 69 7A 65 3D 31 32 38 30 78 37 32 30
18 03 20 01 28 3C 30 80 0A 38 D0 05 40 78
00 00 00 01 09 ...
```

The ASCII in the middle is `Tryx media header v1, fps=60, size=1280x720`. After the protobuf-style fields, Annex-B starts: AUD (`00 00 00 01 09`), SPS (`67`, type 7), PPS (`68`), SEI, then IDRs. Upload the file MediaX wrote. Do not strip the header.

Community write-ups of the direct USB protocol call this wrapper MXHD and treat the transmit-end acknowledgement as the moment the file becomes active. Through UDB, transmit-end `Success` has not been enough on its own; KANALI still sends `CMD_Set_UserConfig` and `CMD_Set_RunConfig` afterwards, and that pair is what changes a preset or a GIF when KANALI does it.

## What was tried and did not change the LCD

All of these returned transmit `Success`, and a later user-config read showed the new basename. The LCD stayed on the last KANALI GIF.

- One-frame MediaX output from a PNG (~30 KB).
- ffmpeg CFR clips (one frame, or 2 seconds, yuv420p, TV range, Main profile).
- MediaX output from an MP4 of the still (I/P frames, ~128 KB), named `.mp4.h264_1280x720`.
- MediaX output from a GIF of the still (all intra, ~5.8 MB, Tryx header, named `.gif.h264_1280x720`), with `CMD_Set_UserConfig` about 3 ms after `Success`.
- Echoing the device’s extra fields (`kaleidoscopeSource`, `mediaRotation`, `mirror`, `uiRotation`).
- `CMD_Set_RunConfig` both glued to the config message and sent as a separate message.

Deleting the active file also leaves the last decoded frame on the panel.

## Frozen player

Seen on 2026-10-09. The panel stopped on one frame of the last KANALI GIF (no animation). From then on every background change failed, including KANALI’s own, while KANALI’s text labels (CPU, GPU, clock) still drew on top. Labels are a separate layer from the media player, so “labels work, background doesn’t” means the player is stuck, not the bridge.

Before it froze, this app had re-uploaded a file under the same basename while that file was the active `singleModeMediaFile` (four times), and had deleted the active file twice. Both are now avoided: every send uses a new basename, and a file is only removed from the cooler when it is not the active one.

Recovery is a board restart:

```json
{ "header": { "cmd": "CMD_SysCtl_Exec", "sn": "TURJJ26010002837" }, "sys_ctl_exec": { "cmd": "Reboot", "params": "" } }
```

KANALI used the same envelope with `"cmd":"InstallApp"` for the firmware update (UDB logged `cmd=1`); the reply is `CMD_SysCtl_Exec_Resp` with `execStatus`, then `CMD_Ntf_DevOffline` and later `CMD_Ntf_DevOnline`. If the restart is refused, a full PC shutdown (not restart) cuts the cooler’s power and has the same effect.

## Logs

| File | What you see |
|---|---|
| `%AppData%\Roaming\kanali\logs\udb_2026-10-09.log` | Pipe JSON, USB auth, transmit begin/data/end, user config protobuf. This is the log that tells you whether the cooler stored the name. |
| `%AppData%\Roaming\kanali\logs\2026-10-8.log` (and the dated KANALI log) | What KANALI thought it sent. The successful GIF apply is the template. |
| `%AppData%\Roaming\turris-screen\bridge.log` | This app: connect, transmit sizes, `listed <name>`, `show <basename>`. |
| `terminals\1.txt` while `wails dev` is running | MediaX’s x264 summary. `frame I:120` and a multi-megabyte size means the GIF path ran. `frame P:` means the MP4 path ran. |

UDB’s `[USB][Recv] UserConfig` block is the device’s copy. Trust that over the app’s own “100%” progress.

## What this app does

`turris-screen` is a Wails window. The Go side is the bridge client and the library. The Svelte side crops to 1280×720 and does not talk to the pipe.

Library: `%AppData%\turris-screen\library\` (`index.json`, `id.png`, a jpeg thumb, and the `.h264_1280x720` file).

Encode path when `MediaX.dll` is present:

1. ffmpeg builds a 2 second, 10 fps GIF at 1280×720.
2. `MX_ConvertToH264Raw` converts that GIF at 1280×720, fps 60.
3. The result is named `….gif.h264_1280x720`.
4. `CMD_File_Transmit` uploads it.
5. `CMD_Get_FileList` until that basename appears.
6. `CMD_Set_UserConfig` using `playbackConfig` (KANALI field order, current backlight if we could read it).
7. 40 ms later, `CMD_Set_RunConfig` with an empty label list.

If MediaX is missing, the app refuses to encode. Plain ffmpeg H.264 is stored by the cooler but never shown.

Every send, including a library re-send, gets a new basename. The previous file is removed from the cooler only when it is no longer `singleModeMediaFile`.

Progress in the title bar is local: encode, then byte counts from `transSize`/`fileSize`, then “showing”. It reaches 100% when the bridge calls return, which has happened without the LCD changing.

## Direct USB, if you skip UDB

Not used here. Other people’s notes on PID `0x2011`:

- Frames are protobuf with a TRYX header, not a printer job.
- Media is 1280×720, MXHD-wrapped.
- File transmit is a begin / data / end sequence. The end acknowledgement is both “bytes arrived” and, in that write-up, the activation.
- That write-up says the Turris path they tested does not use user-config, file list, ping, or keepalive. KANALI on Windows does use user-config and the file list, through UDB, and those commands are accepted by this cooler. Both can be true: UDB may be doing more than the minimal upload.

Useful public projects (behavior, not a dependency of this app):

- [MrEssentials/tryx-linux-display-manager](https://github.com/MrEssentials/tryx-linux-display-manager) — Turris, `391a:2011`
- [DXVSI/Tryx-Linux-GUI](https://github.com/DXVSI/Tryx-Linux-GUI) — Panorama and a narrower Turris upload path

## Open points

- The “What was tried” failures above may all have happened while the player was already frozen (see “Frozen player”). Some of those shapes might work on a healthy panel; they were not retried after the restart.
- Image uploads inside KANALI use fps 30 and were not captured on this cooler. Every picture that is known to have been on this screen came from a GIF converted at fps 60.
