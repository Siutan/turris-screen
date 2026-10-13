// Package turris speaks the TRYX Turris 620 USB protocol.
// The message shapes follow https://github.com/H4rk3nz0/TRYX_TURRIS_LINUX.
package turris

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

const (
	Magic = "TRYX"

	OpPing          = 10
	OpGetDeviceInfo = 100
	OpGetAuth       = 101
	OpGetIdentity   = 102
	OpGetFileList   = 103
	OpGetUserConfig = 104
	OpRunConfig     = 200 // CMD_Set_UserConfig. The payload is the stored clip and backlight.
	OpApply         = 201 // CMD_Set_RunConfig. The payload is the label layer. Empty clears it.
	OpLabels        = 300 // CMD_Update_UIItemText. Replaces label strings without touching the clip.
	OpFileBegin     = 400
	OpFileData      = 401
	OpFileEnd       = 402
	OpFileRemove    = 403
	OpMediaPushA    = 407
	OpMediaPushB    = 408
	OpMediaPushC    = 409
	OpSysCtl        = 450

	SysNone          = 0
	SysInstallApp    = 1
	SysReboot        = 2
	SysRestoreConfig = 3
	SysRestoreDevice = 4
	SysConsole       = 5
	SysReset         = 6

	Width  = 1280
	Height = 720

	// These are the boot clips this panel reports. The spec's examples use a different name shape.
	poweronClip = "default_poweron.mp4.h264_1280x720"
	standbyClip = "default_standby.mp4.h264_1280x720"
)

// Field is one protobuf value. Varint is set for wire type 0.
type Field struct {
	Varint bool
	Num    uint64
	Bytes  []byte
}

func putVarint(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func fieldVarint(field int, v uint64) []byte {
	return append(putVarint(uint64(field<<3)), putVarint(v)...)
}

func zigzag32(n int32) uint64 {
	return uint64(uint32(n<<1) ^ uint32(n>>31))
}

func fieldBytes(field int, data []byte) []byte {
	out := putVarint(uint64(field<<3) | 2)
	out = append(out, putVarint(uint64(len(data)))...)
	return append(out, data...)
}

func readVarint(buf []byte, i int) (uint64, int, error) {
	var val uint64
	var shift uint
	for {
		if i >= len(buf) {
			return 0, i, errors.New("truncated varint")
		}
		b := buf[i]
		i++
		val |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return val, i, nil
		}
		shift += 7
		if shift > 63 {
			return 0, i, errors.New("varint is too long")
		}
	}
}

// Parse reads one protobuf message into fields keyed by field number.
func Parse(body []byte) (map[int][]Field, error) {
	out := map[int][]Field{}
	i := 0
	for i < len(body) {
		tag, next, err := readVarint(body, i)
		if err != nil {
			return nil, err
		}
		i = next
		fn := int(tag >> 3)
		switch tag & 7 {
		case 0:
			v, next, err := readVarint(body, i)
			if err != nil {
				return nil, err
			}
			i = next
			out[fn] = append(out[fn], Field{Varint: true, Num: v})
		case 1:
			if i+8 > len(body) {
				return nil, errors.New("truncated fixed64")
			}
			i += 8
		case 2:
			ln, next, err := readVarint(body, i)
			if err != nil {
				return nil, err
			}
			i = next
			if ln > uint64(len(body)-i) {
				return nil, errors.New("truncated length-delimited field")
			}
			raw := append([]byte(nil), body[i:i+int(ln)]...)
			i += int(ln)
			out[fn] = append(out[fn], Field{Bytes: raw})
		case 5:
			if i+4 > len(body) {
				return nil, errors.New("truncated fixed32")
			}
			i += 4
		default:
			return nil, fmt.Errorf("unsupported protobuf wire type %d", tag&7)
		}
	}
	return out, nil
}

// Frame wraps a protobuf body in the TRYX header.
func Frame(body []byte) []byte {
	out := make([]byte, 8+len(body))
	copy(out, Magic)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(body)))
	copy(out[8:], body)
	return out
}

