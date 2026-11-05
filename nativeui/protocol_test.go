package nativeui

import "testing"

func TestRendererContractRejectsSilentFallback(t *testing.T) {
	hello := Hello{Minimum: 1, Maximum: 1, Renderer: "webview", Capabilities: map[Capability]Support{
		CapabilityNavigation: SupportAvailable, CapabilityEditor: SupportAvailable, CapabilitySheets: SupportFallback,
	}}
	contract, err := Negotiate(hello)
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Check([]Requirement{{Capability: CapabilitySheets}}); err == nil {
		t.Fatal("native sheet requirement accepted a full-page fallback")
	}
	if err := contract.Check([]Requirement{{Capability: CapabilitySheets, AllowFallback: true}}); err != nil {
		t.Fatal(err)
	}
	hello.Capabilities[CapabilitySheets] = SupportAvailable
	if contract.Capabilities[CapabilitySheets] != SupportFallback {
		t.Fatal("advertisement mutation changed the connection contract")
	}
	for _, incompatible := range []Hello{
		{Minimum: 2, Maximum: 2, Renderer: "uikit"},
		{Minimum: 1, Maximum: 0, Renderer: "uikit"},
		{Minimum: 1, Maximum: 1, Renderer: "android-native"},
		{Minimum: 1, Maximum: 1, Renderer: "webview", Capabilities: map[Capability]Support{CapabilityNavigation: SupportFallback, CapabilityEditor: SupportAvailable}},
	} {
		if _, err := Negotiate(incompatible); err == nil {
			t.Fatalf("accepted incompatible peer: %+v", incompatible)
		}
	}
}
