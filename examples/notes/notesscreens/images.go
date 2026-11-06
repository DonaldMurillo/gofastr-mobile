package notesscreens

import (
	"context"
	"errors"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extensions/media"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func mediaComponents(images []nativeui.Image, attachments []nativeui.Attachment, preview *nativeui.FilePreview) ([]extension.Component, error) {
	if len(images)+len(attachments) == 0 && preview == nil {
		return nil, nil
	}
	component, err := media.Block("note-media", media.Content{Images: images, Attachments: attachments, Preview: preview})
	if err != nil {
		return nil, err
	}
	return []extension.Component{component}, nil
}

func noteMedia(ctx context.Context, store *files.Store, noteID string) ([]nativeui.Image, []nativeui.Attachment, error) {
	if store == nil || noteID == "" {
		return nil, nil, nil
	}
	items, err := store.List(ctx, noteID)
	if err != nil {
		return nil, nil, err
	}
	var images []nativeui.Image
	var attachments []nativeui.Attachment
	for _, item := range items {
		item, err = recordingMetadata(ctx, store, noteID, item)
		if err != nil {
			return nil, nil, err
		}
		if item.MediaType != "image/jpeg" && item.MediaType != "image/png" {
			attachments = append(attachments, noteAttachment(noteID, item))
			continue
		}
		document, err := store.Read(ctx, noteID, item.Handle)
		if errors.Is(err, files.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		preview, err := nativeui.NewImage(item.Handle, item.Name, document.Data, 512)
		if err != nil {
			attachments = append(attachments, noteAttachment(noteID, item))
			continue
		}
		preview.Destination = attachmentRoute(noteID, item.Handle)
		images = append(images, preview)
	}
	return images, attachments, nil
}

func noteAttachment(noteID string, item files.File) nativeui.Attachment {
	return nativeui.Attachment{ID: item.Handle, Name: item.Name, MediaType: item.MediaType, Size: item.Size, Destination: attachmentRoute(noteID, item.Handle)}
}
