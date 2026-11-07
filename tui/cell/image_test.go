package cell_test

import (
	"bytes"
	"image"
	"image/color"
	"math/rand/v2"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/vt"
)

func testImage() *cell.Image {
	pixels := image.NewNRGBA(image.Rect(0, 0, 40, 80))
	for row := 0; row < 80; row++ {
		for col := 0; col < 40; col++ {
			pixels.SetNRGBA(col, row, color.NRGBA{R: uint8(col * 6), G: uint8(row * 3), B: 127, A: 255})
		}
	}
	return cell.NewImage(pixels)
}

func assertImageScreen(t *testing.T, screen *vt.Screen, buffer *cell.Buffer, protocol cell.ImageProtocol) {
	t.Helper()
	if len(screen.Errors) != 0 {
		t.Fatalf("VT errors: %v", screen.Errors)
	}
	if len(screen.Placements) != len(buffer.Images()) {
		t.Fatalf("placements: got %+v want %+v", screen.Placements, buffer.Images())
	}
	for _, wanted := range buffer.Images() {
		found := false
		for _, actual := range screen.Placements {
			if actual.Rect == wanted.Rect && actual.Protocol == protocol {
				if protocol == cell.ImageKitty && (actual.ImageID != wanted.Image.ID || actual.ID != wanted.ID || actual.Crop != wanted.Crop.Sub(wanted.Image.Pixels.Bounds().Min)) {
					continue
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %+v in %+v", wanted, screen.Placements)
		}
	}
	for row := 0; row < buffer.Height(); row++ {
		for col := 0; col < buffer.Width(); col++ {
			wanted := buffer.At(col, row)
			if !wanted.Reserved && screen.Cell(col, row) != wanted {
				t.Fatalf("cell %d,%d: got %+v want %+v", col, row, screen.Cell(col, row), wanted)
			}
		}
	}
}

func TestImageKittyLifecycle(t *testing.T) {
	buffer := cell.NewBuffer(12, 6)
	renderer := cell.NewRenderer(12, 6, cell.Options{Images: cell.ImageKitty, Colors: cell.ColorTrue})
	screen := vt.New(12, 6)
	screen.Autowrap = false
	source := testImage()
	buffer.PlaceImage(source, image.Rect(1, 1, 5, 4), image.Rect(0, 0, 40, 60))
	first := bytes.Clone(renderer.Render(buffer, cell.Cursor{}))
	screen.Write(first)
	if bytes.Count(first, []byte("a=t,")) != 1 || bytes.Count(first, []byte("a=p,")) != 1 {
		t.Fatalf("initial output %q", first)
	}
	assertImageScreen(t, screen, buffer, cell.ImageKitty)
	if output := renderer.Render(buffer, cell.Cursor{}); len(output) != 0 {
		t.Fatalf("unchanged output %q", output)
	}
	buffer.Clear()
	buffer.PlaceImage(source, image.Rect(3, 0, 7, 3), image.Rect(5, 10, 35, 70))
	moved := bytes.Clone(renderer.Render(buffer, cell.Cursor{}))
	screen.Write(moved)
	if bytes.Contains(moved, []byte("a=t,")) || bytes.Count(moved, []byte("a=p,")) != 1 || !bytes.Contains(moved, []byte("a=d,d=i,")) {
		t.Fatalf("move %q", moved)
	}
	assertImageScreen(t, screen, buffer, cell.ImageKitty)
	renderer.Invalidate()
	repaint := bytes.Clone(renderer.Render(buffer, cell.Cursor{}))
	screen.Write(repaint)
	if !bytes.Contains(repaint, []byte("a=p,")) {
		t.Fatal("Invalidate did not place")
	}
	assertImageScreen(t, screen, buffer, cell.ImageKitty)
	buffer.Clear()
	removed := bytes.Clone(renderer.Render(buffer, cell.Cursor{}))
	screen.Write(removed)
	if !bytes.Contains(removed, []byte("a=d,d=i,")) {
		t.Fatal("missing placement delete")
	}
	assertImageScreen(t, screen, buffer, cell.ImageKitty)
	for frame := 0; frame < 3; frame++ {
		screen.Write(renderer.Render(buffer, cell.Cursor{}))
	}
	if len(screen.Images) != 0 {
		t.Fatal("unused data was not freed")
	}
}

func TestImageKittyVersionAndChunks(t *testing.T) {
	buffer := cell.NewBuffer(12, 6)
	renderer := cell.NewRenderer(12, 6, cell.Options{Images: cell.ImageKitty})
	screen := vt.New(12, 6)
	screen.Autowrap = false
	source := testImage()
	pixels := image.NewNRGBA(image.Rect(0, 0, 120, 120))
	random := rand.New(rand.NewPCG(2, 3))
	for index := range pixels.Pix {
		pixels.Pix[index] = uint8(random.IntN(256))
	}
	source.SetPixels(pixels)
	buffer.PlaceImage(source, image.Rect(0, 0, 3, 2), image.Rectangle{})
	buffer.PlaceImage(source, image.Rect(5, 1, 8, 3), image.Rectangle{})
	output := bytes.Clone(renderer.Render(buffer, cell.Cursor{}))
	screen.Write(output)
	if bytes.Count(output, []byte("\x1b_Gq=2,m=")) < 1 || bytes.Count(output, []byte("a=t,")) != 1 {
		t.Fatal("did not chunk/transmit once")
	}
	assertImageScreen(t, screen, buffer, cell.ImageKitty)
	source.SetPixels(testImage().Pixels)
	buffer.Clear()
	buffer.PlaceImage(source, image.Rect(0, 0, 3, 2), image.Rectangle{})
	output = bytes.Clone(renderer.Render(buffer, cell.Cursor{}))
	screen.Write(output)
	if !bytes.Contains(output, []byte("a=t,")) {
		t.Fatal("version change reused stale pixels")
	}
	assertImageScreen(t, screen, buffer, cell.ImageKitty)
}

func TestImageInlineProtocols(t *testing.T) {
	for _, protocol := range []cell.ImageProtocol{cell.ImageSixel, cell.ImageITerm} {
		t.Run(protocol.String(), func(t *testing.T) {
			buffer := cell.NewBuffer(12, 6)
			renderer := cell.NewRenderer(12, 6, cell.Options{Images: protocol, Colors: cell.ColorTrue, CellWidth: 2, CellHeight: 4})
			screen := vt.New(12, 6)
			screen.Autowrap = false
			screen.CellWidth, screen.CellHeight = 2, 4
			source := testImage()
			for frame := 0; frame < 5; frame++ {
				buffer.Clear()
				if frame != 4 {
					buffer.PlaceImage(source, image.Rect(2+frame, 1, 5+frame, 3), image.Rect(4, 8, 16, 40))
				}
				buffer.Set(0, 0, "a", 1, cell.Style{})
				screen.Write(renderer.Render(buffer, cell.Cursor{}))
				assertImageScreen(t, screen, buffer, protocol)
				if frame == 0 {
					if output := renderer.Render(buffer, cell.Cursor{}); len(output) != 0 {
						t.Fatalf("steady output %q", output)
					}
					buffer.Set(11, 0, "b", 1, cell.Style{})
					output := bytes.Clone(renderer.Render(buffer, cell.Cursor{}))
					screen.Write(output)
					if bytes.Contains(output, []byte("\x1bP")) || bytes.Contains(output, []byte("1337;File=")) {
						t.Fatal("unrelated text retransmitted image")
					}
				}
				if frame == 2 {
					renderer.Invalidate()
					screen.Write(renderer.Render(buffer, cell.Cursor{}))
					assertImageScreen(t, screen, buffer, protocol)
				}
			}
		})
	}
}

func TestImageRandomFrames(t *testing.T) {
	for _, protocol := range []cell.ImageProtocol{cell.ImageKitty, cell.ImageSixel, cell.ImageITerm} {
		t.Run(protocol.String(), func(t *testing.T) {
			buffer := cell.NewBuffer(18, 8)
			source := testImage()
			renderer := cell.NewRenderer(18, 8, cell.Options{Images: protocol, Colors: cell.ColorTrue, CellWidth: 2, CellHeight: 2})
			screen := vt.New(18, 8)
			screen.Autowrap = false
			screen.CellWidth, screen.CellHeight = 2, 2
			random := rand.New(rand.NewPCG(41, 87))
			for frame := 0; frame < 150; frame++ {
				buffer.Clear()
				if frame%5 != 4 {
					buffer.PlaceImage(source, image.Rect(16, 0, 18, 1), image.Rect(0, 0, 40, 40))
				}
				for index := 0; index < 3; index++ {
					if random.IntN(3) == 0 {
						continue
					}
					left, top := random.IntN(15)-2, random.IntN(7)-1
					buffer.PlaceImage(source, image.Rect(left, top, left+4, top+3), image.Rect(2, 4, 38, 76))
				}
				buffer.Set(random.IntN(18), random.IntN(8), "X", 1, cell.Style{})
				if frame%11 == 0 {
					renderer.Invalidate()
				}
				screen.Write(renderer.Render(buffer, cell.Cursor{}))
				assertImageScreen(t, screen, buffer, protocol)
			}
		})
	}
}

func TestImageInlineRetainedPlacement(t *testing.T) {
	for _, protocol := range []cell.ImageProtocol{cell.ImageSixel, cell.ImageITerm} {
		buffer := cell.NewBuffer(12, 6)
		source := testImage()
		renderer := cell.NewRenderer(12, 6, cell.Options{Images: protocol})
		screen := vt.New(12, 6)
		screen.Autowrap = false
		buffer.PlaceImage(source, image.Rect(1, 1, 4, 3), image.Rectangle{})
		screen.Write(renderer.Render(buffer, cell.Cursor{}))
		buffer.Clear()
		buffer.PlaceImage(source, image.Rect(1, 1, 4, 3), image.Rectangle{})
		buffer.PlaceImage(source, image.Rect(6, 1, 9, 3), image.Rectangle{})
		screen.Write(renderer.Render(buffer, cell.Cursor{}))
		assertImageScreen(t, screen, buffer, protocol)
	}
}

func TestImageRasterAreaAverage(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(7, 9, 9, 11))
	pixels.SetNRGBA(7, 9, color.NRGBA{R: 255, A: 255})
	pixels.SetNRGBA(8, 9, color.NRGBA{G: 255, A: 255})
	pixels.SetNRGBA(7, 10, color.NRGBA{B: 255, A: 255})
	pixels.SetNRGBA(8, 10, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	got := cell.NewImage(pixels).Raster(pixels.Bounds(), 1, 1).NRGBAAt(0, 0)
	if got != (color.NRGBA{R: 128, G: 128, B: 128, A: 255}) {
		t.Fatalf("average %v", got)
	}
}

func TestImageBufferReservationsAndOverlays(t *testing.T) {
	buffer := cell.NewBuffer(8, 4)
	source := testImage()
	buffer.Set(0, 1, "界", 2, cell.Style{})
	buffer.PlaceImage(source, image.Rect(1, 0, 7, 4), image.Rectangle{})
	if buffer.At(0, 1).Text != " " || buffer.At(0, 1).Width != 1 {
		t.Fatal("image did not repair wide edge")
	}
	for row := 0; row < 4; row++ {
		for col := 1; col < 7; col++ {
			if !buffer.At(col, row).Reserved {
				t.Fatal("unreserved image cell")
			}
		}
	}
	buffer.Set(3, 1, "界", 2, cell.Style{})
	if buffer.At(3, 1).Reserved || buffer.At(4, 1).Reserved {
		t.Fatal("text did not release image cells")
	}
	for _, placement := range buffer.Images() {
		if placement.Rect.Overlaps(image.Rect(3, 1, 5, 2)) {
			t.Fatalf("image overlays text: %+v", placement)
		}
	}
	for _, protocol := range []cell.ImageProtocol{cell.ImageKitty, cell.ImageSixel, cell.ImageITerm} {
		screen := vt.New(8, 4)
		screen.Autowrap = false
		renderer := cell.NewRenderer(8, 4, cell.Options{Images: protocol})
		screen.Write(renderer.Render(buffer, cell.Cursor{}))
		assertImageScreen(t, screen, buffer, protocol)
	}
	buffer.Clear()
	if len(buffer.Images()) != 0 || buffer.At(1, 1).Reserved {
		t.Fatal("Clear did not release image cells")
	}
}

func TestImageResetFreesKitty(t *testing.T) {
	buffer := cell.NewBuffer(8, 4)
	buffer.PlaceImage(testImage(), image.Rect(0, 0, 4, 2), image.Rectangle{})
	renderer := cell.NewRenderer(8, 4, cell.Options{Images: cell.ImageKitty})
	screen := vt.New(8, 4)
	screen.Write(renderer.Render(buffer, cell.Cursor{}))
	screen.Write(renderer.Reset())
	if len(screen.Images) != 0 || len(screen.Placements) != 0 || len(screen.Errors) != 0 {
		t.Fatalf("reset retained graphics: %+v %+v %v", screen.Images, screen.Placements, screen.Errors)
	}
}

func TestImageReservedErasesStaleText(t *testing.T) {
	for _, protocol := range []cell.ImageProtocol{cell.ImageKitty, cell.ImageSixel, cell.ImageITerm} {
		buffer := cell.NewBuffer(8, 4)
		renderer := cell.NewRenderer(8, 4, cell.Options{Images: protocol})
		screen := vt.New(8, 4)
		screen.Autowrap = false
		buffer.Set(2, 1, "X", 1, cell.Style{})
		screen.Write(renderer.Render(buffer, cell.Cursor{}))
		buffer.Clear()
		buffer.PlaceImage(testImage(), image.Rect(1, 0, 5, 3), image.Rectangle{})
		screen.Write(renderer.Render(buffer, cell.Cursor{}))
		if screen.Cell(2, 1).Text != " " {
			t.Fatalf("%v left stale text behind reserved image cells", protocol)
		}
	}
}

func BenchmarkImageSteadyFrame(benchmark *testing.B) {
	buffer := cell.NewBuffer(120, 40)
	source := testImage()
	renderer := cell.NewRenderer(120, 40, cell.Options{Images: cell.ImageKitty})
	buffer.PlaceImage(source, image.Rect(4, 2, 50, 25), image.Rectangle{})
	renderer.Render(buffer, cell.Cursor{})
	benchmark.ReportAllocs()
	benchmark.ResetTimer()
	for index := 0; index < benchmark.N; index++ {
		renderer.Render(buffer, cell.Cursor{})
	}
}

func TestImageSteadyAllocations(t *testing.T) {
	for _, protocol := range []cell.ImageProtocol{cell.ImageNone, cell.ImageKitty, cell.ImageSixel, cell.ImageITerm} {
		buffer := cell.NewBuffer(20, 8)
		if protocol != cell.ImageNone {
			buffer.PlaceImage(testImage(), image.Rect(2, 1, 6, 5), image.Rectangle{})
		}
		renderer := cell.NewRenderer(20, 8, cell.Options{Images: protocol})
		renderer.Render(buffer, cell.Cursor{})
		if allocations := testing.AllocsPerRun(50, func() { renderer.Render(buffer, cell.Cursor{}) }); allocations != 0 {
			t.Fatalf("protocol %v steady allocations %v", protocol, allocations)
		}
	}
}
