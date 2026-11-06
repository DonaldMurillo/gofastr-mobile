package notesnative

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func servicesHello(t *testing.T) string {
	t.Helper()
	reply := showcaseCommand(t, nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "uikit", Capabilities: map[nativeui.Capability]nativeui.Support{
		nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable,
		nativeui.CapabilityDrafts: nativeui.SupportAvailable, nativeui.CapabilityFiles: nativeui.SupportAvailable,
		nativeui.CapabilityPresentation: nativeui.SupportAvailable,
		nativeui.CapabilityPhotos:       nativeui.SupportAvailable, nativeui.CapabilitySharing: nativeui.SupportAvailable,
		nativeui.CapabilityClipboard: nativeui.SupportAvailable, nativeui.CapabilityNotifications: nativeui.SupportAvailable,
		nativeui.CapabilityAudio: nativeui.SupportAvailable, nativeui.CapabilityLive: nativeui.SupportAvailable,
		"gofastr.media/v1": nativeui.SupportAvailable,
	}}})
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	return reply.Connection
}

func liveVersion(t *testing.T, connection string) string {
	t.Helper()
	reply := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes"})
	if reply.Error != nil || reply.Screen.Live == nil || reply.Screen.Live.Version == "" {
		t.Fatalf("no Notes live version: %+v", reply)
	}
	return reply.Screen.Live.Version
}

func serviceResult(t *testing.T, command nativeui.Command, identifier string, result nativeui.ServiceResult) nativeui.Reply {
	t.Helper()
	command.Kind, command.ServiceID, command.ServiceResult = "service-result", identifier, &result
	return filesActionResult(t, command)
}

