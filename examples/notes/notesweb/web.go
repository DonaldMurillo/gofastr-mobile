// Package notesweb is a handwritten mobile-web presentation of Notes.
// It depends on typed application behavior and the external mobileui package.
package notesweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-mobile/mobileui"
)

// New mounts an external screen family using GoFastr's existing web host.
func New(service *notes.Service, platform mobileui.Platform) *uihost.UIHost {
	site := appui.NewApp("Notes")
	site.WithTheme(mobileui.Theme(platform))
	site.Register("/", &listScreen{service: service, platform: platform}, nil)
	site.Register("/notes", &listScreen{service: service, platform: platform}, nil)
	site.Register("/notes/create", &editorScreen{service: service, platform: platform}, nil)
	site.Register("/notes/{id}", &editorScreen{service: service, platform: platform}, nil)
	return uihost.New(site)
}

type listScreen struct {
	component.ContextOnly
	service  *notes.Service
	platform mobileui.Platform
}

func (screen *listScreen) ScreenTitle(context.Context) string { return "Notes" }
func (screen *listScreen) RenderCtx(ctx context.Context) render.HTML {
	search := ""
	if request := appui.RequestFromContext(ctx); request != nil {
		search = request.URL.Query().Get("q")
	}
	primary := mobileui.Action{Label: "New note", Href: "/notes/create", Icon: "compose"}
	if screen.platform == mobileui.Android {
		primary.Icon = "add"
	}
	page := mobileui.Page{Platform: screen.platform, Title: "Notes", Subtitle: "A little space for everything on your mind.", Primary: primary}
	items, err := screen.service.List(ctx, search)
	if err != nil {
		appui.SetStatus(ctx, http.StatusInternalServerError)
		return mobileui.Screen(page, mobileui.Empty("Couldn’t load your notes", "Please try again. Your saved notes haven’t been changed.",
			mobileui.Action{Label: "Try again", Href: "/notes"}))
	}
	children := []render.HTML{mobileui.Search(search, "/notes")}
	if len(items) == 0 {
		if strings.TrimSpace(search) == "" {
			children = append(children, mobileui.Empty("Make room for an idea", "A thought, a plan, a list. Your notes stay right here on this device.", primary))
		} else {
			children = append(children, mobileui.Empty("No matching notes", "Try another word, or go back to all your notes.", mobileui.Action{Label: "All notes", Href: "/notes"}))
		}
	} else {
		rows := make([]mobileui.Row, 0, len(items))
		for _, note := range items {
			preview := strings.Join(strings.Fields(note.Body), " ")
			if preview == "" {
				preview = "No additional text"
			}
			meta := "Saved on this device"
			if !note.UpdatedAt.IsZero() {
				meta = note.UpdatedAt.Format("Jan 2, 2006") + " · On this device"
			}
			rows = append(rows, mobileui.Row{Title: note.Title, Preview: preview, Meta: meta, Href: notePath(note.ID)})
		}
		label := "All notes"
		if search != "" {
			label = "Search results"
		}
		children = append(children, mobileui.List(label, strconv.Itoa(len(rows)), rows))
	}
	children = append(children, mobileui.Footer("Stored on this device · Available offline"))
	return mobileui.Screen(page, children...)
}

type editorScreen struct {
	component.ContextOnly
	service  *notes.Service
	platform mobileui.Platform
	id       string
	values   *notes.Note
	message  string
}

func (screen *editorScreen) SetParams(params map[string]string) { screen.id = params["id"] }
func (screen *editorScreen) ScreenTitle(context.Context) string { return "Note" }
func (screen *editorScreen) RenderCtx(ctx context.Context) render.HTML {
	note := notes.Note{ID: screen.id}
	if screen.values != nil {
		note = *screen.values
	} else if screen.id != "" {
		loaded, err := screen.service.Get(ctx, screen.id)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, notes.ErrNotFound) {
				status = http.StatusNotFound
			}
			appui.SetStatus(ctx, status)
			return mobileui.Screen(mobileui.Page{Platform: screen.platform, Title: "Note", Editor: true, BackHref: "/notes", BackLabel: "Notes"},
				mobileui.Empty("Note unavailable", "It may have been removed. Your other notes are still on this device.", mobileui.Action{Label: "All notes", Href: "/notes"}))
		}
		note = loaded
	}
	title, status := "New note", "Not saved yet · tap Done to save on this device"
	if note.ID != "" {
		title, status = note.Title, "Saved on this device · tap Done to keep changes"
	}
	return mobileui.Screen(mobileui.Page{Platform: screen.platform, Title: title, BackHref: "/notes", BackLabel: "Notes", Editor: true,
		Primary: mobileui.Action{Label: "Done", Form: "note-editor"}},
		mobileui.DocumentEditor(ctx, mobileui.Editor{ID: note.ID, Action: "/notes/save", Title: note.Title, Body: note.Body, Status: status, Error: screen.message}))
}

func notePath(id string) string { return "/notes/" + url.PathEscape(id) }

// SaveHandler adapts the shared command to progressively enhanced HTML forms.
// It returns JSON for the presentation behavior and 303 for a native form.
func SaveHandler(service *notes.Service, platform mobileui.Platform, host *uihost.UIHost) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		request.Body = http.MaxBytesReader(writer, request.Body, (1<<20)+16384)
		if err := request.ParseMultipartForm((1 << 20) + 16384); err != nil && !errors.Is(err, http.ErrNotMultipart) {
			http.Error(writer, "Invalid or oversized note", http.StatusBadRequest)
			return
		}
		if request.MultipartForm != nil {
			defer request.MultipartForm.RemoveAll()
		}
		note, err := service.Save(request.Context(), request.FormValue("id"), request.FormValue("title"), request.FormValue("body"))
		status, message := http.StatusOK, ""
		if err != nil {
			status, message = http.StatusInternalServerError, "Couldn’t save. Your text is still here; please try again."
			var validation *notes.ValidationError
			if errors.As(err, &validation) {
				status, message = http.StatusUnprocessableEntity, validation.Message
			} else if errors.Is(err, notes.ErrNotFound) {
				status, message = http.StatusNotFound, "This note is no longer available. Your draft is still here."
			}
		}
		if strings.Contains(request.Header.Get("Accept"), "application/json") {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(status)
			if err != nil {
				_ = json.NewEncoder(writer).Encode(map[string]string{"error": message})
			} else {
				_ = json.NewEncoder(writer).Encode(map[string]string{"id": note.ID, "destination": notePath(note.ID) + "?saved=1"})
			}
			return
		}
		if err != nil {
			values := notes.Note{ID: request.FormValue("id"), Title: request.FormValue("title"), Body: request.FormValue("body")}
			host.RenderScreen(writer, request, &editorScreen{service: service, platform: platform, values: &values, message: message}, uihost.ScreenResponse{Status: status})
			return
		}
		http.Redirect(writer, request, fmt.Sprintf("%s?saved=1", notePath(note.ID)), http.StatusSeeOther)
	}
}
