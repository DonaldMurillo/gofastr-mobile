package notesscreens

import (
	"context"
	"fmt"
	"strconv"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesbackup"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func withBackups(routes []nativeui.Route, service *notes.Service, backups *notesbackup.Manager) []nativeui.Route {
	for index := range routes {
		route := &routes[index]
		if route.Path == "/settings" {
			base := route.Screen
			route.Screen = func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
				screen, err := base(ctx, request)
				if err == nil {
					screen.List.Rows = append([]nativeui.Row{{ID: "backup", Title: "Backup and restore", Subtitle: "Copy saved notes and attachments to a file", Destination: "/settings/backup"}}, screen.List.Rows...)
				}
				return screen, err
			}
		}
		if route.Path == Root {
			base := route.Screen
			route.Screen = func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
				screen, err := base(ctx, request)
				if err == nil {
					screen.Actions = append(screen.Actions, nativeui.Action{ID: "backup-selected", Label: "Back up selected", Commit: true})
				}
				return screen, err
			}
			route.Actions["backup-selected"] = func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
				ids, err := selectedIDs(ctx, service, request)
				if err != nil {
					return nativeui.Result{}, err
				}
				document, err := backups.Export(ctx, ids)
				if err != nil {
					return nativeui.Result{}, fileError(err.Error())
				}
				return nativeui.Result{Files: &nativeui.FileRequest{ID: "backup-export", Export: &document}}, nil
			}
		}
	}
	return append(routes,
		nativeui.Route{Path: "/settings/backup", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: "Backup and restore", Text: "Export saved notes, tags, archive and Trash state, reminder fields, and attachments together. Protected notes and unsaved drafts are excluded.\n\nBackup files are not encrypted. Store them somewhere private. The current file adapter supports up to 1 MiB compressed per file. For a larger library, select up to 20 notes at a time from Notes and choose Back up selected.\n\nRestore shows a preview first and adds copies. It preserves your existing notes and does not schedule reminders.", Actions: []nativeui.Action{{ID: "export", Label: "Export backup", Primary: true}, {ID: "restore", Label: "Choose backup to restore"}}}, nil
		}, Actions: map[string]nativeui.Handle{
			"export": func(ctx context.Context, _ nativeui.Request) (nativeui.Result, error) {
				document, err := backups.Export(ctx, nil)
				if err != nil {
					return nativeui.Result{}, fileError(err.Error())
				}
				return nativeui.Result{Files: &nativeui.FileRequest{ID: "export", Export: &document}}, nil
			},
			"restore": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				return nativeui.Result{Files: &nativeui.FileRequest{ID: "restore", Commit: true, Pick: &files.Pick{Title: "Choose Notes backup", Extensions: []string{".gfnbackup"}, MaxBytes: files.MaxBytes}}}, nil
			},
		}, Files: map[string]nativeui.FileRespond{"restore": func(ctx context.Context, _ nativeui.Request, result nativeui.FileResult) (nativeui.Result, error) {
			token, err := backups.Stage(ctx, *result.File)
			if err != nil {
				return nativeui.Result{}, fileError(err.Error())
			}
			return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: "/settings/backup/" + token}}, nil
		}}},
		nativeui.Route{Path: "/settings/backup/{token}", Screen: func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
			preview, err := backups.Preview(ctx, request.Params["token"])
			if err != nil {
				return nativeui.Screen{}, fileError(err.Error())
			}
			list := &nativeui.List{Header: fmt.Sprintf("%d to restore · %d already present", preview.New, preview.Existing), Footer: fmt.Sprintf("%s · %s. Existing notes are preserved. Interrupted restores can resume from this same file.", quantity(len(preview.Bundle.Notes), "note"), quantity(preview.Files, "attachment")), Empty: "This backup contains no notes.", Rows: []nativeui.Row{}}
			for index, record := range preview.Bundle.Notes {
				if index == 100 {
					list.Footer += " Preview shows the first 100 notes; Restore includes the entire backup."
					break
				}
				state := "Active"
				if record.Note.Archived {
					state = "Archived"
				}
				if record.Note.Trashed {
					state = "Trash"
				}
				list.Rows = append(list.Rows, nativeui.Row{ID: strconv.Itoa(index), Title: record.Note.Title, Subtitle: fmt.Sprintf("%s · %s · %s", state, quantity(len(record.Files), "attachment"), notes.SearchSnippet(record.Note.Body, "")), Disabled: true})
			}
			return nativeui.Screen{Title: "Review backup", List: list, Actions: []nativeui.Action{{ID: "apply", Label: "Restore", Primary: true, Commit: true}, {ID: "discard", Label: "Discard backup review", Commit: true}}}, nil
		}, Actions: map[string]nativeui.Handle{
			"apply": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
				result, err := backups.Restore(ctx, request.Params["token"])
				if err != nil {
					return nativeui.Result{Announcement: fmt.Sprintf("Restore stopped after %d notes; %d were already present. Check storage, then choose Restore again to finish this same import.", result.Imported, result.Skipped)}, nil
				}
				return nativeui.Result{Announcement: fmt.Sprintf("Restore complete · %d restored · %d already present", result.Imported, result.Skipped), Navigation: &nativeui.Navigation{Kind: "pop"}}, nil
			},
			"discard": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
				if err := backups.Discard(ctx, request.Params["token"]); err != nil {
					return nativeui.Result{}, fileError(err.Error())
				}
				return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}, Announcement: "Backup review discarded. Saved notes are kept."}, nil
			},
		}},
	)
}

func quantity(count int, label string) string {
	if count != 1 {
		label += "s"
	}
	return fmt.Sprintf("%d %s", count, label)
}
