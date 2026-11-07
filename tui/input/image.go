package input

import (
	"strconv"
	"strings"
)

// GraphicsReply is a kitty graphics query response, including error responses.
type GraphicsReply struct {
	ID      uint32
	Message string
}

// CellSize is the pixel size reported by CSI 16 t, with width before height.
type CellSize struct{ Width, Height int }

func (GraphicsReply) isEvent() {}
func (CellSize) isEvent()      {}

func graphicsEvent(body []byte) Event {
	control, message, found := strings.Cut(string(body), ";")
	if !found || !strings.HasPrefix(control, "G") {
		return nil
	}
	for field := range strings.SplitSeq(control[1:], ",") {
		if value, ok := strings.CutPrefix(field, "i="); ok {
			id, err := strconv.ParseUint(value, 10, 32)
			if err == nil && id != 0 {
				return GraphicsReply{ID: uint32(id), Message: message}
			}
		}
	}
	return nil
}
