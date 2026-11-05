package notesnative

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-mobile/nativeui"
)

func TestOneNotesApplicationShowsEveryPresentation(t *testing.T) {
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := Stop(); err != nil {
			t.Error(err)
		}
	})
	hello := showcaseCommand(t, nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "uikit", Capabilities: map[nativeui.Capability]nativeui.Support{
		nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable, nativeui.CapabilityPresentation: nativeui.SupportAvailable,
	}}})
	if hello.Error != nil {
		t.Fatal(hello.Error)
	}
	for _, name := range []string{"alert", "confirmation", "menu", "dialog", "sheet", "selection", "multiple"} {
		t.Run(name, func(t *testing.T) {
			opened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: hello.Connection, Route: "/behaviors/" + name})
			if opened.Error != nil {
				t.Fatal(opened.Error)
			}
			command := nativeui.Command{Kind: "action", Connection: hello.Connection, Instance: opened.Instance, RequestID: "show-" + name, Action: "show"}
			if reply := showcaseCommand(t, command); reply.Error != nil {
				t.Fatal(reply.Error)
			}
			command.Kind = "complete"
			shown := showcaseCommand(t, command)
			if shown.Error != nil || shown.PresentationID == "" || shown.Response.Presentation == nil {
				t.Fatalf("not presented: %+v", shown)
			}
			command.Kind, command.PresentationID, command.RequestID = "respond", shown.PresentationID, "respond-"+name
			command.Choice = "cancel"
			switch name {
			case "alert":
				command.Choice = "ok"
			case "dialog":
				command.Choice = "continue"
			case "menu":
				command.Choice = "confirm"
			case "selection":
				command.Choice, command.Selected = "apply", []string{"title"}
			case "multiple":
				command.Choice = "apply"
				if invalid := showcaseCommand(t, command); invalid.Error == nil || invalid.Error.Code != "validation" {
					t.Fatal("minimum selection was not enforced")
				}
				command.Selected = []string{"ideas", "shared"}
				if invalid := showcaseCommand(t, command); invalid.Error == nil || invalid.Error.Code != "validation" {
					t.Fatal("disabled selection was not rejected")
				}
				command.Selected = []string{"personal", "work", "ideas"}
			}
			if reply := showcaseCommand(t, command); reply.Error != nil {
				t.Fatal(reply.Error)
			}
			command.Kind = "complete"
			completed := showcaseCommand(t, command)
			if completed.Error != nil {
				t.Fatal(completed.Error)
			}
			if name == "confirmation" && completed.Response.Announcement != "" {
				t.Fatal("cancel approved reset")
			}
			if name == "multiple" && completed.Response.Announcement != "Selected tags: ideas, work, personal" {
				t.Fatalf("selection: %+v", completed.Response)
			}
			if name == "selection" && (completed.Response.Navigation == nil || completed.Response.Navigation.Route != "/notes?order=title") {
				t.Fatal("single selection did not select Notes order")
			}
			if name == "menu" {
				if completed.PresentationID == shown.PresentationID || completed.Response.Presentation == nil || completed.Response.Presentation.Kind != nativeui.PresentationConfirmation {
					t.Fatal("menu continuation did not open confirmation")
				}
				command.Kind, command.PresentationID, command.RequestID, command.Choice = "respond", completed.PresentationID, "reset-after-menu", "reset"
				if reply := showcaseCommand(t, command); reply.Error != nil {
					t.Fatal(reply.Error)
				}
				command.Kind = "complete"
				if result := showcaseCommand(t, command); result.Error != nil || !strings.Contains(result.Response.Announcement, "confirmed") {
					t.Fatal("confirmation continuation did not run")
				}
			}
			if reply := showcaseCommand(t, nativeui.Command{Kind: "close", Connection: hello.Connection, Instance: opened.Instance}); reply.Error != nil {
				t.Fatal(reply.Error)
			}
		})
	}
}

func showcaseCommand(t *testing.T, command nativeui.Command) nativeui.Reply {
	t.Helper()
	command.Version = nativeui.ProtocolVersion
	payload, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Command(string(payload))
	if err != nil {
		t.Fatal(err)
	}
	var result nativeui.Reply
	if err := json.Unmarshal([]byte(encoded), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
