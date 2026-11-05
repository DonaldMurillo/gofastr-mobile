package nativeui

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/audio"
	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/device/notifications"
	"github.com/DonaldMurillo/gofastr-native/device/sharing"
)

func serviceCommand(t *testing.T, ctx context.Context, runtime *Runtime, command Command) Reply {
	t.Helper()
	command.Version = ProtocolVersion
	encoded, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := runtime.CommandJSON(ctx, string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	var reply Reply
	if err := json.Unmarshal([]byte(observed), &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

func serviceConnection(t *testing.T, runtime *Runtime) string {
	t.Helper()
	reply := serviceCommand(t, context.Background(), runtime, Command{Kind: "hello", Hello: &Hello{Minimum: 1, Maximum: 1, Renderer: "webview", Capabilities: map[Capability]Support{
		CapabilityNavigation: SupportAvailable, CapabilityEditor: SupportAvailable, CapabilityNotifications: SupportUnavailable,
		CapabilitySharing: SupportFallback, CapabilityClipboard: SupportFallback, CapabilityAudio: SupportFallback, CapabilityLive: SupportAvailable,
		CapabilityPresentation: SupportAvailable, CapabilityFiles: SupportFallback,
	}}})
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	return reply.Connection
}

func serviceFixture(t *testing.T, definition *ServiceRequest, handler ServiceRespond, disabled *atomic.Bool) (*Runtime, Command) {
	t.Helper()
	app, err := New("/device", Route{Path: "/device", Screen: func(context.Context, Request) (Screen, error) {
		actions := []Action{{ID: "start", Label: "Start"}}
		if disabled != nil && disabled.Load() {
			actions = nil
		}
		return Screen{Title: "Device", Actions: actions, Form: &Form{Fields: []Field{{Key: "body", Label: "Body", Kind: "multiline", Value: "saved"}}}}, nil
	}, Actions: map[string]Handle{"start": func(context.Context, Request) (Result, error) { return Result{Service: definition}, nil }}, Services: map[string]ServiceRespond{"device": handler}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(app, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	connection := serviceConnection(t, runtime)
	opened := serviceCommand(t, context.Background(), runtime, Command{Kind: "open", Connection: connection, Route: "/device"})
	if opened.Error != nil {
		t.Fatal(opened.Error)
	}
	return runtime, Command{Connection: connection, Instance: opened.Instance, Revision: 1, Values: map[string]string{"body": "draft"}}
}

func admitService(t *testing.T, runtime *Runtime, command Command) Reply {
	t.Helper()
	command.Kind, command.RequestID, command.Action = "action", "start", "start"
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	command.Kind = "complete"
	reply := serviceCommand(t, context.Background(), runtime, command)
	if reply.Error != nil || reply.ServiceID == "" || reply.Response.Service == nil {
		t.Fatalf("service not admitted: %+v", reply)
	}
	return reply
}

func TestServicesAllOutcomesUseOneShotPublicCommands(t *testing.T) {
	definitions := map[string]ServiceRequest{
		"schedule":  {ID: "device", Notification: &notifications.Request{Operation: "schedule", Identifier: "notes:1", Title: "Remember", Route: "/device", Date: "2028-02-29", Time: "10:00"}},
		"cancel":    {ID: "device", Notification: &notifications.Request{Operation: "cancel", Identifier: "notes:1"}},
		"status":    {ID: "device", Notification: &notifications.Request{Operation: "status", Identifier: "notes:1"}},
		"share":     {ID: "device", Share: &sharing.Request{Text: "Private text"}},
		"clipboard": {ID: "device", Clipboard: &sharing.Clipboard{Text: "Copy"}},
		"audio":     {ID: "device", Audio: &audio.Request{Title: "Record"}, Commit: true},
	}
	for name, definition := range definitions {
		for _, status := range []EffectStatus{EffectCompleted, EffectCancelled, EffectDenied, EffectUnsupported, EffectFailed} {
			t.Run(name+"/"+string(status), func(t *testing.T) {
				var calls atomic.Int32
				runtime, command := serviceFixture(t, &definition, func(_ context.Context, request Request, outcome ServiceResult) (Result, error) {
					calls.Add(1)
					if request.Values["body"] != "newer draft" || outcome.Status != status {
						t.Error("continuation lost current draft or observed outcome")
					}
					return Result{Announcement: string(outcome.Status)}, nil
				}, nil)
				shown := admitService(t, runtime, command)
				command.Kind, command.Action, command.RequestID = "complete", "start", "start"
				if replay := serviceCommand(t, context.Background(), runtime, command); replay.ServiceID != shown.ServiceID {
					t.Fatal("completion recovery issued a new OS task")
				}
				command.Kind, command.RequestID, command.ServiceID, command.Revision = "service-result", "observed", shown.ServiceID, 2
				command.Values = map[string]string{"body": "newer draft"}
				command.ServiceResult = &ServiceResult{Status: status}
				if name == "audio" && status == EffectCompleted {
					file := serviceWAV()
					command.ServiceResult.File, command.ServiceResult.DurationMillis = &file, 1000
				}
				for attempt := 0; attempt < 2; attempt++ {
					if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error != nil || reply.Response.Announcement != string(status) {
						t.Fatalf("service result: %+v", reply)
					}
				}
				command.Kind = "complete"
				settled := serviceCommand(t, context.Background(), runtime, command)
				if settled.Error != nil || calls.Load() != 1 || settled.Editor.Values["body"] != "newer draft" || !settled.Editor.Dirty {
					t.Fatalf("one-shot settlement or draft preservation failed: %+v, calls %d", settled, calls.Load())
				}
				command.RequestID = "start"
				if replay := serviceCommand(t, context.Background(), runtime, command); replay.ServiceID != "" || replay.Response.Service != nil {
					t.Fatal("settled service was replayed")
				}
				command.Kind, command.RequestID = "service-result", "forged-again"
				if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil {
					t.Fatal("old result ID was admitted again")
				}
			})
		}
	}
}

func TestServiceResultRejectsContentIdentityAndTargetDrift(t *testing.T) {
	var calls atomic.Int32
	definition := &ServiceRequest{ID: "device", Audio: &audio.Request{Title: "Record", MaxSeconds: 1}, Commit: true}
	runtime, command := serviceFixture(t, definition, func(context.Context, Request, ServiceResult) (Result, error) { calls.Add(1); return Result{}, nil }, nil)
	shown := admitService(t, runtime, command)
	command.Kind, command.RequestID, command.ServiceID = "service-result", "outcome", shown.ServiceID
	file := serviceWAV()
	for _, outcome := range []ServiceResult{
		{Status: EffectCancelled, File: &file}, {Status: EffectDenied, DurationMillis: 1}, {Status: EffectCompleted},
		{Status: EffectCompleted, File: &file, DurationMillis: 1001}, {Status: "invented"},
		{Status: EffectCompleted, File: &files.Document{Name: "fake.wav", MediaType: "audio/wav", Data: []byte("not audio")}, DurationMillis: 1},
		{Status: EffectCancelled, Message: strings.Repeat("x", 513)}, {Status: EffectCancelled, Permission: "bad\npermission"},
	} {
		command.ServiceResult = &outcome
		if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil || calls.Load() != 0 {
			t.Fatalf("invalid audio reached continuation: %+v", reply)
		}
	}
	command.ServiceResult = &ServiceResult{Status: EffectCancelled}
	command.Values["owner"] = "foreign"
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil {
		t.Fatal("changed non-editor target accepted")
	}
	delete(command.Values, "owner")
	other := serviceConnection(t, runtime)
	command.Connection, command.ServiceID = other, shown.ServiceID
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil {
		t.Fatal("foreign connection accepted")
	}
	command.Connection = shown.Connection
	command.ServiceID = "foreign"
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil {
		t.Fatal("foreign service ID accepted")
	}
	command.ServiceID = shown.ServiceID
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error != nil || calls.Load() != 1 {
		t.Fatal("honest cancellation did not reach continuation")
	}
	command.ServiceResult.Message = "changed outcome"
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil || reply.Error.Code != "conflict" || calls.Load() != 1 {
		t.Fatal("result fingerprint drift accepted")
	}
}

func TestServiceRequestAndPlaybackValidationThroughCommands(t *testing.T) {
	for name, request := range map[string]ServiceRequest{
		"empty":                       {ID: "device"},
		"competing":                   {ID: "device", Share: &sharing.Request{Text: "Share"}, Clipboard: &sharing.Clipboard{Text: "Copy"}},
		"large clipboard":             {ID: "device", Clipboard: &sharing.Clipboard{Text: strings.Repeat("x", (64<<10)+1)}},
		"unregistered reminder route": {ID: "device", Notification: &notifications.Request{Operation: "schedule", Identifier: "notes:1", Title: "Reminder", Route: "/foreign", Date: "2028-01-01", Time: "10:00"}},
	} {
		t.Run(name, func(t *testing.T) {
			runtime, command := serviceFixture(t, &request, func(context.Context, Request, ServiceResult) (Result, error) {
				t.Error("invalid service reached continuation")
				return Result{}, nil
			}, nil)
			command.Kind, command.RequestID, command.Action = "action", "start", "start"
			if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil {
				t.Fatal("invalid request accepted")
			}
			command.Kind = "complete"
			if reply := serviceCommand(t, context.Background(), runtime, command); reply.ServiceID != "" || reply.Response.Service != nil {
				t.Fatal("invalid request issued OS work")
			}
		})
	}
	file := serviceWAV()
	attachment := Attachment{ID: "audio", Name: file.Name, MediaType: file.MediaType, Size: len(file.Data), Destination: "/play", Audio: &audio.Document{File: file, DurationMillis: 1000}}
	app, err := New("/play", Route{Path: "/play", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Playback", Text: "Local bytes", Attachments: []Attachment{attachment}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(app, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	command := Command{Kind: "open", Connection: serviceConnection(t, runtime), Route: "/play"}
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error != nil || reply.Screen.Attachments[0].Audio == nil {
		t.Fatal("validated playback unavailable", reply.Error)
	}
	attachment.Size++
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil {
		t.Fatal("mismatched playback bytes accepted")
	}
	attachment.Size--
	attachment.Audio.DurationMillis = 60001
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil {
		t.Fatal("unbounded playback duration accepted")
	}
}

func TestServiceAdmissionReservesItsResultIdentity(t *testing.T) {
	runtime, command := serviceFixture(t, &ServiceRequest{ID: "device", Clipboard: &sharing.Clipboard{Text: "Copy"}}, func(context.Context, Request, ServiceResult) (Result, error) {
		t.Error("unadmitted continuation ran")
		return Result{}, nil
	}, nil)
	command.Kind, command.Action = "action", "start"
	for index := 0; index < 64; index++ {
		command.RequestID = fmt.Sprintf("deferred-%d", index)
		if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error != nil {
			t.Fatal(reply.Error)
		}
	}
	command.Kind = "complete"
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil || reply.Error.Code != "pending" || reply.ServiceID != "" || reply.Response.Service != nil {
		t.Fatal("issued OS work without capacity for its one-shot result")
	}
}

func TestServiceReauthorizesAtAdmissionAndContinuation(t *testing.T) {
	for _, beforeAdmission := range []bool{true, false} {
		t.Run(map[bool]string{true: "admission", false: "continuation"}[beforeAdmission], func(t *testing.T) {
			var disabled atomic.Bool
			var calls atomic.Int32
			definition := &ServiceRequest{ID: "device", Clipboard: &sharing.Clipboard{Text: "original"}}
			runtime, command := serviceFixture(t, definition, func(context.Context, Request, ServiceResult) (Result, error) { calls.Add(1); return Result{}, nil }, &disabled)
			command.Kind, command.RequestID, command.Action = "action", "start", "start"
			serviceCommand(t, context.Background(), runtime, command)
			definition.Clipboard.Text = "mutated after dispatch"
			if beforeAdmission {
				disabled.Store(true)
			}
			command.Kind = "complete"
			shown := serviceCommand(t, context.Background(), runtime, command)
			if beforeAdmission {
				if shown.Error == nil || shown.ServiceID != "" {
					t.Fatal("revoked source issued an OS task")
				}
				return
			}
			if shown.Response.Service.Clipboard.Text != "original" {
				t.Fatal("request was not immutable")
			}
			disabled.Store(true)
			command.Kind, command.RequestID, command.ServiceID = "service-result", "observed", shown.ServiceID
			command.ServiceResult = &ServiceResult{Status: EffectCompleted}
			if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil || calls.Load() != 0 {
				t.Fatal("revoked action reached its continuation")
			}
		})
	}
}

func TestServiceAdmissionRejectsInputDriftAndUnknownMutationLocks(t *testing.T) {
	var calls atomic.Int32
	definition := &ServiceRequest{ID: "device", Audio: &audio.Request{Title: "Record"}, Commit: true}
	runtime, command := serviceFixture(t, definition, func(context.Context, Request, ServiceResult) (Result, error) {
		calls.Add(1)
		return Result{}, errors.New("lost write response")
	}, nil)
	command.Kind, command.RequestID, command.Action = "action", "start", "start"
	serviceCommand(t, context.Background(), runtime, command)
	command.Kind, command.Values = "complete", map[string]string{"body": "changed before OS"}
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil || reply.ServiceID != "" {
		t.Fatal("input drift silently issued stale service request")
	}
	command.Values = map[string]string{"body": "draft"}
	shown := serviceCommand(t, context.Background(), runtime, command)
	if shown.ServiceID == "" {
		t.Fatal(shown.Error)
	}
	command.Kind, command.RequestID, command.ServiceID = "service-result", "outcome", shown.ServiceID
	file := serviceWAV()
	command.ServiceResult = &ServiceResult{Status: EffectCompleted, File: &file, DurationMillis: 1}
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil || reply.Error.Code != "unknown" || !reply.Editor.Unknown {
		t.Fatalf("mutation uncertainty was not retained: %+v", reply)
	}
	command.Kind = "complete"
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil || reply.Error.Code != "unknown" {
		t.Fatal("completion hid uncertain write")
	}
	command.Kind, command.RequestID = "action", "retry-write"
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil || reply.Error.Code != "pending" || calls.Load() != 1 {
		t.Fatal("unknown service mutation replayed")
	}
}

func TestServicesShareForegroundOwnerUntilSettlement(t *testing.T) {
	runtime, command := serviceFixture(t, &ServiceRequest{ID: "device", Clipboard: &sharing.Clipboard{Text: "Copy"}}, func(context.Context, Request, ServiceResult) (Result, error) { return Result{}, nil }, nil)
	shown := admitService(t, runtime, command)
	other := serviceCommand(t, context.Background(), runtime, Command{Kind: "open", Connection: command.Connection, Route: "/device"})
	second := command
	second.Instance, second.Kind, second.Action, second.RequestID = other.Instance, "action", "start", "other"
	serviceCommand(t, context.Background(), runtime, second)
	second.Kind = "complete"
	if reply := serviceCommand(t, context.Background(), runtime, second); reply.Error == nil || reply.Error.Code != "pending" || reply.ServiceID != "" {
		t.Fatal("two simultaneous foreground services admitted")
	}
	command.Kind, command.RequestID, command.ServiceID = "service-result", "cancel", shown.ServiceID
	command.ServiceResult = &ServiceResult{Status: EffectCancelled}
	serviceCommand(t, context.Background(), runtime, command)
	if reply := serviceCommand(t, context.Background(), runtime, second); reply.ServiceID != "" {
		t.Fatal("foreground released before result complete")
	}
	command.Kind = "complete"
	serviceCommand(t, context.Background(), runtime, command)
	if reply := serviceCommand(t, context.Background(), runtime, second); reply.Error != nil || reply.ServiceID == "" {
		t.Fatal("settlement leaked foreground ownership")
	}
}

func TestServiceCoordinatesWithFilesAndPresentations(t *testing.T) {
	for _, competing := range []string{"files", "presentation"} {
		t.Run(competing, func(t *testing.T) {
			app, err := New("/foreground", Route{Path: "/foreground", Screen: func(context.Context, Request) (Screen, error) {
				return Screen{Title: "Foreground", Text: "One owner", Actions: []Action{{ID: "service", Label: "Service"}, {ID: "other", Label: "Other"}}}, nil
			}, Actions: map[string]Handle{
				"service": func(context.Context, Request) (Result, error) {
					return Result{Service: &ServiceRequest{ID: "device", Clipboard: &sharing.Clipboard{Text: "Copy"}}}, nil
				},
				"other": func(context.Context, Request) (Result, error) {
					if competing == "files" {
						return Result{Files: &FileRequest{ID: "pick", Pick: &files.Pick{Title: "Choose", MaxBytes: files.MaxBytes}}}, nil
					}
					return Result{Presentation: &Presentation{ID: "present", Kind: PresentationAlert, Title: "Choose", Choices: []PresentationChoice{{ID: "ok", Label: "OK"}}, Dismissible: true}}, nil
				},
			}, Services: map[string]ServiceRespond{"device": func(context.Context, Request, ServiceResult) (Result, error) { return Result{}, nil }},
				Files:         map[string]FileRespond{"pick": func(context.Context, Request, FileResult) (Result, error) { return Result{}, nil }},
				Presentations: map[string]Respond{"present": func(context.Context, Request, PresentationResult) (Result, error) { return Result{}, nil }}})
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := NewRuntime(app, RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			connection := serviceConnection(t, runtime)
			first := serviceCommand(t, context.Background(), runtime, Command{Kind: "open", Connection: connection, Route: "/foreground"})
			second := serviceCommand(t, context.Background(), runtime, Command{Kind: "open", Connection: connection, Route: "/foreground"})
			service := Command{Kind: "action", Action: "service", RequestID: "service", Connection: connection, Instance: first.Instance}
			serviceCommand(t, context.Background(), runtime, service)
			service.Kind = "complete"
			if reply := serviceCommand(t, context.Background(), runtime, service); reply.Error != nil || reply.ServiceID == "" {
				t.Fatal("service not presented", reply.Error)
			}
			other := Command{Kind: "action", Action: "other", RequestID: "other", Connection: connection, Instance: second.Instance}
			serviceCommand(t, context.Background(), runtime, other)
			other.Kind = "complete"
			if reply := serviceCommand(t, context.Background(), runtime, other); reply.Error == nil || reply.Error.Code != "pending" {
				t.Fatal("competing foreground task was admitted")
			}
			service.Kind = "close"
			serviceCommand(t, context.Background(), runtime, service)
			if reply := serviceCommand(t, context.Background(), runtime, other); reply.Error != nil || reply.FileID == "" && reply.PresentationID == "" {
				t.Fatal("closing service did not release shared foreground owner", reply.Error)
			}
			service.Kind = "service-result"
			service.RequestID = "late"
			service.ServiceResult = &ServiceResult{Status: EffectCompleted}
			if reply := serviceCommand(t, context.Background(), runtime, service); reply.Error == nil {
				t.Fatal("closed service result accepted")
			}
		})
	}
}

func serviceWAV() files.Document {
	data := make([]byte, 46)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], 38)
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 8000)
	binary.LittleEndian.PutUint32(data[28:], 16000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 2)
	return files.Document{Name: "recording.wav", MediaType: "audio/wav", Data: data}
}
