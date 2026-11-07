package vt

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
)

func TestImageVTKitty(t *testing.T) {
	screen := New(20, 10)
	screen.Write([]byte("\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"))
	if string(screen.Replies) != "\x1b_Gi=31;OK\x1b\\" {
		t.Fatalf("reply %q", screen.Replies)
	}
	data := base64.StdEncoding.EncodeToString([]byte{255, 0, 0, 255, 0, 255, 0, 255})
	screen.Write([]byte("\x1b_Ga=t,t=d,f=32,s=2,v=1,i=9,m=1;" + data[:8] + "\x1b\\"))
	screen.Write([]byte("\x1b_Gm=0;" + data[8:] + "\x1b\\"))
	if screen.Images[9].Width != 2 || screen.Images[9].Height != 1 {
		t.Fatalf("images %v", screen.Images)
	}
	screen.Write([]byte("\x1b[3;4H\x1b_Ga=p,i=9,p=7,c=4,r=2,x=1,y=0,w=1,h=1,C=1\x1b\\"))
	if len(screen.Errors) != 0 || len(screen.Placements) != 1 || screen.Placements[0].Rect != image.Rect(3, 2, 7, 4) || screen.Placements[0].Crop != image.Rect(1, 0, 2, 1) {
		t.Fatalf("placement %v errors %v", screen.Placements, screen.Errors)
	}
	screen.Write([]byte("\x1b_Ga=d,d=i,i=9,p=7\x1b\\"))
	if len(screen.Placements) != 0 || len(screen.Images) != 1 {
		t.Fatal("placement-only delete lost data")
	}
	screen.Write([]byte("\x1b_Ga=d,d=I,i=9\x1b\\"))
	if len(screen.Images) != 0 {
		t.Fatal("data delete failed")
	}
}

func TestImageVTInline(t *testing.T) {
	screen := New(20, 10)
	screen.Write([]byte("\x1b[3;4H\x1bP0;1;0q\"1;1;20;40#1;2;100;0;0#1!20~-$\x1b\\"))
	if len(screen.Errors) != 0 || len(screen.Placements) != 1 || screen.Placements[0].Protocol != cell.ImageSixel || screen.Placements[0].Rect != image.Rect(3, 2, 5, 4) {
		t.Fatalf("sixel %v errors %v", screen.Placements, screen.Errors)
	}
	screen.Write([]byte("\x1b[3;4HX"))
	if len(screen.Placements) != 0 {
		t.Fatal("text did not erase inline placement")
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 20, 40))); err != nil {
		t.Fatal(err)
	}
	screen.Write([]byte("\x1b[4;5H\x1b]1337;File=inline=1;width=2;height=2;preserveAspectRatio=0;doNotMoveCursor=1:" + base64.StdEncoding.EncodeToString(encoded.Bytes()) + "\a"))
	if len(screen.Errors) != 0 || len(screen.Placements) != 1 || screen.Placements[0].Protocol != cell.ImageITerm || screen.Placements[0].Rect != image.Rect(4, 3, 6, 5) {
		t.Fatalf("inline %v errors %v", screen.Placements, screen.Errors)
	}
}

func TestImageVTMalformed(t *testing.T) {
	for _, sequence := range []string{
		"\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;bad\x1b\\",
		"\x1b_Ga=t,i=1,f=100;not-base64\x1b\\",
		"\x1b_Ga=t,i=1,f=100;AAAA\x1b\\",
		"\x1b_Ga=p,i=123,c=2,r=3\x1b\\",
		"\x1b_Ga=pp,i=1\x1b\\",
		"\x1b_Gm=0;AAAA\x1b\\",
		"\x1bP0;1;0q\"1;1;0;4?\x1b\\",
		"\x1bP0;1;0q!0?\x1b\\",
		"\x1b]1337;File=inline=1;width=2;height=2:bad\a",
	} {
		screen := New(20, 10)
		screen.Write([]byte(sequence))
		if len(screen.Errors) == 0 {
			t.Errorf("accepted malformed %q", sequence)
		}
	}
}

func TestImageVTScreenIsolation(t *testing.T) {
	screen := New(20, 10)
	screen.Write([]byte("\x1b_Ga=t,t=d,f=24,s=1,v=1,i=1;AAAA\x1b\\\x1b_Ga=p,i=1,p=1,c=2,r=2\x1b\\"))
	screen.Write([]byte("\x1b[?1049h"))
	if len(screen.Images) != 0 || len(screen.Placements) != 0 {
		t.Fatal("main graphics leaked into alt screen")
	}
	screen.Write([]byte("\x1b[?1049l"))
	if len(screen.Images) != 1 || len(screen.Placements) != 1 || len(screen.Errors) != 0 {
		t.Fatalf("main graphics lost %+v %v", screen.Images, screen.Errors)
	}
}

func TestImageVTDeleteByCell(t *testing.T) {
	screen := New(20, 10)
	screen.Write([]byte("\x1b_Ga=t,i=9,t=d,f=24,s=1,v=1;AAAA\x1b\\\x1b_Ga=p,i=9,p=1,c=2,r=2,C=1\x1b\\\x1b[3;4H\x1b_Ga=p,i=9,p=2,c=2,r=2,C=1\x1b\\"))
	screen.Write([]byte("\x1b_Ga=d,d=p,x=1,y=1\x1b\\"))
	if len(screen.Placements) != 1 || screen.Placements[0].ID != 2 || len(screen.Images) != 1 {
		t.Fatalf("delete by cell: %+v %+v", screen.Placements, screen.Images)
	}
	screen.Write([]byte("\x1b_Ga=d,d=I,i=9,p=7\x1b\\"))
	if len(screen.Images) != 1 {
		t.Fatal("unused placement delete freed a referenced image")
	}
	screen.Write([]byte("\x1b_Ga=d,d=P,x=4,y=3\x1b\\"))
	if len(screen.Placements) != 0 || len(screen.Images) != 0 || len(screen.Errors) != 0 {
		t.Fatalf("delete data by cell: %+v %+v %v", screen.Placements, screen.Images, screen.Errors)
	}
}
