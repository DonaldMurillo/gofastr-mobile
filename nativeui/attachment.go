package nativeui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-mobile/device/audio"
	"github.com/DonaldMurillo/gofastr-mobile/device/files"
)

// Attachment presents file metadata and a registered detail destination, not
// bytes or a device path. Renderers supply accessible, full-row file controls.
type Attachment struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	MediaType   string          `json:"mediaType"`
	Size        int             `json:"size"`
	Destination string          `json:"destination"`
	Audio       *audio.Document `json:"audio,omitempty"`
}

// FilePreview presents authorized file metadata and an optional bounded plain-text preview.
type FilePreview struct {
	File         Attachment `json:"file"`
	Caption      string     `json:"caption,omitempty"`
	PreviewTitle string     `json:"previewTitle,omitempty"`
	Text         string     `json:"text,omitempty"`
}

func validateFilePreview(preview FilePreview) error {
	if len(preview.Text) > 16384 || !utf8.ValidString(preview.Text) || strings.ContainsRune(preview.Text, '\x00') || len(preview.Caption) > 4096 || len(preview.PreviewTitle) > 200 {
		return errors.New("nativeui: invalid file preview")
	}
	if preview.File.Audio != nil {
		return errors.New("nativeui: file preview metadata must not contain audio bytes")
	}
	return validateAttachments([]Attachment{preview.File})
}

// FileDescription returns a readable file category and size, without exposing MIME syntax.
func FileDescription(mediaType string, size int) string {
	category := "File"
	switch {
	case strings.HasPrefix(mediaType, "image/"):
		category = "Photo"
	case strings.HasPrefix(mediaType, "audio/"):
		category = "Audio"
	case mediaType == "application/pdf":
		category = "PDF document"
	case mediaType == "text/markdown" || mediaType == "text/x-markdown":
		category = "Markdown"
	case strings.HasPrefix(mediaType, "text/"):
		category = "Text document"
	}
	display := fmt.Sprintf("%d B", size)
	if size >= 1<<20 {
		display = fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	} else if size >= 1<<10 {
		display = fmt.Sprintf("%.0f KB", float64(size)/(1<<10))
	}
	return category + " · " + display
}

// FileIcon returns the renderer-independent file category icon.
func FileIcon(mediaType string) Icon {
	if strings.HasPrefix(mediaType, "image/") {
		return IconPhoto
	}
	if strings.HasPrefix(mediaType, "audio/") {
		return IconAudio
	}
	return IconFile
}

// MarshalJSON includes derived presentation metadata for both renderer adapters.
func (attachment Attachment) MarshalJSON() ([]byte, error) {
	type payload Attachment
	return json.Marshal(struct {
		payload
		Detail string `json:"detail"`
		Icon   Icon   `json:"icon"`
	}{payload(attachment), FileDescription(attachment.MediaType, attachment.Size), FileIcon(attachment.MediaType)})
}

func validateAttachments(attachments []Attachment) error {
	if len(attachments) > 64 {
		return errors.New("nativeui: at most 64 file attachments can be displayed")
	}
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		metadata := files.Document{Name: attachment.Name, MediaType: attachment.MediaType}
		if attachment.ID == "" || len(attachment.ID) > 128 || seen[attachment.ID] || attachment.Size < 0 || attachment.Size > files.MaxBytes || metadata.Validate() != nil {
			return errors.New("nativeui: invalid or duplicate file attachment")
		}
		seen[attachment.ID] = true
		if attachment.Audio != nil {
			if err := attachment.Audio.Validate(); err != nil {
				return err
			}
			file := attachment.Audio.File
			if file.Name != attachment.Name || file.MediaType != attachment.MediaType || len(file.Data) != attachment.Size {
				return errors.New("nativeui: audio playback metadata must match its attachment")
			}
		}
	}
	return nil
}
