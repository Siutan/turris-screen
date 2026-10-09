package turris

import (
	"bytes"
	"encoding/binary"
)

const (
	mxhdMagic = 0x4D584844 // "MXHD"
	ChunkSize = 256 * 1024
)

// WrapMedia builds the blob uploaded with file_begin / file_data / file_end.
// h264 is a raw Annex-B stream. frames is the number of encoded frames.
func WrapMedia(h264 []byte, fps, frames int) []byte {
	if fps <= 0 {
		fps = 60
	}
	if frames <= 0 {
		frames = CountFrames(h264)
	}
	if frames <= 0 {
		frames = 1
	}
	desc := []byte("Tryx media header v1, fps=" + itoa(fps) + ", size=1280x720")
	meta := fieldVarint(1, mxhdMagic)
	meta = append(meta, fieldBytes(2, desc)...)
	meta = append(meta, fieldVarint(3, 4)...)
	meta = append(meta, fieldVarint(4, 1)...)
	meta = append(meta, fieldVarint(5, uint64(fps))...)
	meta = append(meta, fieldVarint(6, Width)...)
	meta = append(meta, fieldVarint(7, Height)...)
	meta = append(meta, fieldVarint(8, uint64(frames))...)
	out := make([]byte, 4+len(meta)+len(h264))
	binary.LittleEndian.PutUint32(out[:4], uint32(len(meta)))
	copy(out[4:], meta)
	copy(out[4+len(meta):], h264)
	return out
}

// IsMediaBlob reports whether b already starts with a Tryx media header.
func IsMediaBlob(b []byte) bool {
	if len(b) < 8 {
		return false
	}
	n := binary.LittleEndian.Uint32(b[:4])
	if n == 0 || int(n) > len(b)-4 {
		return false
	}
	return bytes.Contains(b[4:4+n], []byte("Tryx media header"))
}

// CountFrames counts access-unit delimiters in an Annex-B stream.
func CountFrames(h264 []byte) int {
	n := bytes.Count(h264, []byte{0, 0, 0, 1, 0x09})
	if n == 0 {
		n = bytes.Count(h264, []byte{0, 0, 1, 0x09})
	}
	return n
}

// Chunks splits a blob into 256 KiB pieces. Only the last piece may be shorter.
func Chunks(blob []byte) [][]byte {
	if len(blob) == 0 {
		return nil
	}
	var out [][]byte
	for len(blob) > 0 {
		n := ChunkSize
		if n > len(blob) {
			n = len(blob)
		}
		out = append(out, blob[:n])
		blob = blob[n:]
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
