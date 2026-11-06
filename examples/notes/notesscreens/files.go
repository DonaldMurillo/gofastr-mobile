package notesscreens

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/extensions/photos"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func fileRoutes(service *notes.Service, store *files.Store) []nativeui.Route {
	routes := []nativeui.Route{{Path: "/files", Screen: func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
		list := &nativeui.List{Header: "From recent notes", Footer: "Private attachments · Stored on this device", Empty: "No attachments yet.\nOpen a saved note and choose Add file, or record audio inside it.", Rows: []nativeui.Row{}}
		if store != nil {
			items, err := service.List(ctx, "")
			if err != nil {
				return nativeui.Screen{}, presentationError(err)
			}
			for _, note := range items {
				attachments, err := store.List(ctx, note.ID)
				if err != nil {
					return nativeui.Screen{}, err
				}
				for _, item := range attachments {
					list.Rows = append(list.Rows, nativeui.Row{ID: item.Handle, Title: item.Name, Subtitle: nativeui.FileDescription(item.MediaType, item.Size), Metadata: note.Title, Icon: nativeui.FileIcon(item.MediaType), Destination: attachmentRoute(note.ID, item.Handle)})
				}
			}
		}
		return nativeui.Screen{Title: "Files", LargeTitle: true, List: list,
			Actions: []nativeui.Action{{ID: "import", Label: "Import note", Primary: true}}}, nil
	}, Actions: map[string]nativeui.Handle{"import": func(context.Context, nativeui.Request) (nativeui.Result, error) {
		return nativeui.Result{Files: &nativeui.FileRequest{ID: "import", Commit: true, Pick: &files.Pick{Title: "Import note", Extensions: []string{".txt", ".md"}, MaxBytes: files.MaxBytes}}}, nil
	}}, Files: map[string]nativeui.FileRespond{"import": func(ctx context.Context, _ nativeui.Request, result nativeui.FileResult) (nativeui.Result, error) {
		title, body, err := importedNote(*result.File)
		if err != nil {
			return nativeui.Result{}, err
		}
		note, err := service.Save(ctx, "", title, body)
		if err != nil {
			return nativeui.Result{}, presentationError(err)
		}
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: "/notes/" + url.PathEscape(note.ID)}, Announcement: "Note imported", Haptic: "success"}, nil
	}}}}
	routes = append(routes, nativeui.Route{Path: "/notes/{id}/files", Screen: func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
		if _, err := service.Get(ctx, request.Params["id"]); err != nil {
			return nativeui.Screen{}, presentationError(err)
		}
		if store == nil {
			return nativeui.Screen{Title: "Attachments", Text: "Attachment storage is not configured in this host."}, nil
		}
		items, err := store.List(ctx, request.Params["id"])
		if err != nil {
			return nativeui.Screen{}, err
		}
		list := &nativeui.List{Header: "In this note", Footer: "Private · Stored on this device", Empty: "Keep everything together.\nAdd a file, photo or voice recording to this note."}
		for _, item := range items {
			item, err = recordingMetadata(ctx, store, request.Params["id"], item)
			if err != nil {
				return nativeui.Screen{}, err
			}
			list.Rows = append(list.Rows, nativeui.Row{ID: item.Handle, Title: item.Name, Subtitle: nativeui.FileDescription(item.MediaType, item.Size), Icon: nativeui.FileIcon(item.MediaType), Destination: attachmentRoute(request.Params["id"], item.Handle)})
		}
		return nativeui.Screen{Title: "Attachments", List: list, Actions: []nativeui.Action{{ID: "attach", Label: "Add file", Primary: true}, {ID: "photo", Label: "Choose photo"}, {ID: "camera", Label: "Take photo"}, {ID: "record", Label: "Record audio"}}}, nil
	}, Actions: map[string]nativeui.Handle{"attach": requestAttachment(service, store), "photo": requestPhoto(service, store, photos.Library), "camera": requestPhoto(service, store, photos.Camera), "record": requestRecording(service, store)}, Services: editorServices(service, store), Files: map[string]nativeui.FileRespond{
		"attach": attachDocument(service, store), "photo": attachDocument(service, store), "camera": attachDocument(service, store),
	}})
	loadStored := func(ctx context.Context, request nativeui.Request) (files.Document, error) {
		if _, err := service.Get(ctx, request.Params["id"]); err != nil {
			return files.Document{}, presentationError(err)
		}
		if store == nil {
			return files.Document{}, fileError("Attachment storage is unavailable.")
		}
		document, err := store.Read(ctx, request.Params["id"], request.Params["handle"])
		if errors.Is(err, files.ErrNotFound) {
			return files.Document{}, &nativeui.ActionError{Code: "not_found", Message: "This attachment is no longer available."}
		}
		return document, err
	}
	load := func(ctx context.Context, request nativeui.Request) (files.Document, error) {
		document, err := loadStored(ctx, request)
		if err != nil {
			return files.Document{}, err
		}
		document, _, err = playbackDocument(document)
		return document, err
	}
	routes = append(routes, nativeui.Route{Path: "/notes/{id}/files/{handle}", Screen: func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
		document, err := loadStored(ctx, request)
		if err != nil {
			return nativeui.Screen{}, err
		}
		document, playback, err := playbackDocument(document)
		if err != nil {
			return nativeui.Screen{}, err
		}
		var images []nativeui.Image
		if document.MediaType == "image/jpeg" || document.MediaType == "image/png" {
			if preview, err := nativeui.NewImage(request.Params["handle"], document.Name, document.Data, 1024); err == nil {
				images = append(images, preview)
			}
		}
		filePreview := &nativeui.FilePreview{File: nativeui.Attachment{ID: request.Params["handle"], Name: document.Name, MediaType: document.MediaType, Size: len(document.Data), Destination: attachmentRoute(request.Params["id"], request.Params["handle"])}, Caption: "Private attachment stored on this device"}
		if strings.HasPrefix(document.MediaType, "text/") && utf8.Valid(document.Data) && !strings.ContainsRune(string(document.Data), '\x00') {
			preview := document.Data
			filePreview.PreviewTitle = "Text preview"
			if len(preview) > 16384 {
				preview = preview[:16384]
				for !utf8.Valid(preview) {
					preview = preview[:len(preview)-1]
				}
				filePreview.PreviewTitle = "Text preview (first 16 KiB)"
			}
			filePreview.Text = string(preview)
		}
		var attachments []nativeui.Attachment
		if playback != nil {
			attachments = append(attachments, nativeui.Attachment{ID: request.Params["handle"], Name: document.Name, MediaType: document.MediaType, Size: len(document.Data), Destination: attachmentRoute(request.Params["id"], request.Params["handle"]), Audio: playback})
			filePreview.Caption += fmt.Sprintf(" · %.1f seconds", float64(playback.DurationMillis)/1000)
		}
		components, err := mediaComponents(images, attachments, filePreview)
		if err != nil {
			return nativeui.Screen{}, err
		}
		return nativeui.Screen{Title: "Attachment", Components: components, Actions: []nativeui.Action{{ID: "share", Label: "Share file"}, {ID: "export", Label: "Export file", Primary: true}, {ID: "remove", Label: "Remove"}}}, nil
	}, Actions: map[string]nativeui.Handle{
		"share": requestAttachmentShare(load),
		"export": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
			document, err := load(ctx, request)
			if err != nil {
				return nativeui.Result{}, err
			}
			return nativeui.Result{Files: &nativeui.FileRequest{ID: "export", Export: &document}}, nil
		},
		"remove": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
			if _, err := load(ctx, request); err != nil {
				return nativeui.Result{}, err
			}
			return nativeui.Result{Presentation: &nativeui.Presentation{ID: "remove", Kind: nativeui.PresentationConfirmation, Title: "Remove attachment?", Message: "This removes the private copy from Notes, not the original file.", Dismissible: true, Commit: true, Choices: []nativeui.PresentationChoice{{ID: "cancel", Label: "Cancel", Role: "cancel"}, {ID: "remove", Label: "Remove", Role: "destructive"}}}}, nil
		},
	}, Services: map[string]nativeui.ServiceRespond{"share": func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
		if _, err := load(ctx, request); err != nil {
			return nativeui.Result{}, err
		}
		return serviceOutcome("share", outcome), nil
	}}, Presentations: map[string]nativeui.Respond{"remove": func(ctx context.Context, request nativeui.Request, result nativeui.PresentationResult) (nativeui.Result, error) {
		if result.Cancelled || result.Choice != "remove" {
			return nativeui.Result{}, nil
		}
		if _, err := load(ctx, request); err != nil {
			return nativeui.Result{}, err
		}
		if err := store.Delete(ctx, request.Params["id"], request.Params["handle"]); err != nil {
			return nativeui.Result{}, err
		}
		service.AttachmentsChanged(ctx)
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}, Announcement: "Attachment removed"}, nil
	}}})
	return routes
}