func TestNotesDeviceServicesKeepMetadataAndDirtyTextIndependent(t *testing.T) {
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	connection := servicesHello(t)
	newNote := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/new"})
	if newNote.Response != nil {
		t.Fatal("opening a note initiated a device service")
	}
	saved := filesAction(t, nativeui.Command{Connection: connection, Instance: newNote.Instance, RequestID: "create", Action: "save", Values: map[string]string{
		"title": "Saved reminder", "body": "Saved body", "reminder_date": "2050-11-05", "reminder_time": "08:15",
	}})
	route := saved.Response.Saved.Route
	noteID := strings.TrimPrefix(route, "/notes/")
	editor := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: route})
	dirty := map[string]string{"title": "Unsaved title", "body": "Unsaved body", "reminder_date": "2050-11-06", "reminder_time": "09:45"}
	checkpoint := showcaseCommand(t, nativeui.Command{Kind: "checkpoint", Connection: connection, Instance: editor.Instance, Revision: 1, Values: dirty})
	if checkpoint.Error != nil || !checkpoint.Editor.Dirty || !checkpoint.Editor.DraftStored {
		t.Fatalf("checkpoint: %+v", checkpoint)
	}
	version := liveVersion(t, connection)
	identifier := ""
	for index, test := range []struct {
		action  string
		status  nativeui.EffectStatus
		message string
	}{
		{"reminder-schedule", nativeui.EffectDenied, "Permission denied"},
		{"reminder-schedule", nativeui.EffectCancelled, "Cancelled"},
		{"reminder-schedule", nativeui.EffectUnsupported, "Notifications unavailable"},
		{"reminder-schedule", nativeui.EffectFailed, "Schedule rejected by device"},
		{"reminder-schedule", nativeui.EffectCompleted, "Scheduled on device"},
		{"reminder-cancel", nativeui.EffectUnsupported, "Cancellation unavailable"},
		{"reminder-cancel", nativeui.EffectCompleted, "Cancellation completed"},
		{"reminder-status", nativeui.EffectCompleted, "Scheduled reminder is pending"},
	} {
		command := nativeui.Command{Connection: connection, Instance: editor.Instance, Revision: 1, Values: dirty, RequestID: fmt.Sprintf("device-%d", index), Action: test.action}
		shown := filesAction(t, command)
		if shown.ServiceID == "" || shown.Response.Service == nil || shown.Response.Service.Notification == nil {
			t.Fatalf("no typed notification request: %+v", shown)
		}
		request := shown.Response.Service.Notification
		if identifier == "" {
			identifier = request.Identifier
		}
		if request.Identifier != identifier || !strings.HasPrefix(identifier, "notes.reminder.") {
			t.Fatal("reminder identifier is not stable/scoped")
		}
		if test.action == "reminder-schedule" && (request.Date != dirty["reminder_date"] || request.Time != dirty["reminder_time"] || request.Route != route) {
			t.Fatalf("lost local date/time or route: %+v", request)
		}
		if index == 0 {
			forged := command
			forged.Kind, forged.RequestID, forged.ServiceID, forged.ServiceResult = "service-result", "forged-service", "foreign-service", &nativeui.ServiceResult{Status: nativeui.EffectCompleted}
			if reply := showcaseCommand(t, forged); reply.Error == nil {
				t.Fatal("foreign service identity accepted")
			}
		}
		command.RequestID += "-result"
		outcome := nativeui.ServiceResult{Status: test.status, Message: test.message, Identifier: identifier}
		completed := serviceResult(t, command, shown.ServiceID, outcome)
		if completed.Response.Saved != nil || completed.Response.ClearDraft || !completed.Editor.Dirty || completed.Editor.Values["title"] != dirty["title"] || completed.Editor.Values["body"] != dirty["body"] {
			t.Fatalf("device result committed/lost draft: %+v", completed)
		}
		if test.status != nativeui.EffectCompleted && !strings.Contains(completed.Response.Announcement, "No success was confirmed") {
			t.Fatalf("dishonest unsuccessful status: %+v", completed.Response)
		}
		serviceResult(t, command, shown.ServiceID, outcome)
	}
	for _, action := range []string{"share", "copy"} {
		command := nativeui.Command{Connection: connection, Instance: editor.Instance, Revision: 1, Values: dirty, RequestID: action, Action: action}
		shown := filesAction(t, command)
		if action == "share" && (shown.Response.Service.Share.Text != "Unsaved title\n\nUnsaved body" || len(shown.Response.Service.Share.Files) != 1 || string(shown.Response.Service.Share.Files[0].Data) != "Unsaved title\n\nUnsaved body") {
			t.Fatal("rich note share lost current text/document")
		}
		if action == "copy" && shown.Response.Service.Clipboard.Text != "Unsaved title\n\nUnsaved body" {
			t.Fatal("copy lost current draft")
		}
		command.RequestID += "-result"
		finished := serviceResult(t, command, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCompleted})
		if finished.Response.ClearDraft || !finished.Editor.Dirty {
			t.Fatal("sharing/copy saved the note")
		}
	}
	if liveVersion(t, connection) != version {
		t.Fatal("device outcomes bumped repository version without a write")
	}
	note, err := Get(noteID)
	if err != nil || note.Title != "Saved reminder" || note.Body != "Saved body" || note.ReminderDate != "2050-11-05" || note.ReminderTime != "08:15" {
		t.Fatalf("device flow changed saved note: %+v %v", note, err)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	connection = servicesHello(t)
	reopened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: route})
	if !reopened.Editor.Restored || reopened.Editor.Values["title"] != dirty["title"] {
		t.Fatal("service flow did not preserve durable draft")
	}
	status := filesAction(t, nativeui.Command{Connection: connection, Instance: reopened.Instance, RequestID: "status-after-restart", Action: "reminder-status"})
	if status.Response.Service.Notification.Identifier != identifier {
		t.Fatal("restart changed stable reminder identifier")
	}
	serviceResult(t, nativeui.Command{Connection: connection, Instance: reopened.Instance, RequestID: "status-restarted-result"}, status.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectUnsupported})
	if liveVersion(t, connection) == version {
		t.Fatal("restart reused a previous process epoch")
	}
}

func recordedWAV() files.Document {
	data := make([]byte, 48)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], 1)
	binary.LittleEndian.PutUint32(data[24:28], 8000)
	binary.LittleEndian.PutUint32(data[28:32], 16000)
	binary.LittleEndian.PutUint16(data[32:34], 2)
	binary.LittleEndian.PutUint16(data[34:36], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:44], 4)
	return files.Document{Name: "recording.wav", MediaType: "audio/wav", Data: data}
}