// Envelope builds a host message. opcode is the top-level field number of the payload.
func Envelope(opcode int, payload []byte, trackID uint64) []byte {
	var header []byte
	if trackID != 0 {
		header = fieldVarint(2, trackID)
	}
	body := append(fieldBytes(1, header), fieldBytes(opcode, payload)...)
	return Frame(body)
}

func BuildPing(trackID uint64) []byte {
	return Envelope(OpPing, fieldBytes(1, []byte("hello?")), trackID)
}

func BuildDeviceInfo(trackID uint64) []byte {
	return Envelope(OpGetDeviceInfo, fieldBytes(1, []byte("NA")), trackID)
}

func BuildAuth(trackID uint64) []byte {
	return Envelope(OpGetAuth, fieldVarint(1, 1), trackID)
}

func BuildIdentity(trackID uint64) []byte {
	return Envelope(OpGetIdentity, fieldBytes(1, []byte("NA")), trackID)
}

func BuildFileListQuery(trackID uint64) []byte {
	return Envelope(OpGetFileList, fieldBytes(1, []byte("N/A")), trackID)
}

func BuildUserConfigQuery(trackID uint64) []byte {
	return Envelope(OpGetUserConfig, fieldBytes(1, []byte("N/A")), trackID)
}

// BuildRunConfig sets the active clip and the backlight.
// An empty active name leaves the current clip alone.
// backlight 0 is sticky on the device; pass 1 and brightness 0 to blank it and still be able to light it again.
func BuildRunConfig(active, poweron, standby string, brightness, backlight int, trackID uint64) []byte {
	if poweron == "" {
		poweron = poweronClip
	}
	if standby == "" {
		standby = standbyClip
	}
	if brightness < 0 {
		brightness = 0
	}
	if brightness > 100 {
		brightness = 100
	}
	if backlight != 0 {
		backlight = 1
	}
	var payload []byte
	payload = append(payload, fieldBytes(1, fieldBytes(1, []byte(poweron)))...)
	standbyMsg := append(fieldVarint(1, 1), fieldBytes(2, []byte(standby))...)
	payload = append(payload, fieldBytes(2, standbyMsg)...)
	if active != "" {
		payload = append(payload, fieldBytes(3, fieldBytes(3, []byte(active)))...)
	}
	display := append(fieldVarint(1, uint64(backlight)), fieldVarint(2, uint64(brightness))...)
	payload = append(payload, fieldBytes(5, display)...)
	return Envelope(OpRunConfig, payload, trackID)
}

func BuildApply(trackID uint64) []byte {
	return Envelope(OpApply, nil, trackID)
}

// OverlayLabel is one line inside a label group. IDs stay stable so a later text
// update can find the same line.
type OverlayLabel struct {
	ID    uint32 `json:"id"`
	Line  uint32 `json:"line"`
	Text  string `json:"text"`
	Size  uint32 `json:"size"`
	Color uint32 `json:"color"`
	Font  string `json:"font"`
}

// OverlayGroup is one box of text on the 1280×720 frame. Align is the device enum:
// 1 left, 2 center, 3 right.
type OverlayGroup struct {
	ID     uint32         `json:"id"`
	X      uint32         `json:"x"`
	Y      uint32         `json:"y"`
	W      uint32         `json:"w"`
	H      uint32         `json:"h"`
	Align  uint32         `json:"align"`
	Labels []OverlayLabel `json:"labels"`
}

// OverlayText is one string in a CMD_Update_UIItemText batch.
type OverlayText struct {
	GroupID uint32 `json:"groupId"`
	LabelID uint32 `json:"labelId"`
	Text    string `json:"text"`
}

// BuildLabelConfig is the run-config message KANALI sends with the label layout.
// The clip itself is unchanged. An empty list clears the layer.
func BuildLabelConfig(groups []OverlayGroup, trackID uint64) []byte {
	var payload []byte
	for _, group := range groups {
		payload = append(payload, fieldBytes(1, encodeLabelGroup(group))...)
	}
	return Envelope(OpApply, payload, trackID)
}

