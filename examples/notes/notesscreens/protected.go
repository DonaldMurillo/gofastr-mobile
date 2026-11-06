package notesscreens

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/DonaldMurillo/gofastr-native/device/securestore"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notedevices"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func protectedKey(id string) string              { return "notes.protected." + id }
func protectedEditRoute(id, lease string) string { return "/protected/" + id + "/edit/" + lease }

func protectedFailure(err error) error {
	if errors.Is(err, notedevices.ErrLocked) {
		return &nativeui.ActionError{Code: "forbidden", Message: "This protected note is locked. Open it again to authenticate."}
	}
	return err
}

func protectedRoutes(devices *notedevices.Store) []nativeui.Route {
	load := func(ctx context.Context, id string) (notedevices.ProtectedEntry, error) {
		state, err := devices.Snapshot(ctx)
		if err != nil {
			return notedevices.ProtectedEntry{}, err
		}
		entry, exists := state.Protected[id]
		if !exists || !notedevices.ValidID(id) {
			return entry, &nativeui.ActionError{Code: "not_found", Message: "This protected note is no longer available."}
		}
		return entry, nil
	}
	secure := func(id, operation string, value []byte) nativeui.Result {
		reason := ""
		if operation != "delete" {
			reason = "Authenticate to " + map[string]string{"get": "open", "put": "save"}[operation] + " this protected note."
		}
		return nativeui.Result{Service: &nativeui.ServiceRequest{ID: operation, Commit: true, SecureStorage: &securestore.Request{
			Operation: securestore.Operation(operation), Key: protectedKey(id), Protection: securestore.Biometric, Value: value, Reason: reason,
		}}}
	}
	return []nativeui.Route{
		{Path: "/protected", Screen: func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
			state, err := devices.Snapshot(ctx)
			if err != nil {
				return nativeui.Screen{}, err
			}
			entries := make([]notedevices.ProtectedEntry, 0, len(state.Protected))
			for _, entry := range state.Protected {
				entries = append(entries, entry)
			}
			sort.Slice(entries, func(i, j int) bool {
				if entries[i].Updated.Equal(entries[j].Updated) {
					return entries[i].ID < entries[j].ID
				}
				return entries[i].Updated.After(entries[j].Updated)
			})
			list := &nativeui.List{Header: "Locked notes", Empty: "No protected notes.\nCreate a note whose title and text require biometrics to open.", Footer: "Protected notes hold up to 16 KiB of text, including the title. Unsaved text clears when Notes goes to the background."}
			for _, entry := range entries {
				detail := "Saved " + entry.Updated.Local().Format("Jan 2, 2006 15:04")
				if entry.Pending {
					detail = "Save not confirmed. Open to check."
				}
				list.Rows = append(list.Rows, nativeui.Row{ID: entry.ID, Title: "Protected note", Subtitle: detail, Destination: "/protected/" + entry.ID})
			}
			return nativeui.Screen{Title: "Protected notes", LargeTitle: true, List: list, Actions: []nativeui.Action{{ID: "new", Label: "New protected note", Primary: true}, {ID: "status", Label: "Check biometrics"}}}, nil
		}, Actions: map[string]nativeui.Handle{
			"new": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
				epoch, err := devices.Epoch(ctx)
				if err != nil {
					return nativeui.Result{}, err
				}
				id, err := notedevices.Identifier()
				if err != nil {
					return nativeui.Result{}, err
				}
				token, err := devices.Grant(ctx, id, epoch, notedevices.ProtectedNote{})
				if err != nil {
					return nativeui.Result{}, protectedFailure(err)
				}
				return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: protectedEditRoute(id, token)}}, nil
			},
			"status": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "status", SecureStorage: &securestore.Request{Operation: securestore.Status}}}, nil
			},
		}, Services: map[string]nativeui.ServiceRespond{"status": func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
			if outcome.Status != nativeui.EffectCompleted {
				return featureOutcome("Biometrics could not be checked.", outcome), nil
			}
			if outcome.SecureStorage.BiometricAvailable {
				return nativeui.Result{Announcement: "Biometrics are available for protected notes."}, nil
			}
			return nativeui.Result{Announcement: "Set up a supported fingerprint or face biometric in this device's settings before saving protected notes."}, nil
		}}},
		{Path: "/protected/{id}", Screen: func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
			entry, err := load(ctx, request.Params["id"])
			if err != nil {
				return nativeui.Screen{}, err
			}
			epoch, err := devices.Epoch(ctx)
			if err != nil {
				return nativeui.Screen{}, err
			}
			request.Values["epoch"] = epoch
			message := "Authenticate to read or edit this note. Its title and text are encrypted in this device's secure storage."
			if entry.Pending {
				message = "A previous save was not confirmed. Unlock to check the encrypted content, or delete this entry."
			}
			return nativeui.Screen{Title: "Protected note", Text: message, Protection: &nativeui.ScreenProtection{LockRoute: "/protected"},
				Requires: []nativeui.Requirement{{Capability: nativeui.CapabilitySecureStorage}}, Actions: []nativeui.Action{{ID: "unlock", Label: "Unlock note", Primary: true}, {ID: "delete", Label: "Delete protected note"}}}, nil
		}, Actions: map[string]nativeui.Handle{
			"unlock": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
				return secure(request.Params["id"], "get", nil), nil
			},
			"delete": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
				return nativeui.Result{Presentation: &nativeui.Presentation{ID: "delete", Kind: nativeui.PresentationConfirmation, Title: "Delete protected note?", Message: "This permanently removes the encrypted note from this device.", Dismissible: true, Choices: []nativeui.PresentationChoice{{ID: "cancel", Label: "Cancel", Role: "cancel"}, {ID: "delete", Label: "Delete note", Role: "destructive"}}}}, nil
			},
		}, Presentations: map[string]nativeui.Respond{"delete": func(ctx context.Context, request nativeui.Request, choice nativeui.PresentationResult) (nativeui.Result, error) {
			if choice.Cancelled || choice.Choice != "delete" {
				return nativeui.Result{}, nil
			}
			return secure(request.Params["id"], "delete", nil), nil
		}}, Services: map[string]nativeui.ServiceRespond{
			"get": func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
				if outcome.Status != nativeui.EffectCompleted {
					return featureOutcome("The note remains locked.", outcome), nil
				}
				if !outcome.SecureStorage.Found {
					return nativeui.Result{Announcement: "Encrypted content is missing. You can delete this unfinished entry."}, nil
				}
				note, err := notedevices.DecodeProtected(outcome.SecureStorage.Value)
				if err != nil {
					return nativeui.Result{}, fileError(err.Error())
				}
				token, err := devices.Grant(ctx, request.Params["id"], request.ServiceValues["epoch"], note)
				if err != nil {
					return nativeui.Result{}, protectedFailure(err)
				}
				if err := devices.Update(ctx, func(state *notedevices.State) error {
					entry := state.Protected[request.Params["id"]]
					entry.Pending = false
					state.Protected[entry.ID] = entry
					return nil
				}); err != nil {
					return nativeui.Result{}, err
				}
				return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: protectedEditRoute(request.Params["id"], token)}}, nil
			},
			"delete": func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
				if outcome.Status != nativeui.EffectCompleted {
					return featureOutcome("Deletion was not confirmed.", outcome), nil
				}
				if err := devices.Update(ctx, func(state *notedevices.State) error { delete(state.Protected, request.Params["id"]); return nil }); err != nil {
					return nativeui.Result{}, err
				}
				return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}, Announcement: "Protected note deleted."}, nil
			},
		}},
		{Path: "/protected/{id}/edit/{lease}", Screen: func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
			note, err := devices.ReadLease(ctx, request.Params["id"], request.Params["lease"])
			if err != nil {
				return nativeui.Screen{}, protectedFailure(err)
			}
			return nativeui.Screen{Title: "Protected note", Protection: &nativeui.ScreenProtection{LockRoute: "/protected"}, Requires: []nativeui.Requirement{{Capability: nativeui.CapabilitySecureStorage}}, Actions: []nativeui.Action{{ID: "save", Label: "Save and lock", Primary: true}},
				Form: &nativeui.Form{Style: "document", Status: "Saving requires biometrics. Unsaved text clears when Notes goes to the background.", Fields: []nativeui.Field{
					{Key: "title", Label: "Title", Kind: "text", Style: "title", Value: note.Title, Placeholder: "Title", Required: true},
					{Key: "body", Label: "Note", Kind: "multiline", Value: note.Body, Placeholder: "Start writing"},
				}}}, nil
		}, Actions: map[string]nativeui.Handle{"save": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
			content, err := notedevices.EncodeProtected(request.Values["title"], request.Values["body"])
			if err != nil {
				return nativeui.Result{}, fileError(err.Error())
			}
			id := request.Params["id"]
			if err := devices.Update(ctx, func(state *notedevices.State) error {
				entry, exists := state.Protected[id]
				if !exists {
					entry = notedevices.ProtectedEntry{ID: id, Updated: time.Now().UTC()}
				}
				entry.Pending = true
				state.Protected[id] = entry
				return nil
			}); err != nil {
				return nativeui.Result{}, err
			}
			return secure(id, "put", content), nil
		}}, Services: map[string]nativeui.ServiceRespond{"put": func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
			if outcome.Status != nativeui.EffectCompleted {
				return featureOutcome("Protected note save was not confirmed. Your text stays here while Notes remains in the foreground.", outcome), nil
			}
			if _, err := devices.ReadLease(ctx, request.Params["id"], request.Params["lease"]); err != nil {
				return nativeui.Result{}, protectedFailure(err)
			}
			note, err := notedevices.DecodeProtected(request.Service.SecureStorage.Value)
			if err != nil {
				return nativeui.Result{}, err
			}
			if err := devices.Update(ctx, func(state *notedevices.State) error {
				state.Protected[request.Params["id"]] = notedevices.ProtectedEntry{ID: request.Params["id"], Updated: time.Now().UTC()}
				return nil
			}); err != nil {
				return nativeui.Result{}, err
			}
			return nativeui.Result{Saved: &nativeui.SavedState{Route: request.Route, Values: map[string]string{"title": note.Title, "body": note.Body}}, Navigation: &nativeui.Navigation{Kind: "pop"}, Announcement: "Protected note saved."}, nil
		}}},
	}
}
