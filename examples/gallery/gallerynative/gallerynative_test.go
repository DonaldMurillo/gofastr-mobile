package gallerynative

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func send(t *testing.T, command nativeui.Command) nativeui.Reply {
	t.Helper()
	command.Version = nativeui.ProtocolVersion
	encoded, _ := json.Marshal(command)
	response, err := Command(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	var reply nativeui.Reply
	if err := json.Unmarshal([]byte(response), &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

func TestStartOpenTabsStopAndRestart(t *testing.T) {
	if err := Start("relative"); err == nil {
		t.Fatal("relative data directory accepted")
	}
	for round := 0; round < 2; round++ {
		if err := Start(t.TempDir()); err != nil {
			t.Fatal(err)
		}
		if err := Start(t.TempDir()); err == nil {
			t.Fatal("second Start accepted")
		}
		hello := send(t, nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "android", Capabilities: map[nativeui.Capability]nativeui.Support{
			nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable, nativeui.CapabilityDrafts: nativeui.SupportAvailable}}})
		root := send(t, nativeui.Command{Kind: "open", Connection: hello.Connection, Route: Root()})
		if root.Error != nil || root.Screen == nil || len(root.Screen.Tabs) != 4 {
			t.Fatalf("root: %+v", root)
		}
		for _, tab := range root.Screen.Tabs {
			opened := send(t, nativeui.Command{Kind: "open", Connection: hello.Connection, Route: tab.Route})
			if opened.Error != nil {
				t.Fatalf("tab %s: %+v", tab.Route, opened.Error)
			}
			send(t, nativeui.Command{Kind: "close", Connection: hello.Connection, Instance: opened.Instance})
		}
		draft := send(t, nativeui.Command{Kind: "open", Connection: hello.Connection, Route: "/gallery/draft"})
		if draft.Error != nil {
			t.Fatalf("draft screen needs a real draft store: %+v", draft.Error)
		}
		if _, err := Render(Root(), "{}"); err != nil {
			t.Fatal(err)
		}
		if err := Stop(); err != nil {
			t.Fatal(err)
		}
		if _, err := Command("{}"); err == nil {
			t.Fatal("Command after Stop")
		}
	}
}