// BuildLabelText is the one-second text push. Layout, font, and position stay as they were.
func BuildLabelText(items []OverlayText, trackID uint64) []byte {
	var payload []byte
	for _, item := range items {
		line := append(fieldVarint(1, uint64(item.LabelID)), fieldBytes(2, []byte(item.Text))...)
		group := append(fieldVarint(1, uint64(item.GroupID)), fieldBytes(2, line)...)
		payload = append(payload, fieldBytes(1, group)...)
	}
	return Envelope(OpLabels, payload, trackID)
}

func encodeLabelGroup(group OverlayGroup) []byte {
	msg := fieldVarint(1, uint64(group.ID))
	msg = append(msg, fieldVarint(2, uint64(group.X))...)
	msg = append(msg, fieldVarint(3, uint64(group.Y))...)
	msg = append(msg, fieldVarint(4, uint64(group.W))...)
	msg = append(msg, fieldVarint(5, uint64(group.H))...)
	if group.Align != 0 {
		msg = append(msg, fieldVarint(6, uint64(group.Align))...)
	}
	// KANALI sends a small negative line gap. The panel uses it when it places the lines.
	msg = append(msg, fieldVarint(7, zigzag32(-10))...)
	for _, label := range group.Labels {
		msg = append(msg, fieldBytes(8, encodeLabel(label))...)
	}
	return msg
}

func encodeLabel(label OverlayLabel) []byte {
	msg := fieldVarint(1, uint64(label.ID))
	msg = append(msg, fieldVarint(2, uint64(label.Line))...)
	if label.Font != "" {
		msg = append(msg, fieldBytes(8, []byte(label.Font))...)
	}
	if label.Size != 0 {
		msg = append(msg, fieldVarint(9, uint64(label.Size))...)
	}
	msg = append(msg, fieldVarint(10, uint64(label.Color))...)
	msg = append(msg, fieldBytes(11, []byte(label.Text))...)
	return msg
}

func BuildFileBegin(name string, total int, trackID uint64) []byte {
	payload := append(fieldBytes(1, []byte(name)), fieldVarint(2, uint64(total))...)
	return Envelope(OpFileBegin, payload, trackID)
}

func BuildFileData(chunk []byte, trackID uint64) []byte {
	return Envelope(OpFileData, fieldBytes(1, chunk), trackID)
}

func BuildFileEnd(category string, trackID uint64) []byte {
	if category == "" {
		category = "media"
	}
	return Envelope(OpFileEnd, fieldBytes(1, []byte(category)), trackID)
}

func BuildFileRemove(name, category string, trackID uint64) []byte {
	if category == "" {
		category = "media"
	}
	payload := append(fieldBytes(1, []byte(name)), fieldBytes(2, []byte(category))...)
	return Envelope(OpFileRemove, payload, trackID)
}

func BuildSysCtl(cmd int, params string, trackID uint64) []byte {
	payload := append(fieldVarint(1, uint64(cmd)), fieldBytes(2, []byte(params))...)
	return Envelope(OpSysCtl, payload, trackID)
}

// ErrorFrom reports a failure carried in response field 2.
func ErrorFrom(body []byte) error {
	fields, err := Parse(body)
	if err != nil {
		return err
	}
	items := fields[2]
	if len(items) == 0 {
		return nil
	}
	inner, err := Parse(items[0].Bytes)
	if err != nil {
		return err
	}
	var code uint64
	var why string
	if v := inner[1]; len(v) > 0 && v[0].Varint {
		code = v[0].Num
	}
	if v := inner[2]; len(v) > 0 && !v[0].Varint {
		why = strings.TrimSpace(string(v[0].Bytes))
	}
	// A zero code with an empty reason or "NA" is the status the panel sends on success.
	if code == 0 && (why == "" || why == "NA" || why == "N/A") {
		return nil
	}
	if why == "" {
		return fmt.Errorf("the cooler refused the request (%d)", code)
	}
	return fmt.Errorf("the cooler refused the request: %s", why)
}

// Strings collects length-delimited text fields, including nested ones one level down.
func Strings(body []byte) []string {
	fields, err := Parse(body)
	if err != nil {
		return nil
	}
	var out []string
	for n := 1; n < 600; n++ {
		for _, f := range fields[n] {
			if f.Varint || len(f.Bytes) == 0 {
				continue
			}
			if s := printable(f.Bytes); s != "" {
				out = append(out, s)
			}
			inner, err := Parse(f.Bytes)
			if err != nil {
				continue
			}
			for _, group := range inner {
				for _, g := range group {
					if g.Varint {
						continue
					}
					if s := printable(g.Bytes); s != "" {
						out = append(out, s)
					}
				}
			}
		}
	}
	return out
}

