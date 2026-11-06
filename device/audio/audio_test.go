package audio

import (
	"encoding/binary"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/files"
)

func wav() files.Document {
	data := make([]byte, 46)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], 38)
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 8000)
	binary.LittleEndian.PutUint32(data[28:], 16000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 2)
	return files.Document{Name: "recording.wav", MediaType: "audio/wav", Data: data}
}

func TestAudioBoundsAndUntrustedContent(t *testing.T) {
	request, document := Request{Title: "Record"}, wav()
	if err := request.Accept(document, 1000); err != nil {
		t.Fatal(err)
	}
	if err := (Document{File: document, DurationMillis: 60000}).Validate(); err != nil {
		t.Fatal(err)
	}
	for _, duration := range []int{-1, 0, 60001} {
		if request.Accept(document, duration) == nil {
			t.Fatalf("accepted duration %d", duration)
		}
	}
	if (Request{Title: "Record", MaxSeconds: 1}).Accept(document, 1001) == nil {
		t.Fatal("request duration bound ignored")
	}
	if (Request{Title: "Record", MaxBytes: 45}).Accept(document, 1000) == nil {
		t.Fatal("request size bound ignored")
	}
	for _, request := range []Request{{}, {Title: "Record", MaxSeconds: -1}, {Title: "Record", MaxSeconds: 61}, {Title: "Record", MaxBytes: MaxBytes + 1}} {
		if request.Validate() == nil {
			t.Fatalf("invalid request: %+v", request)
		}
	}
	for _, mediaType := range []string{"audio/mpeg", "audio/mp4", "audio/webm", "audio/ogg", "audio/aac", "audio/wav; codecs=opus"} {
		forged := document
		forged.MediaType = mediaType
		if request.Accept(forged, 1000) == nil {
			t.Fatalf("trusted MIME %s without signature", mediaType)
		}
	}
	for length := 0; length < len(document.Data); length++ {
		truncated := document
		truncated.Data = document.Data[:length]
		if request.Accept(truncated, 1000) == nil {
			t.Fatalf("accepted truncated WAV %d", length)
		}
	}
}

func TestAcceptedAudioContainers(t *testing.T) {
	box := func(kind string, body []byte) []byte {
		data := make([]byte, 8+len(body))
		binary.BigEndian.PutUint32(data, uint32(len(data)))
		copy(data[4:], kind)
		copy(data[8:], body)
		return data
	}
	m4a := append(box("ftyp", []byte("M4A \x00\x00\x00\x00isom")), box("moov", []byte("mp4a"))...)
	m4a = append(m4a, box("mdat", []byte{1})...)
	page := func(body []byte, flags byte) []byte {
		data := make([]byte, 28+len(body))
		copy(data, "OggS")
		data[5], data[26], data[27] = flags, 1, byte(len(body))
		copy(data[28:], body)
		return data
	}
	header := append([]byte("OpusHead"), 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	ogg := append(page(header, 2), page([]byte{0xf8, 0xff, 0xfe}, 4)...)
	webm := append([]byte{0x1a, 0x45, 0xdf, 0xa3}, []byte("webm....OpusHead................")...)
	webm = append(webm, 0x18, 0x53, 0x80, 0x67)
	for mediaType, data := range map[string][]byte{"audio/mp4": m4a, "audio/ogg; codecs=opus": ogg, "audio/webm; codecs=opus": webm, "audio/aac": {0xff, 0xf1, 0x50, 0x80, 0x01, 0x1f, 0xfc, 1}} {
		if err := (Request{Title: "Record"}).Accept(files.Document{Name: "untrusted.bin", MediaType: mediaType, Data: data}, 1000); err != nil {
			t.Fatalf("%s: %v", mediaType, err)
		}
	}
}

func TestAndroidM4AExtendedSizeMediaBox(t *testing.T) {
	box := func(kind string, body []byte) []byte {
		b := make([]byte, 8+len(body))
		binary.BigEndian.PutUint32(b, uint32(len(b)))
		copy(b[4:], kind)
		copy(b[8:], body)
		return b
	}
	header := append(box("ftyp", []byte("mp42\x00\x00\x00\x00isommp42")), box("moov", []byte("mp4a"))...)
	media := make([]byte, 20)
	binary.BigEndian.PutUint32(media, 1)
	copy(media[4:], "mdat")
	binary.BigEndian.PutUint64(media[8:], 20)
	copy(media[16:], []byte{1, 2, 3, 4})
	content := append(append([]byte(nil), header...), media...)
	request := Request{Title: "Android recording"}
	document := files.Document{Name: "Recording.m4a", MediaType: "audio/mp4", Data: content}
	if err := request.Accept(document, 1000); err != nil {
		t.Fatal(err)
	}
	for length := 0; length < len(content); length++ {
		document.Data = content[:length]
		if request.Accept(document, 1000) == nil {
			t.Fatalf("accepted truncated extended box at %d", length)
		}
	}
	for _, size := range []uint64{0, 8, 15, 21, ^uint64(0)} {
		binary.BigEndian.PutUint64(media[8:], size)
		document.Data = append(append([]byte(nil), header...), media...)
		if request.Accept(document, 1000) == nil {
			t.Fatalf("accepted invalid extended size %d", size)
		}
	}
}