func TestNotesRecordingValidatesPersistsReopensPlaysAndSharesWithoutSavingDraft(t *testing.T) {
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	note, err := Save("", "Audio note", "Saved body")
	if err != nil {
		t.Fatal(err)
	}
	connection := servicesHello(t)
	route := "/notes/" + note.ID
	editor := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: route})
	dirty := map[string]string{"title": "Dirty recording title", "body": "Do not save this body"}
	version := liveVersion(t, connection)
	for _, outcome := range []nativeui.EffectStatus{nativeui.EffectCancelled, nativeui.EffectDenied, nativeui.EffectUnsupported} {
		command := nativeui.Command{Connection: connection, Instance: editor.Instance, Revision: 1, Values: dirty, Action: "record", RequestID: "record-" + string(outcome)}
		shown := filesAction(t, command)
		if shown.ServiceID == "" || shown.Response.Service.Audio.MaxSeconds != 60 || shown.Response.Service.Audio.MaxBytes != 512<<10 {
			t.Fatal("recording is not bounded")
		}
		command.RequestID += "-result"
		completed := serviceResult(t, command, shown.ServiceID, nativeui.ServiceResult{Status: outcome})
		if completed.Response.Navigation != nil || !completed.Editor.Dirty {
			t.Fatal("unsuccessful recording changed editor/navigation")
		}
	}
	if liveVersion(t, connection) != version {
		t.Fatal("cancelled/denied recording bumped live data")
	}
	empty := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: route + "/files"})
	if empty.Error != nil || len(empty.Screen.List.Rows) != 0 {
		t.Fatal("unsuccessful recording created an attachment")
	}
	command := nativeui.Command{Connection: connection, Instance: editor.Instance, Revision: 1, Values: dirty, RequestID: "record-valid", Action: "record"}
	shown := filesAction(t, command)
	document := recordedWAV()
	for index, invalid := range []nativeui.ServiceResult{
		{Status: nativeui.EffectCompleted, File: &files.Document{Name: "fake.wav", MediaType: "audio/wav", Data: []byte("not audio")}, DurationMillis: 1},
		{Status: nativeui.EffectCompleted, File: &document, DurationMillis: 60001},
		{Status: nativeui.EffectCompleted, File: &files.Document{Name: "large.wav", MediaType: "audio/wav", Data: make([]byte, (512<<10)+1)}, DurationMillis: 1},
		{Status: nativeui.EffectCancelled, File: &document, DurationMillis: 1},
	} {
		reply := showcaseCommand(t, nativeui.Command{Kind: "service-result", Connection: connection, Instance: editor.Instance, Revision: 1, Values: dirty, RequestID: fmt.Sprintf("invalid-audio-%d", index), ServiceID: shown.ServiceID, ServiceResult: &invalid})
		if reply.Error == nil || reply.Error.Code != "validation" {
			t.Fatalf("invalid audio admitted: %+v", reply)
		}
	}
	dirty["body"] = "Edited again while recording was pending"
	checkpoint := showcaseCommand(t, nativeui.Command{Kind: "checkpoint", Connection: connection, Instance: editor.Instance, Revision: 2, Values: dirty})
	if checkpoint.Error != nil || !checkpoint.Editor.Dirty {
		t.Fatalf("could not edit during recording: %+v", checkpoint)
	}
	command.Revision = 2
	command.RequestID = "record-success"
	completed := serviceResult(t, command, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCompleted, File: &document, DurationMillis: 1})
	if completed.Response.Navigation == nil || completed.Response.Saved != nil || completed.Response.ClearDraft || !completed.Editor.Dirty || completed.Editor.Values["title"] != dirty["title"] {
		t.Fatalf("recording lost current draft: %+v", completed)
	}
	attachmentRoute := completed.Response.Navigation.Route
	newVersion := liveVersion(t, connection)
	if newVersion == version {
		t.Fatal("successful recording did not bump live data")
	}
	serviceResult(t, command, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCompleted, File: &document, DurationMillis: 1})
	if liveVersion(t, connection) != newVersion {
		t.Fatal("duplicate recording settlement wrote again")
	}
	list := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: route + "/files"})
	if list.Error != nil || len(list.Screen.List.Rows) != 1 {
		t.Fatalf("recording missing or duplicated: %+v", list)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	connection = servicesHello(t)
	detail := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: attachmentRoute})
	if detail.Error != nil || len(screenMedia(t, detail.Screen).Attachments) != 1 || screenMedia(t, detail.Screen).Attachments[0].Audio == nil {
		t.Fatalf("no persisted playback: %+v", detail)
	}
	playback := screenMedia(t, detail.Screen).Attachments[0].Audio
	if playback.DurationMillis != 1 || !bytes.Equal(playback.File.Data, document.Data) || playback.File.MediaType != "audio/wav" {
		t.Fatal("duration or audio bytes did not survive restart")
	}
	share := filesAction(t, nativeui.Command{Connection: connection, Instance: detail.Instance, RequestID: "share-audio", Action: "share"})
	if len(share.Response.Service.Share.Files) != 1 || !bytes.Equal(share.Response.Service.Share.Files[0].Data, document.Data) || share.Response.Service.Share.Files[0].MediaType != "audio/wav" {
		t.Fatal("audio sharing leaked private envelope instead of audio")
	}
	serviceResult(t, nativeui.Command{Connection: connection, Instance: detail.Instance, RequestID: "shared-audio"}, share.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCancelled})
	reopened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: route})
	if !reopened.Editor.Restored || reopened.Editor.Values["body"] != dirty["body"] || reopened.Screen.Form.Fields[1].Value != "Saved body" {
		t.Fatal("recording committed dirty text or lost durable draft")
	}
	remove := filesAction(t, nativeui.Command{Connection: connection, Instance: detail.Instance, RequestID: "remove-audio", Action: "remove"})
	beforeRemove := liveVersion(t, connection)
	filesActionResult(t, nativeui.Command{Kind: "respond", Connection: connection, Instance: detail.Instance, RequestID: "removed-audio", PresentationID: remove.PresentationID, Choice: "remove"})
	if liveVersion(t, connection) == beforeRemove {
		t.Fatal("successful attachment deletion did not bump live data")
	}
}