func requestAttachment(service *notes.Service, store *files.Store) nativeui.Handle {
	return func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
		if _, err := service.Get(ctx, request.Params["id"]); err != nil {
			return nativeui.Result{}, presentationError(err)
		}
		if store == nil {
			return nativeui.Result{}, fileError("Attachment storage is unavailable.")
		}
		return nativeui.Result{Files: &nativeui.FileRequest{ID: "attach", Commit: true, Pick: &files.Pick{Title: "Add attachment", MaxBytes: files.MaxBytes}}}, nil
	}
}

func requestPhoto(service *notes.Service, store *files.Store, source photos.Source) nativeui.Handle {
	return func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
		if _, err := service.Get(ctx, request.Params["id"]); err != nil {
			return nativeui.Result{}, presentationError(err)
		}
		if store == nil {
			return nativeui.Result{}, fileError("Attachment storage is unavailable.")
		}
		identifier, title := "photo", "Choose photo"
		if source == photos.Camera {
			identifier, title = "camera", "Take photo"
		}
		operation, err := photos.Acquire(photos.Request{
			Title: title, Source: source, MaxBytes: files.MaxBytes, MaxDimension: photos.MaxDimension,
		})
		if err != nil {
			return nativeui.Result{}, err
		}
		return nativeui.Result{Files: &nativeui.FileRequest{ID: identifier, Commit: true, Extension: operation}}, nil
	}
}

