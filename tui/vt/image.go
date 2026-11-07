package vt

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/png"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
)

type ImageData struct {
	Width, Height int
	Data          []byte
}
type ImagePlacement struct {
	Protocol    cell.ImageProtocol
	ImageID, ID uint32
	Rect, Crop  image.Rectangle
}

func (s *Screen) graphics(body string) {
	if !strings.HasPrefix(body, "G") {
		s.errorf("malformed graphics APC")
		return
	}
	header, payload, _ := strings.Cut(body[1:], ";")
	control := make(map[string]int)
	action, deletion := byte('t'), byte('a')
	for field := range strings.SplitSeq(header, ",") {
		key, value, ok := strings.Cut(field, "=")
		if !ok || len(key) != 1 {
			s.errorf("malformed graphics control")
			return
		}
		switch key {
		case "a":
			if len(value) != 1 {
				s.errorf("bad graphics action")
				return
			}
			action = value[0]
		case "d":
			if len(value) != 1 {
				s.errorf("bad graphics deletion")
				return
			}
			deletion = value[0]
		case "t":
			if value != "d" {
				s.errorf("unsupported graphics medium")
				return
			}
		default:
			number, err := strconv.ParseInt(value, 10, 64)
			if err != nil || number < -2147483648 || number > 4294967295 {
				s.errorf("bad graphics number")
				return
			}
			control[key] = int(number)
		}
	}
	if action == 'q' {
		data, err := base64.StdEncoding.DecodeString(payload)
		if control["i"] <= 0 || err != nil || control["f"] != 24 || len(data) != 3*control["s"]*control["v"] {
			s.errorf("bad graphics query")
			return
		}
		s.Replies = []byte(string(s.Replies) + "\x1b_Gi=" + strconv.Itoa(control["i"]) + ";OK\x1b\\")
		return
	}
	if action == 'd' {
		if !strings.ContainsRune("aAiIpP", rune(deletion)) {
			s.errorf("unsupported graphics deletion")
			return
		}
		if (deletion == 'p' || deletion == 'P') && (control["x"] <= 0 || control["y"] <= 0) {
			s.errorf("invalid graphics deletion coordinates")
			return
		}
		freed := make(map[uint32]bool)
		kept := s.Placements[:0]
		for _, placement := range s.Placements {
			match := false
			switch deletion {
			case 'a', 'A':
				match = true
			case 'i', 'I':
				match = int(placement.ImageID) == control["i"] && (control["p"] == 0 || int(placement.ID) == control["p"])
			case 'p', 'P':
				match = image.Pt(control["x"]-1, control["y"]-1).In(placement.Rect)
			}
			if !match {
				kept = append(kept, placement)
			} else if deletion >= 'A' && deletion <= 'Z' {
				freed[placement.ImageID] = true
			}
		}
		s.Placements = kept
		if deletion == 'I' && control["p"] == 0 {
			freed[uint32(control["i"])] = true
		}
		for _, placement := range kept {
			delete(freed, placement.ImageID)
		}
		for id := range freed {
			delete(s.Images, id)
		}
		if s.graphicsControl != nil {
			s.graphicsChunk, s.graphicsControl = s.graphicsChunk[:0], nil
		}
		return
	}
	if action == 't' || action == 'T' {
		if control["i"] != 0 {
			if len(s.graphicsChunk) != 0 {
				s.errorf("interrupted graphics transmission")
			}
			s.graphicsControl = control
			s.graphicsAction = action
			s.graphicsChunk = s.graphicsChunk[:0]
		} else if s.graphicsControl == nil {
			s.errorf("graphics chunk without image ID")
			return
		}
		s.graphicsChunk = append(s.graphicsChunk, payload...)
		if control["m"] == 1 {
			return
		}
		control = s.graphicsControl
		action = s.graphicsAction
		data, err := base64.StdEncoding.DecodeString(string(s.graphicsChunk))
		s.graphicsChunk, s.graphicsControl = s.graphicsChunk[:0], nil
		if err != nil {
			s.errorf("bad graphics base64")
			return
		}
		width, height := control["s"], control["v"]
		switch control["f"] {
		case 100:
			config, _, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil {
				s.errorf("bad graphics PNG")
				return
			}
			width, height = config.Width, config.Height
		case 24, 32:
			if width <= 0 || height <= 0 || width*height*(control["f"]/8) != len(data) {
				s.errorf("bad graphics pixels")
				return
			}
		default:
			s.errorf("unsupported graphics format")
			return
		}
		s.Images[uint32(control["i"])] = ImageData{width, height, data}
		if action != 'T' {
			return
		}
	}
	if action != 'p' && action != 'T' {
		s.errorf("unsupported graphics action")
		return
	}
	img, ok := s.Images[uint32(control["i"])]
	if !ok || control["c"] <= 0 || control["r"] <= 0 {
		s.errorf("invalid graphics placement")
		return
	}
	crop := image.Rect(control["x"], control["y"], control["x"]+control["w"], control["y"]+control["h"])
	if control["w"] == 0 {
		crop.Max.X = img.Width
	}
	if control["h"] == 0 {
		crop.Max.Y = img.Height
	}
	if crop.Empty() || crop != crop.Intersect(image.Rect(0, 0, img.Width, img.Height)) {
		s.errorf("invalid graphics crop")
		return
	}
	placement := ImagePlacement{Protocol: cell.ImageKitty, ImageID: uint32(control["i"]), ID: uint32(control["p"]), Rect: image.Rect(s.X, s.Y, s.X+control["c"], s.Y+control["r"]), Crop: crop}
	for index, old := range s.Placements {
		if old.ImageID == placement.ImageID && old.ID == placement.ID {
			s.Placements[index] = placement
			return
		}
	}
	s.Placements = append(s.Placements, placement)
}