func TestNotesRecordingStorageFailureDoesNotBumpLiveOrSaveText(t *testing.T) {
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	note, err := Save("", "Full storage", "Still saved")
	if err != nil {
		t.Fatal(err)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	fixture, err := files.NewStore(filepath.Join(directory, "mobile-files"))
	if err != nil {
		t.Fatal(err)
	}
	for index := range files.MaxStoredFiles {
		if _, err := fixture.Put(context.Background(), note.ID, files.Document{Name: fmt.Sprintf("existing-%d.txt", index), MediaType: "text/plain", Data: []byte("existing")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	connection := servicesHello(t)
	editor := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/" + note.ID})
	version := liveVersion(t, connection)
	shown := filesAction(t, nativeui.Command{Connection: connection, Instance: editor.Instance, RequestID: "record-full", Action: "record"})
	document := recordedWAV()
	failed := showcaseCommand(t, nativeui.Command{Kind: "service-result", Connection: connection, Instance: editor.Instance, RequestID: "record-full-result", ServiceID: shown.ServiceID, ServiceResult: &nativeui.ServiceResult{Status: nativeui.EffectCompleted, File: &document, DurationMillis: 1}})
	if failed.Error == nil || failed.Error.Code != "validation" || liveVersion(t, connection) != version {
		t.Fatalf("failed storage treated as a successful live write: %+v", failed)
	}
	list := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/" + note.ID + "/files"})
	if list.Error != nil || len(list.Screen.List.Rows) != files.MaxStoredFiles {
		t.Fatal("storage failure changed the attachment count")
	}
	unchanged, err := Get(note.ID)
	if err != nil || unchanged.Title != "Full storage" || unchanged.Body != "Still saved" {
		t.Fatal("storage failure changed note text")
	}
}

func TestNotesShareCopyBoundsDoNotCommitDraft(t *testing.T) {
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	connection := servicesHello(t)
	editor := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/new"})
	version := liveVersion(t, connection)
	for _, action := range []string{"share", "copy"} {
		reply := showcaseCommand(t, nativeui.Command{Kind: "action", Connection: connection, Instance: editor.Instance, RequestID: "large-" + action, Action: action, Revision: 1, Values: map[string]string{"title": "Large draft", "body": strings.Repeat("x", 64<<10)}})
		if reply.Error == nil || reply.Error.Code != "validation" || reply.ServiceID != "" || !reply.Editor.Dirty {
			t.Fatalf("oversized share/copy reached OS or committed draft: %+v", reply)
		}
	}
	if liveVersion(t, connection) != version {
		t.Fatal("failed share/copy changed repository data")
	}
}
