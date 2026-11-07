// Package screens defines the app's screens in Go. The bundled Android and
// UIKit renderers draw them, so the app has no Swift or Java of its own.
package screens

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

// Root is the first screen the app shows.
const Root = "/items"

// About is the text of the About screen. Edit it while gofastr-native dev
// runs to see one rebuild and restart.
const About = "Items keeps short notes on this device. Saved items and unsaved drafts live in the app's private data directory."

const (
	appName     = "Starter"
	collection  = "items"
	draftPrefix = "starter.item.draft."
	maxTitle    = 120
	maxBody     = 4000
	// files.Store holds at most files.MaxStoredFiles records. Saving an edit
	// writes the new copy before removing the old one, so keep one slot free.
	maxItems = files.MaxStoredFiles - 1
)

// Item is one saved record.
type Item struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Store keeps items in a private directory through device/files, which
// restricts the directory to the current user and writes each record to a
// synced temporary file before renaming it into place.
type Store struct{ files *files.Store }

// OpenStore opens or creates the item directory.
func OpenStore(directory string) (*Store, error) {
	store, err := files.NewStore(directory)
	if err != nil {
		return nil, err
	}
	return &Store{files: store}, nil
}

type entry struct {
	ID   string
	Item Item
}

func (store *Store) list(ctx context.Context) ([]entry, error) {
	found, err := store.files.List(ctx, collection)
	if err != nil {
		return nil, err
	}
	entries := make([]entry, 0, len(found))
	for _, file := range found {
		item, err := store.get(ctx, file.Handle)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry{ID: file.Handle, Item: item})
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := strings.ToLower(entries[i].Item.Title), strings.ToLower(entries[j].Item.Title)
		if a != b {
			return a < b
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, nil
}

func (store *Store) get(ctx context.Context, id string) (Item, error) {
	document, err := store.files.Read(ctx, collection, id)
	if err != nil {
		return Item{}, err
	}
	var item Item
	if err := json.Unmarshal(document.Data, &item); err != nil {
		return Item{}, err
	}
	return item, nil
}

var errStaleCopy = errors.New("the previous copy could not be removed")

// save writes item as a new record, then removes the previous one. It
// returns the new identifier. When only the removal fails, the new record is
// saved and the error wraps errStaleCopy.
func (store *Store) save(ctx context.Context, previous string, item Item) (string, error) {
	if previous == "" {
		existing, err := store.files.List(ctx, collection)
		if err != nil {
			return "", err
		}
		if len(existing) >= maxItems {
			return "", &nativeui.ActionError{Code: "validation", Message: "This starter store is full. Delete an item first."}
		}
	}
	data, err := json.Marshal(item)
	if err != nil {
		return "", err
	}
	saved, err := store.files.Put(ctx, collection, files.Document{Name: "item.json", MediaType: "application/json", Data: data})
	if err != nil {
		return "", err
	}
	if previous != "" {
		if err := store.files.Delete(ctx, collection, previous); err != nil && !errors.Is(err, files.ErrNotFound) {
			return saved.Handle, errors.Join(errStaleCopy, err)
		}
	}
	return saved.Handle, nil
}

// New builds the app's screen definitions over store.
func New(store *Store) (*nativeui.App, error) {
	if store == nil {
		return nil, errors.New("screens: a store is required")
	}
	editorActions := map[string]nativeui.Handle{"save": save(store), "delete": remove(store)}
	return nativeui.New(Root,
		nativeui.Route{Path: Root, Screen: list(store), Actions: map[string]nativeui.Handle{"new": push("/items/new"), "about": push("/about")}},
		nativeui.Route{Path: "/items/new", Screen: editor(store), Actions: editorActions},
		nativeui.Route{Path: "/items/{id}", Screen: editor(store), Actions: editorActions},
		nativeui.Route{Path: "/about", Screen: about},
	)
}

func list(store *Store) nativeui.Render {
	return func(ctx context.Context, _ nativeui.Request) (nativeui.Screen, error) {
		entries, err := store.list(ctx)
		if err != nil {
			return nativeui.Screen{}, err
		}
		rows := make([]nativeui.Row, 0, len(entries))
		for _, entry := range entries {
			title := entry.Item.Title
			if strings.TrimSpace(title) == "" {
				title = "Untitled"
			}
			subtitle, _, _ := strings.Cut(entry.Item.Body, "\n")
			rows = append(rows, nativeui.Row{ID: entry.ID, Title: title, Subtitle: subtitle, Destination: "/items/" + entry.ID})
		}
		return nativeui.Screen{Title: appName, LargeTitle: true,
			Actions: []nativeui.Action{
				{ID: "new", Label: "New item", Icon: nativeui.IconCompose, Primary: true},
				{ID: "about", Label: "About", Icon: nativeui.IconInfo},
			},
			List: &nativeui.List{Empty: "No items yet. Tap New item to add one.", Rows: rows}}, nil
	}
}

func editor(store *Store) nativeui.Render {
	return func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
		id := request.Params["id"]
		item := Item{}
		title, status, key := "New item", "Not saved yet. Tap Save to keep it.", draftPrefix+"new"
		actions := []nativeui.Action{{ID: "save", Label: "Save", Primary: true, Commit: true}}
		if id != "" {
			var err error
			if item, err = store.get(ctx, id); err != nil {
				return nativeui.Screen{}, storeError(err)
			}
			title, status, key = "Item", "Saved on this device", draftPrefix+id
			actions = append(actions, nativeui.Action{ID: "delete", Label: "Delete item", Commit: true})
		}
		return nativeui.Screen{Title: title, Actions: actions, Form: &nativeui.Form{Status: status,
			Fields: []nativeui.Field{
				{Key: "title", Label: "Title", Placeholder: "Title", Value: item.Title, Kind: "text", Style: "title", Required: true},
				{Key: "body", Label: "Notes", Placeholder: "Details", Value: item.Body, Kind: "multiline"},
			},
			Draft: &nativeui.Draft{Key: key, Changed: "Unsaved changes. Draft kept on this device.", Restored: "Unsaved draft restored. Tap Save to keep it.",
				BackLabel: "Items", Title: "Keep your draft?", Message: "Your changes are not saved yet.",
				KeepEditing: "Keep Editing", KeepDraft: "Keep Draft", Discard: "Discard Changes"}}}, nil
	}
}

