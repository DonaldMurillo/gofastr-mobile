// Package audio validates bounded foreground recordings and local playback data.
package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"mime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/device/files"
)

const (
	MaxSeconds        = 60
	MaxBytes          = files.MaxBytes
	DefaultMaxSeconds = MaxSeconds
	DefaultMaxBytes   = MaxBytes
)

// Request asks for a foreground recording; zero limits use 60 seconds/1 MiB.
type Request struct {
	Title      string `json:"title"`
	MaxSeconds int    `json:"maxSeconds"`
	MaxBytes   int    `json:"maxBytes"`
}

// Document carries private bounded bytes for explicit play/pause/seek controls.
type Document struct {
	File           files.Document `json:"file"`
	DurationMillis int            `json:"durationMillis"`
}

// Validate checks policy before a host may request microphone permission.
func (request Request) Validate() error {
	if request.Title == "" || len(request.Title) > 200 || !utf8.ValidString(request.Title) || strings.ContainsFunc(request.Title, unicode.IsControl) || request.MaxSeconds < 0 || request.MaxSeconds > MaxSeconds || request.MaxBytes < 0 || request.MaxBytes > MaxBytes {
		return errors.New("audio: invalid title or recording limits")
	}
	return nil
}

// Accept verifies reported duration, content bounds, media type and container.
// Signature validation is not full decoding; platform players must fail safely.
func (request Request) Accept(document files.Document, durationMillis int) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if err := document.Validate(); err != nil {
		return err
	}
	seconds, maximum := request.MaxSeconds, request.MaxBytes
	if seconds == 0 {
		seconds = DefaultMaxSeconds
	}
	if maximum == 0 {
		maximum = DefaultMaxBytes
	}
	if durationMillis < 1 || durationMillis > seconds*1000 || len(document.Data) == 0 || len(document.Data) > maximum {
		return errors.New("audio: recording exceeds its duration or byte limits, or is empty")
	}
	mediaType, parameters, err := mime.ParseMediaType(document.MediaType)
	if err != nil {
		return errors.New("audio: invalid media type")
	}
	if codec := parameters["codecs"]; codec != "" {
		compatible := (mediaType == "audio/webm" || mediaType == "audio/ogg" || mediaType == "application/ogg") && codec == "opus" || (mediaType == "audio/mp4" || mediaType == "audio/x-m4a" || mediaType == "audio/aac") && codec == "mp4a.40.2"
		if !compatible {
			return errors.New("audio: unsupported codec for this media type")
		}
	}
	valid := false
	switch mediaType {
	case "audio/aac":
		valid = validADTS(document.Data)
	case "audio/mp4", "audio/x-m4a":
		valid = validM4A(document.Data)
	case "audio/webm":
		valid = len(document.Data) > 32 && bytes.HasPrefix(document.Data, []byte{0x1a, 0x45, 0xdf, 0xa3}) && bytes.Contains(document.Data, []byte("webm")) && bytes.Contains(document.Data, []byte("OpusHead")) && bytes.Contains(document.Data, []byte{0x18, 0x53, 0x80, 0x67})
	case "audio/ogg", "application/ogg":
		valid = validOgg(document.Data)
	case "audio/wav", "audio/wave", "audio/x-wav":
		valid = validWAV(document.Data)
	}
	if !valid {
		return errors.New("audio: unsupported or malformed audio content")
	}
	return nil
}

// Validate applies the maximum recording bounds to a saved playback value.
func (document Document) Validate() error {
	return (Request{Title: "Playback"}).Accept(document.File, document.DurationMillis)
}

func validADTS(data []byte) bool {
	for len(data) > 0 {
		if len(data) < 7 || data[0] != 0xff || data[1]&0xf6 != 0xf0 || data[2]>>2&15 > 12 {
			return false
		}
		header := 7
		if data[1]&1 == 0 {
			header = 9
		}
		length := int(data[3]&3)<<11 | int(data[4])<<3 | int(data[5]>>5)
		if length <= header || length > len(data) {
			return false
		}
		data = data[length:]
	}
	return true
}

func validM4A(data []byte) bool {
	brand, codec, payload := false, false, false
	for len(data) >= 8 {
		length, header := uint64(binary.BigEndian.Uint32(data[:4])), 8
		if length == 1 {
			if len(data) < 16 {
				return false
			}
			length, header = binary.BigEndian.Uint64(data[8:16]), 16
		} else if length == 0 {
			length = uint64(len(data))
		}
		if length < uint64(header) || length > uint64(len(data)) {
			return false
		}
		kind, body := string(data[4:8]), data[header:int(length)]
		switch kind {
		case "ftyp":
			if len(body) < 8 || len(body)%4 != 0 {
				return false
			}
			for offset := 0; offset+4 <= len(body); offset += 4 {
				if offset == 4 {
					continue
				}
				switch string(body[offset : offset+4]) {
				case "M4A ", "mp42", "isom", "iso2":
					brand = true
				}
			}
		case "moov":
			codec = bytes.Contains(body, []byte("mp4a"))
		case "mdat":
			payload = len(body) > 0
		}
		data = data[length:]
	}
	return len(data) == 0 && brand && codec && payload
}

func validOgg(data []byte) bool {
	first, payload := true, false
	for len(data) > 0 {
		if len(data) < 27 || string(data[:4]) != "OggS" || data[4] != 0 {
			return false
		}
		segments := int(data[26])
		if len(data) < 27+segments {
			return false
		}
		size := 0
		for _, length := range data[27 : 27+segments] {
			size += int(length)
		}
		if size == 0 || len(data) < 27+segments+size {
			return false
		}
		body := data[27+segments : 27+segments+size]
		if first {
			if data[5]&2 == 0 || len(body) < 19 || !bytes.HasPrefix(body, []byte("OpusHead")) || body[8] != 1 || body[9] < 1 || body[9] > 2 {
				return false
			}
			first = false
		} else if !bytes.HasPrefix(body, []byte("OpusTags")) {
			payload = true
		}
		data = data[27+segments+size:]
	}
	return !first && payload
}

func validWAV(data []byte) bool {
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return false
	}
	format, payload, alignment := false, false, 0
	data = data[12:]
	for len(data) >= 8 {
		length := int(binary.LittleEndian.Uint32(data[4:8]))
		if length > len(data)-8 {
			return false
		}
		body := data[8 : 8+length]
		switch string(data[:4]) {
		case "fmt ":
			if length < 16 {
				return false
			}
			encoding, channels := binary.LittleEndian.Uint16(body[:2]), binary.LittleEndian.Uint16(body[2:4])
			bits := binary.LittleEndian.Uint16(body[14:16])
			alignment = int(binary.LittleEndian.Uint16(body[12:14]))
			format = (encoding == 1 || encoding == 3 && bits == 32) && channels >= 1 && channels <= 2 && binary.LittleEndian.Uint32(body[4:8]) > 0 && bits >= 8 && bits <= 32 && bits%8 == 0 && alignment == int(channels)*int(bits)/8
		case "data":
			payload = format && length > 0 && length%alignment == 0
		}
		padded := length + length%2
		if padded > len(data)-8 {
			return false
		}
		data = data[8+padded:]
	}
	return len(data) == 0 && format && payload
}