func (s *Screen) removeInline(rect image.Rectangle) {
	kept := s.Placements[:0]
	for _, placement := range s.Placements {
		if placement.Protocol == cell.ImageKitty || !placement.Rect.Overlaps(rect) {
			kept = append(kept, placement)
		}
	}
	s.Placements = kept
}

func (s *Screen) addInline(protocol cell.ImageProtocol, width, height, pixelWidth, pixelHeight int) {
	s.inlineID++
	s.Placements = append(s.Placements, ImagePlacement{Protocol: protocol, ID: s.inlineID, Rect: image.Rect(s.X, s.Y, s.X+width, s.Y+height), Crop: image.Rect(0, 0, pixelWidth, pixelHeight)})
}

func (s *Screen) inlineImage(body string) {
	header, payload, ok := strings.Cut(strings.TrimPrefix(body, "1337;File="), ":")
	values := make(map[string]string)
	for field := range strings.SplitSeq(header, ";") {
		key, value, found := strings.Cut(field, "=")
		if !found {
			s.errorf("malformed inline image option")
			return
		}
		values[key] = value
	}
	width, errW := strconv.Atoi(values["width"])
	height, errH := strconv.Atoi(values["height"])
	data, err := base64.StdEncoding.DecodeString(payload)
	config, _, decodeErr := image.DecodeConfig(bytes.NewReader(data))
	if !ok || values["inline"] != "1" || errW != nil || errH != nil || width <= 0 || height <= 0 || err != nil || decodeErr != nil {
		s.errorf("malformed inline image")
		return
	}
	s.addInline(cell.ImageITerm, width, height, config.Width, config.Height)
}

func (s *Screen) sixel(body string) {
	header, data, found := strings.Cut(body, "q")
	if !found {
		return
	}
	for _, character := range header {
		if character != ';' && (character < '0' || character > '9') {
			return
		}
	}
	width, height, col, band, maxCol, maxRow := 0, 0, 0, 0, 0, 0
	for offset := 0; offset < len(data); {
		character := data[offset]
		offset++
		switch {
		case character >= '?' && character <= '~':
			col++
			maxCol = max(maxCol, col)
			maxRow = max(maxRow, band+6)
		case character == '$':
			col = 0
		case character == '-':
			band += 6
			col = 0
		case character == '"' || character == '#':
			start := offset
			for offset < len(data) && (data[offset] >= '0' && data[offset] <= '9' || data[offset] == ';') {
				offset++
			}
			fields := strings.Split(data[start:offset], ";")
			for _, field := range fields {
				if _, err := strconv.Atoi(field); err != nil {
					s.errorf("bad sixel number")
					return
				}
			}
			if character == '"' {
				if len(fields) != 4 {
					s.errorf("bad sixel raster")
					return
				}
				width, _ = strconv.Atoi(fields[2])
				height, _ = strconv.Atoi(fields[3])
				if width <= 0 || height <= 0 {
					s.errorf("bad sixel dimensions")
					return
				}
			} else if len(fields) != 1 && len(fields) != 5 {
				s.errorf("bad sixel palette")
				return
			}
		case character == '!':
			start := offset
			for offset < len(data) && data[offset] >= '0' && data[offset] <= '9' {
				offset++
			}
			repeat, err := strconv.Atoi(data[start:offset])
			if err != nil || repeat <= 0 || repeat > 1<<20 || offset >= len(data) || data[offset] < '?' || data[offset] > '~' {
				s.errorf("bad sixel repeat")
				return
			}
			offset++
			col += repeat
			maxCol = max(maxCol, col)
			maxRow = max(maxRow, band+6)
		default:
			s.errorf("bad sixel command")
			return
		}
	}
	if width == 0 {
		width, height = maxCol, maxRow
	}
	if width <= 0 || height <= 0 {
		s.errorf("empty sixel")
		return
	}
	cellWidth, cellHeight := (cell.Options{CellWidth: s.CellWidth, CellHeight: s.CellHeight}).CellPixels()
	s.addInline(cell.ImageSixel, (width+cellWidth-1)/cellWidth, (height+cellHeight-1)/cellHeight, width, height)
}