func attachDocument(service *notes.Service, store *files.Store) nativeui.FileRespond {
	return func(ctx context.Context, request nativeui.Request, result nativeui.FileResult) (nativeui.Result, error) {
		if _, err := service.Get(ctx, request.Params["id"]); err != nil {
			return nativeui.Result{}, presentationError(err)
		}
		if store == nil {
			return nativeui.Result{}, fileError("Attachment storage is unavailable.")
		}
		item, err := store.Put(ctx, request.Params["id"], *result.File)
		if err != nil {
			return nativeui.Result{}, fileError("Could not store the attachment. Storage may be full.")
		}
		service.AttachmentsChanged(ctx)
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: attachmentRoute(request.Params["id"], item.Handle)}, Announcement: "Attachment added"}, nil
	}
}

func exportNote(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
	document, err := noteDocument(request)
	if err != nil {
		return nativeui.Result{}, err
	}
	return nativeui.Result{Files: &nativeui.FileRequest{ID: "export", Export: &document}}, nil
}

func noteDocument(request nativeui.Request) (files.Document, error) {
	name := strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || strings.ContainsRune(`/\\:*?"<>|`, character) {
			return '-'
		}
		return character
	}, request.Values["title"])
	name = strings.Trim(name, " .")
	if name == "" {
		name = "Note"
	}
	characters := []rune(name)
	if len(characters) > 40 {
		name = string(characters[:40])
	}
	document := files.Document{Name: name + ".md", MediaType: "text/markdown", Data: []byte(request.Values["title"] + "\n\n" + request.Values["body"])}
	if err := document.Validate(); err != nil {
		return files.Document{}, fileError("This note is too large to export. The prototype supports files up to 1 MiB.")
	}
	return document, nil
}

func importedNote(document files.Document) (string, string, error) {
	if !utf8.Valid(document.Data) || strings.ContainsRune(string(document.Data), '\x00') {
		return "", "", fileError("Choose a UTF-8 text or Markdown file, not binary content.")
	}
	text := strings.TrimPrefix(strings.ReplaceAll(string(document.Data), "\r\n", "\n"), "\ufeff")
	if strings.TrimSpace(text) == "" {
		return "", "", fileError("This file is empty. Choose a file containing a note.")
	}
	title, body, _ := strings.Cut(text, "\n")
	title = strings.TrimSpace(strings.TrimPrefix(title, "# "))
	if title == "" {
		title = strings.TrimSuffix(document.Name, filepath.Ext(document.Name))
	}
	return title, strings.TrimLeft(body, "\n"), nil
}

func attachmentRoute(noteID, handle string) string {
	return "/notes/" + url.PathEscape(noteID) + "/files/" + url.PathEscape(handle)
}

func fileError(message string) error {
	return &nativeui.ActionError{Code: "validation", Message: message}
}
