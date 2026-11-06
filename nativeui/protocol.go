package nativeui

import (
	"errors"
	"fmt"
)

// ProtocolVersion identifies the compiled mobile command contract, not an app release.
const ProtocolVersion = 1

// Capability names a versioned presentation service, not a permission grant.
type Capability string

const (
	CapabilityNavigation     Capability = "navigation/v1"
	CapabilityEditor         Capability = "editor/v1"
	CapabilityDrafts         Capability = "drafts/v1"
	CapabilityShare          Capability = "share/v1"
	CapabilityHaptics        Capability = "haptics/v1"
	CapabilitySheets         Capability = "sheets/v1"
	CapabilityAnnouncement   Capability = "announcement/v1"
	CapabilityPresentation   Capability = "presentation/v1"
	CapabilityFiles          Capability = "files/v1"
	CapabilityPhotos         Capability = "photos/v1"
	CapabilityCamera         Capability = "camera/v1"
	CapabilityNotifications  Capability = "notifications/v1"
	CapabilitySharing        Capability = "sharing/v1"
	CapabilityClipboard      Capability = "clipboard/v1"
	CapabilityAudio          Capability = "audio/v1"
	CapabilityLive           Capability = "live/v1"
	CapabilityIncomingShares Capability = "incoming-shares/v1"
	CapabilitySecureStorage  Capability = "secure-storage/v1"
	CapabilityWidgets        Capability = "widgets/v1"
	CapabilityPrivacy        Capability = "privacy/v1"
)

// Support distinguishes a service from an explicitly disclosed fallback.
type Support string

const (
	SupportAvailable   Support = "available"
	SupportFallback    Support = "fallback"
	SupportUnavailable Support = "unavailable"
)

// Requirement makes a screen's nonoptional presentation needs explicit.
type Requirement struct {
	Capability    Capability `json:"capability"`
	AllowFallback bool       `json:"allowFallback,omitempty"`
}

// Hello advertises facts supplied by a compiled renderer at startup.
type Hello struct {
	Minimum      int                    `json:"minimum"`
	Maximum      int                    `json:"maximum"`
	Renderer     string                 `json:"renderer"`
	Capabilities map[Capability]Support `json:"capabilities"`
}

// Contract records the negotiated version and renderer services for one connection.
type Contract struct {
	Version      int                    `json:"version"`
	Renderer     string                 `json:"renderer"`
	Capabilities map[Capability]Support `json:"capabilities"`
}

// Negotiate rejects incompatible compiled peers instead of silently changing renderers.
func Negotiate(hello Hello) (Contract, error) {
	return negotiate(hello, knownCapability)
}

func negotiate(hello Hello, allowed func(Capability) bool) (Contract, error) {
	if hello.Minimum < 1 || hello.Maximum < hello.Minimum || hello.Minimum > ProtocolVersion || hello.Maximum < ProtocolVersion {
		return Contract{}, errors.New("nativeui: incompatible mobile protocol")
	}
	if hello.Renderer != "uikit" && hello.Renderer != "android" && hello.Renderer != "webview" {
		return Contract{}, errors.New("nativeui: unknown renderer")
	}
	if len(hello.Capabilities) > 32 {
		return Contract{}, errors.New("nativeui: too many capabilities")
	}
	contract := Contract{Version: ProtocolVersion, Renderer: hello.Renderer, Capabilities: make(map[Capability]Support)}
	for capability, support := range hello.Capabilities {
		if !allowed(capability) || (support != SupportAvailable && support != SupportFallback && support != SupportUnavailable) {
			return Contract{}, fmt.Errorf("nativeui: invalid capability %q", capability)
		}
		contract.Capabilities[capability] = support
	}
	if err := contract.Check([]Requirement{{Capability: CapabilityNavigation}, {Capability: CapabilityEditor}}); err != nil {
		return Contract{}, err
	}
	return contract, nil
}

// Check verifies required services before mounting a screen.
func (contract Contract) Check(requirements []Requirement) error {
	for _, requirement := range requirements {
		support := contract.Capabilities[requirement.Capability]
		if support != SupportAvailable && !(support == SupportFallback && requirement.AllowFallback) {
			return fmt.Errorf("nativeui: required capability %q is unavailable", requirement.Capability)
		}
	}
	return nil
}

func knownCapability(capability Capability) bool {
	switch capability {
	case CapabilityNavigation, CapabilityEditor, CapabilityDrafts, CapabilityShare, CapabilityHaptics, CapabilitySheets, CapabilityAnnouncement, CapabilityPresentation, CapabilityFiles, CapabilityPhotos, CapabilityCamera, CapabilityNotifications, CapabilitySharing, CapabilityClipboard, CapabilityAudio, CapabilityLive, CapabilityIncomingShares, CapabilitySecureStorage, CapabilityWidgets, CapabilityPrivacy:
		return true
	default:
		return false
	}
}

// EffectStatus describes an observed platform outcome, including user cancellation.
type EffectStatus string

const (
	EffectCompleted   EffectStatus = "completed"
	EffectCancelled   EffectStatus = "cancelled"
	EffectUnsupported EffectStatus = "unsupported"
	EffectDenied      EffectStatus = "denied"
	EffectFailed      EffectStatus = "failed"
)

// EffectReceipt acknowledges a local effect. It does not promise exactly-once OS delivery.
type EffectReceipt struct {
	ID         string       `json:"id"`
	Capability Capability   `json:"capability"`
	Status     EffectStatus `json:"status"`
	Message    string       `json:"message,omitempty"`
}