func save(store *Store) nativeui.Handle {
	return func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
		item := Item{Title: strings.TrimSpace(request.Values["title"]), Body: request.Values["body"]}
		if problems := validate(item); len(problems) != 0 {
			return nativeui.Result{}, &nativeui.ActionError{Code: "validation", Message: "Check the highlighted fields.", Fields: problems}
		}
		id, err := store.save(ctx, request.Params["id"], item)
		announcement := "Item saved"
		if errors.Is(err, errStaleCopy) {
			announcement = "Item saved. The previous copy could not be removed."
		} else if err != nil {
			return nativeui.Result{}, storeError(err)
		}
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}, ClearDraft: true, Announcement: announcement,
			Saved: &nativeui.SavedState{Route: "/items/" + id, Values: map[string]string{"title": item.Title, "body": item.Body}, DraftKey: draftPrefix + id}}, nil
	}
}

func remove(store *Store) nativeui.Handle {
	return func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
		if err := store.files.Delete(ctx, collection, request.Params["id"]); err != nil {
			return nativeui.Result{}, storeError(err)
		}
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}, ClearDraft: true, Announcement: "Item deleted"}, nil
	}
}

func about(context.Context, nativeui.Request) (nativeui.Screen, error) {
	return nativeui.Screen{Title: "About", Text: About}, nil
}

func push(route string) nativeui.Handle {
	return func(context.Context, nativeui.Request) (nativeui.Result, error) {
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: route}}, nil
	}
}

func validate(item Item) map[string]string {
	problems := map[string]string{}
	switch {
	case item.Title == "":
		problems["title"] = "Enter a title."
	case utf8.RuneCountInString(item.Title) > maxTitle:
		problems["title"] = "Use at most 120 characters."
	}
	if utf8.RuneCountInString(item.Body) > maxBody {
		problems["body"] = "Use at most 4000 characters."
	}
	return problems
}

func storeError(err error) error {
	var action *nativeui.ActionError
	if errors.As(err, &action) {
		return err
	}
	if errors.Is(err, files.ErrNotFound) {
		return &nativeui.ActionError{Code: "not_found", Message: "This item is no longer available.", Err: err}
	}
	return err
}