func printable(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return ""
		}
	}
	return strings.TrimSpace(string(b))
}

// File is one clip stored on the cooler.
type File struct {
	Path   string
	Ext    string
	Size   uint64
	Preset bool
}

// ParseFileList reads a get_file_list response.
func ParseFileList(body []byte) ([]File, error) {
	fields, err := Parse(body)
	if err != nil {
		return nil, err
	}
	var files []File
	for _, raw := range fields[503] {
		inner, err := Parse(raw.Bytes)
		if err != nil {
			return nil, err
		}
		for _, preset := range []bool{false, true} {
			key := 1
			if preset {
				key = 2
			}
			for _, entry := range inner[key] {
				ef, err := Parse(entry.Bytes)
				if err != nil {
					return nil, err
				}
				f := File{Preset: preset}
				if v := ef[1]; len(v) > 0 && !v[0].Varint {
					f.Path = string(v[0].Bytes)
				}
				if v := ef[2]; len(v) > 0 && !v[0].Varint {
					f.Ext = string(v[0].Bytes)
				}
				if v := ef[3]; len(v) > 0 && v[0].Varint {
					f.Size = v[0].Num
				}
				if f.Path != "" {
					files = append(files, f)
				}
			}
		}
	}
	return files, nil
}

// UserConfig is the part of the device config this app acts on.
type UserConfig struct {
	Active        string
	Poweron       string
	Standby       string
	Brightness    int
	HasBrightness bool
	BacklightOn   bool
}

// ParseUserConfig reads a get_user_config response.
func ParseUserConfig(body []byte) (UserConfig, error) {
	fields, err := Parse(body)
	if err != nil {
		return UserConfig{}, err
	}
	var cfg UserConfig
	for _, raw := range fields[504] {
		inner, err := Parse(raw.Bytes)
		if err != nil {
			return UserConfig{}, err
		}
		if power := inner[1]; len(power) > 0 && !power[0].Varint {
			pf, err := Parse(power[0].Bytes)
			if err != nil {
				return UserConfig{}, err
			}
			if v := pf[1]; len(v) > 0 && !v[0].Varint {
				cfg.Poweron = string(v[0].Bytes)
			}
		}
		if standby := inner[2]; len(standby) > 0 && !standby[0].Varint {
			sf, err := Parse(standby[0].Bytes)
			if err != nil {
				return UserConfig{}, err
			}
			if v := sf[2]; len(v) > 0 && !v[0].Varint {
				cfg.Standby = string(v[0].Bytes)
			}
		}
		if work := inner[3]; len(work) > 0 && !work[0].Varint {
			wf, err := Parse(work[0].Bytes)
			if err != nil {
				return UserConfig{}, err
			}
			if v := wf[3]; len(v) > 0 && !v[0].Varint {
				cfg.Active = Basename(string(v[0].Bytes))
			}
		}
		if display := inner[5]; len(display) > 0 && !display[0].Varint {
			df, err := Parse(display[0].Bytes)
			if err != nil {
				return UserConfig{}, err
			}
			if v := df[1]; len(v) > 0 && v[0].Varint {
				cfg.BacklightOn = v[0].Num != 0
			}
			if v := df[2]; len(v) > 0 && v[0].Varint {
				cfg.Brightness = int(v[0].Num)
				cfg.HasBrightness = true
			}
		}
	}
	return cfg, nil
}

// Basename is the clip name the cooler uses in run_config and file_remove.
func Basename(path string) string {
	path = strings.ReplaceAll(path, `\`, `/`)
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// Category is the directory a stored path belongs to: media, preset, or fixed.
func Category(path string) string {
	p := strings.ToLower(strings.ReplaceAll(path, `\`, `/`))
	switch {
	case strings.Contains(p, "/preset/"):
		return "preset"
	case strings.Contains(p, "/fixed/"):
		return "fixed"
	default:
		return "media"
	}
}
